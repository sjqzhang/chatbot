package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-xorm/xorm"
	"github.com/kevwan/chatbot/bot/corpus"
	"github.com/prometheus/common/log"
	"path/filepath"
	"sync"

	//"github.com/jinzhu/gorm"
	"github.com/kevwan/chatbot/bot/adapters/logic"
	"github.com/kevwan/chatbot/bot/adapters/storage"
	_ "github.com/mattn/go-sqlite3"
	"runtime"
	"time"
)

const mega = 1024 * 1024

type ChatBot struct {
	PrintMemStats bool
	//InputAdapter   input.InputAdapter
	LogicAdapter logic.LogicAdapter
	//OutputAdapter  output.OutputAdapter
	StorageAdapter storage.StorageAdapter
	Trainer        Trainer
	Config         Config
}

type ChatBotFactory struct {
	mu       sync.Mutex
	chatBots map[string]*ChatBot
	config   Config
}

//var chatBotFactory *ChatBotFactory

func NewChatBotFactory(config Config) *ChatBotFactory {

	return &ChatBotFactory{
		mu:       sync.Mutex{},
		config:   config,
		chatBots: make(map[string]*ChatBot),
	}

}
func (f *ChatBotFactory) Init() {
	var err error
	if engine == nil {
		engine, err = xorm.NewEngine(f.config.Driver, f.config.DataSource)
		if err != nil {
			panic(err)
		}
		err = engine.Sync2(&Corpus{}, &Project{})
		if err != nil {
			log.Error(err)
		}

	}
	projects := make([]Project, 0)
	err = engine.Find(&projects)
	for _, project := range projects {
		var conf Config
		json.Unmarshal([]byte(project.Config), &conf)
		if project.Name == "" {
			continue
		}
		store, _ := storage.NewSeparatedMemoryStorage(fmt.Sprintf("%s.gob", project.Name))
		chatbot := &ChatBot{
			LogicAdapter:   logic.NewClosestMatch(store, 5),
			PrintMemStats:  false,
			Trainer:        NewCorpusTrainer(store),
			StorageAdapter: store,
			Config:         conf,
		}
		f.AddChatBot(project.Name, chatbot)
		chatbot.Init()
	}

}

func (f *ChatBotFactory) GetChatBot(project string) (*ChatBot, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	chatBot, ok := f.chatBots[project]
	return chatBot, ok
}

func (f *ChatBotFactory) AddChatBot(project string, chatBot *ChatBot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.chatBots[project]; !ok {
		f.chatBots[project] = chatBot
	}

}

func (f *ChatBotFactory) ListProject() []Project {
	var projects []Project
	var err error
	err = engine.Find(&projects)
	if err != nil {
		log.Error(err)
	}
	return projects
}

func (f *ChatBotFactory) ListCorpus(corpus Corpus, start int, limit int) []Corpus {
	var corpuses []Corpus
	var err error
	err = engine.Limit(limit, start).Find(&corpuses, corpus)
	if err != nil {
		log.Error(err)
	}
	return corpuses
}

var engine *xorm.Engine

func (chatbot *ChatBot) Init() {
	var err error
	if engine == nil {
		engine, err = xorm.NewEngine(chatbot.Config.Driver, chatbot.Config.DataSource)
	}
	if err != nil {
		panic(err)
	}

	err = engine.Sync2(&Corpus{}, &Project{})
	if err != nil {
		log.Error(err)
	}

	if chatbot.Config.DirCorpus != "" {
		files := chatbot.FindCorporaFiles(chatbot.Config.DirCorpus)
		if len(files) > 0 {
			corpuses, _ := chatbot.LoadCorpusFromFiles(files)
			if len(corpuses) > 0 {
				chatbot.SaveCorpusToDB(corpuses)

			}
		}
	}
	err = chatbot.TrainWithDB()
	if err != nil {
		panic(err)
	}
}

type Corpus struct {
	Id          int       `json:"id" form:"id" xorm:"int pk autoincr notnull 'id' comment('编号')"`
	Class       string    `json:"class" form:"class"  xorm:"varchar(255) notnull 'class' comment('分类')"`
	Project     string    `json:"project" form:"project" xorm:"varchar(255) notnull 'project' comment('项目')"`
	Question    string    `json:"question" form:"question"  xorm:"varchar(512) notnull index  'question' comment('问题')"`
	Answer      string    `json:"answer" form:"answer" xorm:"varchar(102400) notnull  'answer' comment('回答')"`
	Principal   string    `json:"principal" form:"principal" xorm:"varchar(256) notnull  'principal' comment('责负人')"`
	Reviser     string    `json:"reviser" form:"reviser" xorm:"varchar(256) notnull  'reviser' comment('修订人')"`
	AcceptCount string    `json:"accept_count" form:"accept_count" xorm:"int notnull  'accept_count' comment('解决次数')"`
	RejectCount string    `json:"reject_count" form:"reject_count" xorm:"int notnull  'reject_count' comment('解决次数')"`
	CreatTime   time.Time `json:"creat_time" xorm:"creat_time created" json:"creat_time" description:"创建时间"`
	UpdateTime  time.Time `json:"update_time" xorm:"update_time updated"json:"update_time"description:"更新时间"`
	Qtype       int       `json:"qtype" form:"qtype" xorm:"int notnull 'qtype' comment('类型，需求，问答')"`
}

