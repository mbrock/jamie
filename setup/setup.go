package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	"node.town/config"
	"node.town/db"
)

func RunSetup() {
	log.Info("Starting Jamie setup...")

	// Check if running as root
	currentUser, err := user.Current()
	if err != nil {
		log.Fatal("Failed to get current user", "error", err)
	}
	isRoot := currentUser.Uid == "0"

	// Prompt for database setup options
	var createJamieUser, useSudo bool
	var dbOwner string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Do you want to create a 'jamie' system user?").
				Value(&createJamieUser),
			huh.NewConfirm().
				Title("Do you want to use sudo for database operations?").
				Value(&useSudo),
			huh.NewInput().
				Title("Enter the desired database owner (default: current user)").
				Value(&dbOwner),
		),
	)

	err = form.Run()
	if err != nil {
		log.Fatal("Error during setup", "error", err)
	}

	if dbOwner == "" {
		dbOwner = currentUser.Username
	}

	// Create 'jamie' user if requested
	if createJamieUser {
		if err := createSystemUser("jamie", useSudo); err != nil {
			log.Fatal("Failed to create 'jamie' user", "error", err)
		}
	}

	// Initialize database connection
	dbPool, dbQueries, err := db.OpenDatabase(false)
	if err != nil {
		log.Error("Failed to connect to database", "error", err)
		createDB := false
		err := huh.NewConfirm().
			Title("Do you want to create the database?").
			Value(&createDB).
			Run()
		if err != nil {
			log.Fatal("Error during setup", "error", err)
			return
		}

		if createDB {
			if err := createDatabase(dbOwner, useSudo); err != nil {
				log.Fatal("Failed to create database", "error", err)
			}
			// Try to open the database again after creation
			dbPool, dbQueries, err = db.OpenDatabase(true)
			if err != nil {
				log.Fatal(
					"Failed to connect to the newly created database",
					"error",
					err,
				)
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

	form = huh.NewForm(
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

func createSystemUser(username string, useSudo bool) error {
	log.Info("Creating system user", "username", username)

	var cmd *exec.Cmd
	if useSudo {
		cmd = exec.Command("sudo", "useradd", "-r", "-s", "/bin/false", username)
	} else {
		cmd = exec.Command("useradd", "-r", "-s", "/bin/false", username)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to create system user: %w", err)
	}

	log.Info("System user created successfully", "username", username)
	return nil
}

func createDatabase(owner string, useSudo bool) error {
	log.Info("Creating database...")

	var cmd *exec.Cmd
	if useSudo {
		cmd = exec.Command("sudo", "-u", "postgres", "createdb", "-O", owner, "jamie")
	} else {
		cmd = exec.Command("createdb", "-O", owner, "jamie")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}

	log.Info("Database created successfully")

	// Initialize the database schema
	log.Info("Initializing database schema...")

	if useSudo {
		cmd = exec.Command("sudo", "-u", owner, "psql", "-d", "jamie", "-f", "db/db_init.sql")
	} else {
		cmd = exec.Command("psql", "-d", "jamie", "-f", "db/db_init.sql")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to initialize database schema: %w", err)
	}

	log.Info("Database schema initialized successfully")

	return nil
}

func main() {
	RunSetup()
}
