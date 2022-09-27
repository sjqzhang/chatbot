package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/go-xorm/xorm"
	"github.com/kevwan/chatbot/bot/corpus"
	"github.com/prometheus/common/log"

	"runtime"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/kevwan/chatbot/bot/adapters/logic"
	"github.com/kevwan/chatbot/bot/adapters/storage"
	_ "github.com/mattn/go-sqlite3"
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

type CORPUS_TYPE int

const (
	CORPUS_CORPUS      CORPUS_TYPE = 1
	CORPUS_REQUIREMENT CORPUS_TYPE = 2
	CORPUS_RULES       CORPUS_TYPE = 3
)

func (c CORPUS_TYPE) Int() int {
	return int(c)
}

type ChatBotFactory struct {
	mu       sync.Mutex
	chatBots map[string]*ChatBot
	config   Config
}

type ChatBotUpdate struct {
	ChatBot    ChatBot
	UpdateTime int
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
		err = engine.Sync2(&Corpus{}, &Project{}, &Feedback{})
		if err != nil {
			log.Error(err)
		}

	}
	//if err := engine.Ping(); err != nil {
	//	fmt.Println(err)
	//	return
	//}
	projects := make([]Project, 0)
	err = engine.Find(&projects)
	for _, project := range projects {
		var conf Config
		json.Unmarshal([]byte(project.Config), &conf)
		if project.Name == "" {
			continue
		}
		conf.Project = project.Name
		if _, ok := f.GetChatBot(project.Name); !ok {
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
}

func (f *ChatBotFactory) Refresh() {
	f.Init()
}

func (f *ChatBotFactory) GetChatBot(project string) (*ChatBot, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	chatBot, ok := f.chatBots[project]
	// if chatBotUpdate == nil {
	// 	return nil, ok
	// }
	// return &chatBotUpdate.ChatBot, ok
	return chatBot, ok
}

func (f *ChatBotFactory) AddChatBot(project string, chatBot *ChatBot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.chatBots[project]; !ok {
		// var ChatBotUpdate = new(ChatBotUpdate)
		// ChatBotUpdate.ChatBot = *chatBot
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
	err = engine.Limit(limit, start).Where("question like ?", "%"+corpus.Question+"%").Find(&corpuses)
	if err != nil {
		log.Error(err)
	}
	return corpuses
}

type CorpusResp struct {
	Id              int    `json:"id" form:"id" xorm:"int pk autoincr notnull 'id' comment('编号')"`
	Class           string `json:"class" form:"class"  xorm:"varchar(255) notnull 'class' comment('分类')"`
	Project         string `json:"project" form:"project" xorm:"varchar(255) notnull 'project' comment('项目')"`
	Question        string `json:"question" form:"question"  xorm:"varchar(2048) notnull  'question' comment('问题')"`
	Answer          string `json:"answer" form:"answer" xorm:"text notnull  'answer' comment('回答')"`
	Sample          string `json:"sample" form:"sample" xorm:"text notnull  'sample' comment('样本')"`
	Creator         string `json:"creator" form:"creator" xorm:"varchar(256) notnull  'creator' comment('创建人')"`
	Principal       string `json:"principal" form:"principal" xorm:"varchar(256) notnull  'principal' comment('责负人')"`
	Reviser         string `json:"reviser" form:"reviser" xorm:"varchar(256) notnull  'reviser' comment('修订人')"`
	AcceptCount     int    `json:"accept_count" form:"accept_count" xorm:"int notnull default 0  'accept_count' comment('解决次数')"`
	RejectCount     int    `json:"reject_count" form:"reject_count" xorm:"int notnull  default 0 'reject_count' comment('解决次数')"`
	CreatedAt       int    `json:"created_at" xorm:"created_at created" description:"创建时间"`
	UpdatedAt       int    `json:"updated_at" xorm:"updated_at updated" description:"更新时间"`
	DeletedAt       int    `xorm:"deleted_at" json:"deleted_at" description:"删除时间"`
	Qtype           int    `json:"qtype" form:"qtype" xorm:"int notnull 'qtype' comment('类型，需求，问答, 规则')"`
	RequirementType string `json:"requirement_type" xorm:"requirement_type"`
	QuesState       int    `json:"ques_state" xorm:"ques_state"`
	Resp            string `json:"resp" xorm:"resp"`
	SubProject      string `json:"sub_project" xorm:"sub_project"`
}

func (f *ChatBotFactory) GetRequirementList(project string, user string, qtype int) (corpusListResp []*CorpusResp) {
	corpusList := make([]*Corpus, 0)
	corpusListResp = make([]*CorpusResp, 0)
	var querys []string
	var args []interface{}
	if len(project) != 0 {
		querys = append(querys, "project = ?")
		args = append(args, project)
	}
	if user != "" {
		user += "@shopee.com"
		querys = append(querys, "creator = ?")
		args = append(args, user)
	}
	if qtype == CORPUS_CORPUS.Int() {
		if qtype == CORPUS_CORPUS.Int() {
			querys = append(querys, "qtype = ?")
			args = append(args, CORPUS_CORPUS.Int())
			querys = append(querys, "ques_state = ?")
			args = append(args, QuesReceive)
		}
		err := engine.Where(strings.Join(querys, " AND "), args...).OrderBy("id desc").Find(&corpusList)
		if err != nil {
			log.Error(err)
		}
	} else if qtype == CORPUS_REQUIREMENT.Int() {
		if qtype == CORPUS_REQUIREMENT.Int() {
			querys = append(querys, "qtype = ?")
			args = append(args, CORPUS_REQUIREMENT.Int())
		}
		err := engine.Where(strings.Join(querys, " AND "), args...).OrderBy("id desc").Find(&corpusList)
		if err != nil {
			log.Error(err)
		}
	} else if qtype == 0 {
		var queryQues []string
		var argsQues []interface{}
		queryQues = append(queryQues, querys...)
		argsQues = append(argsQues, args...)
		queryQues = append(queryQues, "qtype = ?")
		argsQues = append(argsQues, CORPUS_CORPUS.Int())
		queryQues = append(queryQues, "ques_state >= ?")
		argsQues = append(argsQues, QuesReceive)
		err := engine.Where(strings.Join(queryQues, " AND "), argsQues...).OrderBy("id desc").Find(&corpusList)
		if err != nil {
			log.Error(err)
		}
		var queryRequire []string
		var argsRequire []interface{}
		queryRequire = append(queryRequire, querys...)
		argsRequire = append(argsRequire, args...)
		queryRequire = append(queryRequire, "qtype = ?")
		argsRequire = append(argsRequire, CORPUS_REQUIREMENT.Int())
		var corpusListRequirement = make([]*Corpus, 0)
		err = engine.Where(strings.Join(queryRequire, " AND "), argsRequire...).OrderBy("id desc").Find(&corpusListRequirement)
		if err != nil {
			log.Error(err)
		}
		corpusList = append(corpusList, corpusListRequirement...)
	}

	for _, corpusItem := range corpusList {
		corpusListResp = append(corpusListResp, &CorpusResp{
			Id:              corpusItem.Id,
			Class:           corpusItem.Class,
			Project:         project,
			Question:        corpusItem.Question,
			Answer:          corpusItem.Answer,
			Sample:          corpusItem.Sample,
			Creator:         corpusItem.Creator,
			Principal:       corpusItem.Principal,
			Reviser:         corpusItem.Reviser,
			AcceptCount:     corpusItem.AcceptCount,
			RejectCount:     corpusItem.RejectCount,
			CreatedAt:       int(corpusItem.CreatedAt.Unix()),
			UpdatedAt:       int(corpusItem.UpdatedAt.Unix()),
			DeletedAt:       int(corpusItem.DeletedAt.Unix()),
			Qtype:           corpusItem.Qtype,
			RequirementType: corpusItem.RequirementType,
			QuesState:       corpusItem.QuesState,
			Resp:            corpusItem.Resp,
		})
	}

	return corpusListResp
}

func (f *ChatBotFactory) GetCorpusList(qusType CORPUS_TYPE) []Corpus {
	var corpuses []Corpus
	err := engine.Where("qtype = ?", int(qusType)).Find(&corpuses)
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

	err = engine.Sync2(&Corpus{}, &Project{}, &Feedback{})
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
	log.Error(err)
	if err != nil {
		panic(err)
	}

	go chatbot.syncCorpus()

}

func (chatbot *ChatBot) syncCorpus() {
	for {
		defer func() {
			if err := recover(); err != nil {
				fmt.Printf("Runtime panic caught: %v\n", err)
			}
		}()

		time.Sleep(time.Second * 10)
		corpuses, err := chatbot.LoadCorpusFromDB()
		if err != nil {
			log.Error(err)
		}

		if err := chatbot.Trainer.TrainWithCorpus(corpuses); err != nil {
			log.Error(err)
		}

	}
}

type Corpus struct {
	Id              int       `json:"id" form:"id" xorm:"int pk autoincr notnull 'id' comment('编号')"`
	Class           string    `json:"class" form:"class"  xorm:"varchar(255) notnull 'class' comment('分类')"`
	Project         string    `json:"project" form:"project" xorm:"varchar(255) notnull 'project' comment('项目')"`
	Question        string    `json:"question" form:"question"  xorm:"varchar(2048) notnull  'question' comment('问题')"`
	Answer          string    `json:"answer" form:"answer" xorm:"text notnull  'answer' comment('回答')"`
	Sample          string    `json:"sample" form:"sample" xorm:"text notnull  'sample' comment('样本')"`
	Creator         string    `json:"creator" form:"creator" xorm:"varchar(256) notnull  'creator' comment('创建人')"`
	Principal       string    `json:"principal" form:"principal" xorm:"varchar(256) notnull  'principal' comment('责负人')"`
	Reviser         string    `json:"reviser" form:"reviser" xorm:"varchar(256) notnull  'reviser' comment('修订人')"`
	AcceptCount     int       `json:"accept_count" form:"accept_count" xorm:"int notnull default 0  'accept_count' comment('解决次数')"`
	RejectCount     int       `json:"reject_count" form:"reject_count" xorm:"int notnull  default 0 'reject_count' comment('解决次数')"`
	CreatedAt       time.Time `json:"created_at" xorm:"created_at created" description:"创建时间"`
	UpdatedAt       time.Time `json:"updated_at" xorm:"updated_at updated" description:"更新时间"`
	DeletedAt       time.Time `xorm:"deleted_at" json:"deleted_at" description:"删除时间"`
	Qtype           int       `json:"qtype" form:"qtype" xorm:"int notnull 'qtype' comment('类型，需求，问答, 规则')"`
	RequirementClass string `json:"requirement_class" xorm:"varchar(256) notnull requirement_class"`
	RequirementType string    `json:"requirement_type" xorm:"requirement_type"`
	QuesState       int       `json:"ques_state" xorm:"ques_state"`
	Resp            string    `json:"resp" xorm:"resp"`
	SubProject      string    `json:"sub_project" xorm:"sub_project"`
}

type Feedback struct {
	Id          int       `json:"id" form:"id" xorm:"int pk autoincr 'id' comment('编号')"`
	Cid         int       `json:"cid" form:"cid" xorm:"int 'cid' comment('语料编号')"`
	Class       string    `json:"class" form:"class"  xorm:"varchar(255) 'class' comment('分类')"`
	Project     string    `json:"project" form:"project" xorm:"varchar(255) 'project' comment('项目')"`
	Question    string    `json:"question" form:"question"  xorm:"varchar(2048)  'question' comment('问题')"`
	Answer      string    `json:"answer" form:"answer" xorm:"text 'answer' comment('回答')"`
	Creator     string    `json:"creator" form:"creator" xorm:"varchar(256)  'creator' comment('创建人')"`
	Principal   string    `json:"principal" form:"principal" xorm:"varchar(256)  'principal' comment('责负人')"`
	Reviser     string    `json:"reviser" form:"reviser" xorm:"varchar(256) 'reviser' comment('修订人')"`
	AcceptCount int       `json:"accept_count" form:"accept_count" xorm:"int default 0  'accept_count' comment('解决次数')"`
	RejectCount int       `json:"reject_count" form:"reject_count" xorm:"int default 0  'reject_count' comment('解决次数')"`
	CreatedAt   time.Time `json:"created_at" xorm:"created_at created" description:"创建时间"`
	UpdatedAt   time.Time `json:"updated_at" xorm:"updated_at updated" description:"更新时间"`
	//DeletedAt   time.Time `xorm:"deleted_at" json:"deleted_at" description:"删除时间"`
	Qtype int `json:"qtype" form:"qtype" xorm:"int 'qtype' comment('类型，需求，问答, 规则')"`
}

type Project struct {
	Id        int       `json:"id" form:"id" xorm:"int pk autoincr notnull 'id' comment('编号')"`
	Name      string    `json:"name" form:"name"  xorm:"varchar(255) notnull 'name' comment('名称')"`
	Config    string    `json:"config" form:"config"  xorm:"text notnull 'config' comment('配置')"`
	Status    int       `json:"status" form:"status"  xorm:"int notnull default 1 'status' comment('状态')"`
	CreatedAt time.Time `json:"created_at" xorm:"created_at created" json:"created_at" description:"创建时间"`
	UpdatedAt time.Time `json:"updated_at" xorm:"updated_at updated"json:"updated_at"description:"更新时间"`
	DeletedAt time.Time `xorm:"deleted_at" json:"deleted_at" description:"删除时间"`
	//Config Config
}

type Config struct {
	Driver     string `json:"driver"`
	DataSource string `json:"data_source"`
	Project    string `json:"project"`
	DirCorpus  string `json:"dir_corpus"`
	StoreFile  string `json:"store_file"`
}

type JiraConf struct {
	BaseUrl   string `json:"base_url"`
	UserName  string `json:"username"`
	Password  string `json:"password"`
	SecretKey string `json:"secretkey"`
	Board     string `json:"board"`
}

type ProjectConf struct {
	JiraConf JiraConf `json:"jira_conf"`
	Users    []string `json:"users"`
	Class    []string `json:"class"`
	Name     string   `json:"name"`
	Id       *int     `json:"id"`
	Status   *int     `json:"status"`
}

type BoardJiraReq struct {
	Board       string `json:"board"`
	Description string `json:"description" gorm:"column:description"`
	FixVersion  string `json:"fixVersions" gorm:"column:fixVersions"`
	Assignee    string `json:"assignee" gorm:"column:assignee"`
	Summary     string `json:"summary" gorm:"column:summary"`
	Id          int    `json:"id"`
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
		Project:   chatbot.Config.Project,
		Qtype:     CORPUS_CORPUS.Int(),
		QuesState: QuesCustom.Int(),
	}
	err := engine.Find(&rows, &query)
	if err != nil {
		return nil, err
	}
	var corpuses [][]string
	exp, err := regexp.Compile(`[|｜\r\n]+`)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		var corpus []string
		questions := exp.Split(row.Question, -1)

		for _, question := range questions {
			if strings.TrimSpace(question) == "" {
				continue
			}
			if !strings.HasSuffix(question, "?") && !strings.HasSuffix(question, "？") {
				question = question + "?"
			}
			corpus = append(corpus, question, fmt.Sprintf("%s$$$$%s$$$$%v", question, row.Answer, row.Id))
		}
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

func (chatbot *ChatBot) AddFeedbackToDB(feedback *Feedback) error {
	corpus := Corpus{
		Id: feedback.Cid,
	}
	var (
		ok  bool
		err error
	)
	if ok, _ = engine.Get(&corpus); ok {
		feedback.Project = corpus.Project
		feedback.Class = corpus.Class
		_, err = engine.Insert(feedback)
		return err
	} else {
		_, err = engine.Insert(feedback)
	}

	return err

}

func (chatbot *ChatBotFactory) UpdateCorpusCounter(id int, isOk bool) error {
	if id <= 0 {
		return fmt.Errorf("%v", "编号<0不合法")
	}
	q := Corpus{
		Id: id,
	}
	var (
		err error
		ok  bool
	)
	if ok, err = engine.Get(&q); ok {
		if isOk {
			q.AcceptCount = q.AcceptCount + 1
		} else {
			q.RejectCount = q.RejectCount + 1
		}
		if id > 0 {
			_, err = engine.Id(id).Cols("reject_count", "accept_count").Update(&q)
		}
	} else {
		err = fmt.Errorf("record not found")
	}
	return err

}

type RequirementType int

const (
	RequirementReceive  RequirementType = 1
	RequirementReject   RequirementType = 2
	RequirementHandle   RequirementType = 3
	RequirementComplete RequirementType = 4
)

func (r RequirementType) Int() int {
	return int(r)
}

type QuesState int

const (
	QuesCustom  QuesState = 1
	QuesReceive QuesState = 2
	QuesHandle  QuesState = 3
)

func (q QuesState) Int() int {
	return int(q)
}

func (chatbot *ChatBot) AddCorpusToDB(corpus *Corpus) error {
	q := Corpus{
		Question: corpus.Question,
		Class:    corpus.Class,
	}
	if corpus.Id != 0 {
		q = Corpus{
			Id: corpus.Id,
		}
	}

	if ok, err := engine.Get(&q); !ok {
		if corpus.Qtype == int(CORPUS_REQUIREMENT) {
			corpus.RequirementType = "收到"
		} else if corpus.Qtype == int(CORPUS_CORPUS) {
			corpus.Qtype = int(CORPUS_REQUIREMENT)
			//corpus.QuesState = QuesReceive.Int()
			corpus.RequirementType = "收到"
		} else {
			corpus.Qtype = int(CORPUS_REQUIREMENT)
			corpus.RequirementType = "收到"
		}
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

func (chatbot *ChatBot) ModifyCorpusToDB(id int, ques string, ans string) error {
	q := Corpus{
		Id:       id,
		Question: ques,
		Answer:   ans,
	}
	_, err := engine.Update(&q)
	if err != nil {
		return err
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
