package main

import (
	"flag"
	"fmt"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gobuffalo/packr"
	"github.com/kevwan/chatbot/bot"
	"github.com/kevwan/chatbot/bot/adapters/logic"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var factory *bot.ChatBotFactory

var (
	verbose = flag.Bool("v", false, "verbose mode")
	tops    = flag.Int("t", 5, "the number of answers to return")
	dir     = flag.String("d", "/Users/dev/repo/chatterbot-corpus/chatterbot_corpus/data/chinese", "the directory to look for corpora files")
	//sqliteDB = flag.String("sqlite3", "/Users/junqiang.zhang/repo/go/chatbot/chatbot.db", "the file path of the corpus sqlite3")
	driver        = flag.String("driver", "sqlite3", "db driver")
	datasource    = flag.String("datasource", "chatbot.db", "datasource connection")
	bind          = flag.String("b", ":8080", "bind addr")
	project       = flag.String("project", "DMS", "the name of the project in sqlite3 db")
	corpora       = flag.String("i", "", "the corpora files, comma to separate multiple files")
	storeFile     = flag.String("o", "/Users/dev/repo/chatbot/corpus.gob", "the file to store corpora")
	printMemStats = flag.Bool("m", false, "enable printing memory stats")
)

type JsonResult struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data"`
}

type QA struct {
	Question string  `json:"question"`
	Answer   string  `json:"answer"`
	Score    float32 `json:"score"`
	ID       int     `json:"id"`
}

func init() {

	flag.Parse()

}

