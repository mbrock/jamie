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
}
