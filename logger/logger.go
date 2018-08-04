package logger

import (
	"log"
	"os"
)

var infolog = log.New(os.Stdout, "[INFO ] ", log.LstdFlags)
var errlog = log.New(os.Stdout, "[ERROR] ", log.LstdFlags)

func Info(a ...interface{}) {
	infolog.Println(a...)
}

func Error(a ...interface{}) {
	errlog.Println(a...)
}
