package tts

import (
	"io"
	"os"

	"github.com/charmbracelet/log"
)

var fileLogger *log.Logger

func initFileLogger() {
	logFile, err := os.OpenFile("jamie.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal("Failed to open log file", "error", err)
	}

	fileLogger = log.NewWithOptions(logFile, log.Options{
		ReportCaller:    true,
		ReportTimestamp: true,
		Level:           log.InfoLevel,
	})
}

func getLogger() *log.Logger {
	if fileLogger != nil {
		return fileLogger
	}
	return log.Default()
}

func closeFileLogger() {
	if fileLogger != nil {
		if closer, ok := fileLogger.Writer().(io.Closer); ok {
			closer.Close()
		}
	}
}
