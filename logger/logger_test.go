package logger

import "testing"

func TestLogger(t *testing.T) {
	InitLogger()
	Info("info test", "it is ok?")
	Infof("infof test,%s", "hhh")
	Error("error test", "it is ok?")
	Errorf("errorf test,%s", "hhh")
}
