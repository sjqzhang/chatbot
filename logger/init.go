package logger

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"os"
)

type logConfig struct {
	Error logger
	Info  logger
	Data  logger
}

type logger struct {
	Output string
}

var conf logConfig

const (
	logDir    = "./log"
	errorFile = "./log/error.log"
	infoFile  = "./log/info.log"
	dataFile  = "./log/data.log"
)

func InitLogger() {
	conf = logConfig{
		Error: logger{Output: errorFile},
		Info:  logger{Output: infoFile},
		Data:  logger{Output: dataFile},
	}
	err := os.Mkdir(logDir, os.FileMode(0755))
	if err != nil {
		return
	}
}

func (l *logger) write(content ...interface{}) {
	f, err := os.OpenFile(l.Output, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0777)
	if err != nil {
		return
	}
	defer func(f *os.File) {
		err = f.Close()
		if err != nil {
			fmt.Printf("%v", err)
			return
		}
	}(f)
	log := logrus.New()
	log.SetOutput(f)
	log.Error(content...)
	fmt.Printf("%s\n", content)
}

func Error(args ...interface{}) {
	conf.Error.write(args...)
}

func Errorf(format string, args ...interface{}) {
	content := fmt.Sprintf(format, args...)
	conf.Error.write(content)
}

func Info(args ...interface{}) {
	conf.Info.write(args...)
}

func Infof(format string, args ...interface{}) {
	conf.Info.write(fmt.Sprintf(format, args...))
}
