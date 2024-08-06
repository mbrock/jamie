package setup

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	_ "github.com/lib/pq"
	"github.com/spf13/viper"
)

func RunSetup() {
	log.Info("Starting Jamie setup...")

	// Check database connection
	dbURL := viper.GetString("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://jamie:jamie@localhost:5432/jamie"
	}

	db, err := sql.Open("postgres", dbURL)
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
		} else {
			log.Fatal("Database connection is required to continue")
		}
	} else {
		defer db.Close()
		log.Info("Successfully connected to the database")
	}

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
	viper.Set("DISCORD_TOKEN", discordToken)
	viper.Set("GEMINI_API_KEY", geminiAPIKey)
	viper.Set("SPEECHMATICS_API_KEY", speechmaticsAPIKey)

	err = viper.WriteConfig()
	if err != nil {
		log.Fatal("Error saving configuration", "error", err)
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
