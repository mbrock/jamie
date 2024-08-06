package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	_ "github.com/lib/pq"
	"github.com/spf13/viper"
	"node.town/config"
	"node.town/db"
)

func RunSetup() {
	log.Info("Starting Jamie setup...")

	// Check database connection
	dbURL := viper.GetString("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://jamie:jamie@localhost:5432/jamie"
	}

	dbPool, dbQueries, err := db.OpenDatabase(false)
	if err != nil {
		log.Error("Failed to connect to database", "error", err)
		createDB := false
		huh.NewConfirm().
			Title("Do you want to create the database?").
			Value(&createDB).
			Run()

		if createDB {
			if err := createDatabase(); err != nil {
				log.Fatal("Failed to create database", "error", err)
			}
			// Try to open the database again after creation
			dbPool, dbQueries, err = db.OpenDatabase(true)
			if err != nil {
				log.Fatal("Failed to connect to the newly created database", "error", err)
			}
		} else {
			log.Fatal("Database connection is required to continue")
		}
	}
	defer dbPool.Close()

	log.Info("Successfully connected to the database")

	// Initialize config
	cfg := config.New(dbQueries)

	// Prompt for API keys and tokens
	var discordToken, geminiAPIKey, speechmaticsAPIKey string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Enter your Discord Bot Token").
				Value(&discordToken),
			huh.NewInput().
				Title("Enter your Google Cloud (Gemini) API Key").
				Value(&geminiAPIKey),
			huh.NewInput().
				Title("Enter your Speechmatics API Key").
				Value(&speechmaticsAPIKey),
		),
	)

	err = form.Run()
	if err != nil {
		log.Fatal("Error during setup", "error", err)
	}

	// Save the configuration
	ctx := context.Background()
	err = cfg.Set(ctx, "DISCORD_TOKEN", discordToken)
	if err != nil {
		log.Fatal("Error saving Discord token", "error", err)
	}
	err = cfg.Set(ctx, "GEMINI_API_KEY", geminiAPIKey)
	if err != nil {
		log.Fatal("Error saving Gemini API key", "error", err)
	}
	err = cfg.Set(ctx, "SPEECHMATICS_API_KEY", speechmaticsAPIKey)
	if err != nil {
		log.Fatal("Error saving Speechmatics API key", "error", err)
	}

	log.Info("Setup completed successfully!")
}

func createDatabase() error {
	log.Info("Creating database...")

	cmd := exec.Command("createdb", "jamie")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}

	log.Info("Database created successfully")

	// Initialize the database schema
	log.Info("Initializing database schema...")

	cmd = exec.Command("psql", "-d", "jamie", "-f", "db/db_init.sql")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to initialize database schema: %w", err)
	}

	log.Info("Database schema initialized successfully")

	return nil
}
