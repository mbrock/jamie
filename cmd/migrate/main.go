package main

import (
	"jamie/db"
	"os"

	"github.com/charmbracelet/log"
)

func main() {
	logger := log.New(os.Stdout)
	sqlLogger := logger.With("component", "sql")

	db.InitDB()
	defer db.Close()

	// Load and apply migrations
	migrations, err := db.LoadMigrations("db")
	if err != nil {
		logger.Fatal("load migrations", "error", err.Error())
	}

	logger.Info("Starting database migration process...")
	err = db.Migrate(db.GetDB().DB, migrations, sqlLogger)
	if err != nil {
		logger.Fatal("apply migrations", "error", err.Error())
	}

	logger.Info("Migrations applied successfully")

	logger.Info("Preparing statements...")
	err = db.GetDB().PrepareStatements()
	if err != nil {
		logger.Fatal("prepare statements", "error", err.Error())
	}

	logger.Info("Statements prepared successfully")
}
