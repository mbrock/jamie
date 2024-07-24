package main

import (
	"jamie/db"
	"os"

	"github.com/charmbracelet/log"
)

func main() {
	logger := log.New(os.Stdout)
	sqlLogger := logger.With("component", "sql")

	err := db.InitDB(sqlLogger)
	if err != nil {
		logger.Fatal("initialize database", "error", err.Error())
	}
	defer db.Close()

	logger.Info("Database initialized and migrations applied successfully")
}