type Project struct {
	Id     int    `json:"id" form:"id" xorm:"int pk autoincr notnull 'id' comment('编号')"`
	Name   string `json:"name" form:"name"  xorm:"varchar(255) notnull 'name' comment('名称')"`
	Config string `json:"config" form:"config"  xorm:"varchar(102400) notnull 'config' comment('配置')"`
	//Config Config
}

type Config struct {
	Driver     string `json:"driver"`
	DataSource string `json:"data_source"`
	Project    string `json:"project"`
	DirCorpus  string `json:"dir_corpus"`
	StoreFile  string `json:"store_file"`
}

func (chatbot *ChatBot) Train(data interface{}) error {
	start := time.Now()
	defer func() {
		fmt.Printf("Elapsed: %s\n", time.Since(start))
	}()

	if chatbot.PrintMemStats {
		go func() {
			for {
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				fmt.Printf("Alloc = %vm\nTotalAlloc = %vm\nSys = %vm\nNumGC = %v\n\n",
					m.Alloc/mega, m.TotalAlloc/mega, m.Sys/mega, m.NumGC)
				time.Sleep(5 * time.Second)
			}
		}()
	}

	if err := chatbot.Trainer.Train(data); err != nil {
		return err
	} else {
		return chatbot.StorageAdapter.Sync()
	}
}

func (chatbot *ChatBot) LoadCorpusFromDB() (map[string][][]string, error) {
	results := make(map[string][][]string)
	var rows []Corpus
	query := Corpus{
		Project: chatbot.Config.Project,
	}
	err := engine.Find(&rows, &query)
	if err != nil {
		return nil, err
	}
	var corpuses [][]string
	for _, row := range rows {
		var corpus []string
		corpus = append(corpus, row.Question, row.Answer)
		corpuses = append(corpuses, corpus)
	}
	results[chatbot.Config.Project] = corpuses
	return results, nil

}

func (chatbot *ChatBot) LoadCorpusFromFiles(filePaths []string) (map[string][][]string, error) {
	return corpus.LoadCorpora(filePaths)
}

func (chatbot *ChatBot) SaveCorpusToDB(corpuses map[string][][]string) {
	for k, v := range corpuses {
		for _, cp := range v {
			if len(cp) == 2 {
				corpus := Corpus{
					Class:    k,
					Question: cp[0],
					Answer:   cp[1],
					Qtype:    1,
					Project:  chatbot.Config.Project,
				}
				chatbot.AddCorpusToDB(&corpus)
			}
		}
	}

}

func (chatbot *ChatBot) AddCorpusToDB(corpus *Corpus) error {
	q := Corpus{
		Question: corpus.Question,
		Class:    corpus.Class,
	}
	if ok, err := engine.Get(&q); !ok {
		_, err = engine.Insert(corpus)
		return err
	} else {
		if q.Id > 0 {
			corpus.Id = q.Id
			_, err = engine.Update(corpus, &Corpus{Id: q.Id})
			return err
		}
	}
	return nil
}

func (chatbot *ChatBot) RemoveCorpusFromDB(corpus *Corpus) error {
	q := Corpus{}
	if corpus.Id > 0 {
		q.Id = corpus.Id
	} else {
		if corpus.Question == "" {
			return errors.New("id or question must be set value")
		}
		q.Question = corpus.Question
	}
	if ok, err := engine.Get(&q); ok {
		chatbot.StorageAdapter.Remove(q.Question)
		engine.Delete(&q)
		return err
	}
	return nil
}

func (chatbot *ChatBot) TrainWithDB() error {
	start := time.Now()
	defer func() {
		fmt.Printf("Elapsed: %s\n", time.Since(start))
	}()

	if chatbot.PrintMemStats {
		go func() {
			for {
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				fmt.Printf("Alloc = %vm\nTotalAlloc = %vm\nSys = %vm\nNumGC = %v\n\n",
					m.Alloc/mega, m.TotalAlloc/mega, m.Sys/mega, m.NumGC)
				time.Sleep(5 * time.Second)
			}
		}()
	}

	corpuses, err := chatbot.LoadCorpusFromDB()
	if err != nil {
		return err
	}

	if err := chatbot.Trainer.TrainWithCorpus(corpuses); err != nil {
		return err
	} else {
		return nil
		//return chatbot.StorageAdapter.Sync()
	}

}

func (chatbot *ChatBot) FindCorporaFiles(dir string) []string {
	var files []string

	jsonFiles, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		fmt.Println(err)
		return nil
	}

	files = append(files, jsonFiles...)

	ymlFiles, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		fmt.Println(err)
		return nil
	}

	files = append(files, ymlFiles...)

	yamlFiles, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		fmt.Println(err)
		return nil
	}

	return append(files, yamlFiles...)
}

func (chatbot *ChatBot) GetResponse(text string) []logic.Answer {
	if chatbot.LogicAdapter.CanProcess(text) {
		return chatbot.LogicAdapter.Process(text)
	}

	return nil
}
