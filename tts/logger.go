package tts

import (
	"os"

	"github.com/charmbracelet/log"
)

var logFile *os.File

func InitLogger() {
	var err error
	logFile, err = os.OpenFile(
		"jamie.log",
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0666,
	)
	if err != nil {
		log.Fatal("Failed to open log file", "error", err)
	}

	log.SetOutput(logFile)
	log.SetReportCaller(true)
	log.SetReportTimestamp(true)
	log.SetLevel(log.DebugLevel)
}

func CloseLogger() {
	if logFile != nil {
		logFile.Close()
	}
}
