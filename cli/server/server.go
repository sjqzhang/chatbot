package main

import (
	"flag"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gobuffalo/packr"
	"github.com/kevwan/chatbot/bot"
	"github.com/kevwan/chatbot/bot/adapters/logic"
	"github.com/prometheus/common/log"
)

var factory *bot.ChatBotFactory

var (
	verbose = flag.Bool("v", false, "verbose mode")
	tops    = flag.Int("t", 5, "the number of answers to return")
	dir     = flag.String("d", "/Users/dev/repo/chatterbot-corpus/chatterbot_corpus/data/chinese", "the directory to look for corpora files")
	//sqliteDB = flag.String("sqlite3", "/Users/junqiang.zhang/repo/go/chatbot/chatbot.db", "the file path of the corpus sqlite3")
	driver        = flag.String("driver", "sqlite3", "db driver")
	datasource    = flag.String("datasource", "./chatbot.db", "datasource connection")
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

type ResoveReq struct {
	IsOk bool `json:"is_ok"`
	Id   int  `json:"id"`
}

type ruleDataReq struct {
	Data  string `json:"data"`
	Other string `json:"other"`
}

type RequirementListReq struct {
	Project string `json:"project"`
	User    string `json:"user"`
	Qtype   int    `json:"qtype"`
}

type RequirementListRespItem struct {
	RequirementDesc  string `json:"requirement_desc"`
	Sample           string `json:"sample"`
	RequirementType  int    `json:"requirement_type"`
	RequirementTitle string `json:"requirement_title"`
	Ctime            string `json:"ctime"`
}

type RequirementListResp struct {
	List []RequirementListRespItem `json:"list"`
}

type ModifyCorpus struct {
	Id       int    `json:"id"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type DeployLogRecordItem struct {
	Ans []QueryLogResp `json:"ans"`
}

type QueryLogResp struct {
	Id        int    `json:"id"`
	MatchRule string `json:"match_rule"`
	Question  string `json:"question"`
	Answer    string `json:"answer"`
	Principal string `json:"principal"`
}

type DeployLogRecordList struct {
	Logs []DeployLogRecordItem `json:"logs"`
}

type BuildLogAnysisResp struct {
	Ans []string `json:"ans"`
}

type RuleResp struct {
	DeployLogRecordList DeployLogRecordList `json:"deploy_log_record"`
	AnysisRes           string              `json:"anysisRes"`
	Flag                bool                `json:"flag"`
}

func init() {

	flag.Parse()

}

func HandlerResult(ctx *gin.Context, data *interface{}, err *error) {
	message := "success"
	if *err != nil {
		message = (*err).Error()
	}
	if *err != nil {
		ctx.JSON(200, JsonResult{
			Msg:  message,
			Code: 500,
			Data: data,
		})
	} else {
		ctx.JSON(200, JsonResult{
			Msg:  message,
			Code: 0,
			Data: data,
		})
	}
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

	v1 := router.Group("api/v1")
	v1.GET("/smoke", func(context *gin.Context) {
		context.JSON(200, JsonResult{
			Msg:  "smoke success",
			Code: 200,
		})
	})
	v1.POST("add", func(context *gin.Context) {
		var (
			data interface{}
			err  error
		)
		defer HandlerResult(context, &data, &err)
		var corpus bot.Corpus

		context.Bind(&corpus)

		corpus.Qtype = int(bot.CORPUS_CORPUS)

		project := corpus.Project
		var chatbot *bot.ChatBot
		if chatbot, _ = factory.GetChatBot(project); chatbot == nil {
			err = fmt.Errorf("project '%s' not found", project)
			return
		}
		corpus.Question = strings.ToLower(corpus.Question)
		err = chatbot.AddCorpusToDB(&corpus)
		if err != nil {
			return
		}
		answer := make(map[string]int)
		exp, err := regexp.Compile(`[|｜\r\n]+`)
		if err != nil {
			return
		}
		questions := exp.Split(corpus.Question, -1)
		for _, question := range questions {
			if strings.TrimSpace(question) == "" {
				continue
			}
			if !strings.HasSuffix(question, "?") && !strings.HasSuffix(question, "？") {
				question = question + "?"
			}
			answer[fmt.Sprintf("%s$$$$%s$$$$%v", question, corpus.Answer, corpus.Id)] = 1
			chatbot.StorageAdapter.Update(question, answer)
		}
		chatbot.StorageAdapter.BuildIndex()
	})

	v1.GET("search", func(context *gin.Context) {
		var (
			data interface{}
			err  error
		)
		defer HandlerResult(context, &data, &err)
		qusString := context.Query("qus_type")
		var qusType int
		if len(qusString) == 0 {
			qusType = 1
		} else {
			qusType, err = strconv.Atoi(qusString)
			if err != nil {
				err = fmt.Errorf("qus_type '%s' atoi err:'%s'", qusString, err)
				return
			}
		}
		q := context.Query("q")
		if qusType == int(bot.CORPUS_CORPUS) {
			p := context.Query("p")
			if p == "" {
				p = *project
			}
			var chatbot *bot.ChatBot
			if chatbot, _ = factory.GetChatBot(p); chatbot == nil {
				factory.Refresh()
				err = fmt.Errorf("project '%s' not found,please retry 1 minute later.", p)
				return
			}
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
			data = qas
		}
	})

	v1.POST("rule", func(context *gin.Context) {
		var (
			data interface{}
			err  error
		)
		defer HandlerResult(context, &data, &err)
		var dataRule ruleDataReq
		//dataRule.Data = context.Query("data")
		//dataRule.Other = context.Query("other")
		context.Bind(&dataRule)
		if err != nil {
			log.Error(err)
			return
		}
		corpuses := factory.GetCorpusList(bot.CORPUS_RULES)
		i := 0
		resp := &RuleResp{}
		var anysisRes string
		var deployLogRecordList = new(DeployLogRecordList)
		for _, rule := range corpuses {
			reg, err := regexp.Compile("(?i)" + rule.Question)
			if err != nil {
				log.Error(err)
				return
			}
			if reg == nil {
				return
			}
			result := reg.FindAllString(dataRule.Data, -1)
			var logItem = new(DeployLogRecordItem)
			//如果没匹配到错误，直接返回
			if len(result) == 0 {
				continue
			}
			//把所有匹配到的日志进行拼接
			var matchLogs string
			for _, resultItem := range result {
				matchLogs += resultItem + "\n"
			}
			//形成一个答案
			ans := &QueryLogResp{
				Id:        i,
				MatchRule: rule.Question,
				Question:  matchLogs,
				Answer:    rule.Answer,
				Principal: rule.Principal,
			}
			if rule.Principal == dataRule.Other {
				resp.Flag = true
			}
			i++
			logItem.Ans = append(logItem.Ans, *ans)
			deployLogRecordList.Logs = append(deployLogRecordList.Logs, *logItem)

			anysisRes += rule.Answer + "\n"
		}
		resp.DeployLogRecordList = *deployLogRecordList
		resp.AnysisRes = anysisRes
		data = resp
	})

	v1.POST("corpus/remove", func(context *gin.Context) {
		var (
			data interface{}
			err  error
		)
		defer HandlerResult(context, &data, &err)
		var corpus bot.Corpus
		var chatbot *bot.ChatBot
		if chatbot, _ = factory.GetChatBot(*project); chatbot == nil {
			err = fmt.Errorf("project '%s' not found", *project)
			return
		}
		context.Bind(&corpus)
		err = chatbot.RemoveCorpusFromDB(&corpus)
		if err != nil {
			return
		}
		chatbot.StorageAdapter.BuildIndex()
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

	v1.POST("modify/corpus", func(context *gin.Context) {
		var (
			data interface{}
			err  error
		)
		defer HandlerResult(context, &data, &err)
		var corpus bot.Corpus
		var chatbot *bot.ChatBot
		if chatbot, _ = factory.GetChatBot(*project); chatbot == nil {
			err = fmt.Errorf("project '%s' not found", *project)
			return
		}
		err = context.Bind(&corpus)
		if err != nil {
			return
		}
		err = chatbot.ModifyCorpusToDB(corpus.Id, corpus.Question, corpus.Answer)
		if err != nil {
			return
		}
	})

	v1.POST("requirement/add", func(context *gin.Context) {
		var (
			data interface{}
			err  error
		)
		defer HandlerResult(context, &data, &err)
		var corpus bot.Corpus
		context.Bind(&corpus)
		if len(corpus.Question) < 10 {
			err = fmt.Errorf("标题于简单，不少于10个汉字！！！")
			return
		}
		// if len(corpus.Answer) < 120 {
		// 	err = fmt.Errorf("问题描述过于简单，不少于40个汉字！！！")
		// 	return
		// }
		project := corpus.Project
		var chatbot *bot.ChatBot
		if chatbot, _ = factory.GetChatBot(project); chatbot == nil {
			err = fmt.Errorf("project '%s' not found", project)
			return
		}
		corpus.Question = strings.ToLower(corpus.Question)
		corpus.Creator += "@shopee.com"
		if len(corpus.SubProject) == 0 {
			corpus.SubProject = corpus.Project
		}
		err = chatbot.AddCorpusToDB(&corpus)
		if err != nil {
			return
		}
	})

	v1.POST("requirement/list", func(context *gin.Context) {
		var requirementReq RequirementListReq
		context.Bind(&requirementReq)

		corpusList := factory.GetRequirementList(requirementReq.Project, requirementReq.User, requirementReq.Qtype)
		context.JSON(200, JsonResult{
			Code: 0,
			Msg:  "success",
			Data: corpusList,
		})
	})

	v1.POST("feedback", func(context *gin.Context) {
		var (
			data interface{}
			err  error
		)
		defer HandlerResult(context, &data, &err)
		var req ResoveReq
		err = context.Bind(&req)
		if err != nil {
			return
		}
		err = factory.UpdateCorpusCounter(req.Id, req.IsOk)
		if err != nil {
			return
		}
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
	box := packr.NewBox("./static")
	_ = box
	//router.StaticFS("/static", http.FileSystem(box))
	router.StaticFS("/static", http.Dir("./static"))
	bindRounter(router)
	router.Run(*bind)
}