func bindRounter(router *gin.Engine) {
	buildAnswer := func(answers []logic.Answer) []QA {
		var qas []QA
		for _, answer := range answers {
			contents := strings.Split(answer.Content, "$$$$")
			if len(contents) > 2 {
				qa := QA{
					Question: contents[0],
					Answer:   contents[1],
					Score:    answer.Confidence,
				}
				qa.ID, _ = strconv.Atoi(contents[2])
				qas = append(qas, qa)
			}
		}
		return qas
	}
	v1 := router.Group("v1")
	v1.POST("add", func(context *gin.Context) {

		var corpus bot.Corpus

		context.Bind(&corpus)

		corpus.Qtype = int(bot.CORPUS_CORPUS)

		project := corpus.Project
		var chatbot *bot.ChatBot
		if chatbot, _ = factory.GetChatBot(project); chatbot == nil {
			context.JSON(200, JsonResult{
				Code: 404,
				Msg:  fmt.Sprintf("project '%s' not found", project),
			})
		}
		corpus.Question = strings.ToLower(corpus.Question)
		err := chatbot.AddCorpusToDB(&corpus)
		if err != nil {
			context.JSON(500, JsonResult{
				Msg: err.Error(),
			})
			return
		}
		answer := make(map[string]int)
		exp, err := regexp.Compile(`[|｜\r\n]+`)
		if err != nil {
			context.JSON(500, JsonResult{
				Msg: err.Error(),
			})
			return
		}
		questions := exp.Split(corpus.Question, -1)
		for _, question := range questions {
			if !strings.HasSuffix(question, "?") && !strings.HasSuffix(question, "？") {
				question = question + "?"
			}
			answer[fmt.Sprintf("%s$$$$%s$$$$%v", question, corpus.Answer, corpus.Id)] = 1
			chatbot.StorageAdapter.Update(question, answer)
		}
		chatbot.StorageAdapter.BuildIndex()
		context.JSON(200, JsonResult{
			Code: 0,
			Msg:  "success",
		})

	})

	v1.GET("search", func(context *gin.Context) {
		p := context.Query("p")
		if p == "" {
			p = *project
		}
		var chatbot *bot.ChatBot
		if chatbot, _ = factory.GetChatBot(p); chatbot == nil {
			factory.Refresh()
			context.JSON(200, JsonResult{
				Code: 404,
				Msg:  fmt.Sprintf("project '%s' not found,please retry 1 minute later.", p),
			})
		}
		q := context.Query("q")
		if !strings.HasSuffix(q, "?") && !strings.HasSuffix(q, "？") {
			q = q + "?"
		}
		results := chatbot.GetResponse(q)
		qas := buildAnswer(results)
		if len(qas) > 0 {
			feedback := bot.Feedback{
				Question: q,
				Answer:   qas[0].Answer,
				Cid:      qas[0].ID,
			}
			chatbot.AddFeedbackToDB(&feedback)
		} else {
			answer := "对不起，没有找答案,请详细描述你的问题（文字不少于15个汉字），\n我们会自动收集你的问题并进行反馈，谢谢！！"
			if len(q) > 45 {
				answer = "对不起，没有找答案,你的问题我已经记录并反馈，无需重复提交，谢谢！！！。"
				feedback := bot.Feedback{
					Question: q,
					Answer:   "",
					Cid:      0,
				}
				chatbot.AddFeedbackToDB(&feedback)
			}
			qa := QA{
				Answer:   answer,
				Question: q,
			}
			qas = append(qas, qa)
		}
		msg := "ok"
		context.JSON(200, JsonResult{
			Code: 0,
			Msg:  msg,
			Data: qas,
		})
	})

	v1.POST("remove", func(context *gin.Context) {
		var corpus bot.Corpus
		var chatbot *bot.ChatBot
		if chatbot, _ = factory.GetChatBot(*project); chatbot == nil {
			context.JSON(200, JsonResult{
				Code: 404,
				Msg:  fmt.Sprintf("project '%s' not found", *project),
			})
		}

		context.Bind(&corpus)
		err := chatbot.RemoveCorpusFromDB(&corpus)
		if err != nil {
			context.JSON(500, JsonResult{
				Msg: err.Error(),
			})
			return
		}
		chatbot.StorageAdapter.BuildIndex()
		context.JSON(200, JsonResult{
			Code: 0,
			Msg:  "success",
		})

	})

	v1.GET("list/project", func(context *gin.Context) {

		projects := factory.ListProject()

		context.JSON(200, JsonResult{
			Code: 0,
			Msg:  "success",
			Data: projects,
		})

	})

	v1.POST("list/corpus", func(context *gin.Context) {
		var corpus bot.Corpus
		var start int
		var limit int
		start, _ = strconv.Atoi(context.PostForm("start"))
		limit, _ = strconv.Atoi(context.PostForm("length"))
		context.Bind(&corpus)
		search := context.PostFormMap("search")
		if len(search) > 0 {
			if q, ok := search["value"]; ok {
				corpus.Question = q
			}
		}
		projects := factory.ListCorpus(corpus, start, limit)
		context.JSON(200, JsonResult{
			Code: 0,
			Msg:  "success",
			Data: projects,
		})

	})

	v1.POST("add/requirement", func(context *gin.Context) {
		var corpus bot.Corpus

		context.Bind(&corpus)
		corpus.Qtype = int(bot.CORPUS_REQUIREMENT)
		project := corpus.Project
		var chatbot *bot.ChatBot
		if chatbot, _ = factory.GetChatBot(project); chatbot == nil {
			context.JSON(200, JsonResult{
				Code: 404,
				Msg:  fmt.Sprintf("project '%s' not found", project),
			})
		}
		corpus.Question = strings.ToLower(corpus.Question)
		err := chatbot.AddCorpusToDB(&corpus)
		msg := "success"
		if err != nil {
			msg = err.Error()
		}
		context.JSON(200, JsonResult{
			Code: 0,
			Msg:  msg,
			Data: err,
		})

	})

	v1.POST("feedback", func(context *gin.Context) {
		id, _ := strconv.Atoi(context.PostForm("id"))
		isOk := false
		if context.PostForm("isOk") == "1" {
			isOk = true
		}
		err := factory.UpdateCorpusCounter(id, isOk)
		msg := "success"
		if err != nil {
			msg = err.Error()
		}
		context.JSON(200, JsonResult{
			Code: 0,
			Msg:  msg,
			Data: err,
		})

	})

}

func Cors() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowAllOrigins:  true,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"*"},
		ExposeHeaders:    []string{"Content-Length", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	},
	)
}

//go:generate packr
func main() {
	factory = bot.NewChatBotFactory(bot.Config{
		Driver:     *driver,
		DataSource: *datasource,
	})
	factory.Init()
	router := gin.Default()
	router.Use(Cors())
	box := packr.NewBox("../../static")
	_ = box
	//router.StaticFS("/static", http.FileSystem(box))
	router.StaticFS("/static", http.Dir("./static"))
	bindRounter(router)
	router.Run(*bind)
}
