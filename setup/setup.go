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

const (
	jamieUsername = "jamie"
	jamiePassword = "jamie"
	dbName        = "jamie"
)

func RunSetup() {
	log.Info("Starting Jamie setup...")

	// Check if running as root
	currentUser, err := user.Current()
	if err != nil {
		log.Fatal("Failed to get current user", "error", err)
	}
	isRoot := currentUser.Uid == "0"

	// Check and create system user if needed
	if err := ensureSystemUser(jamieUsername); err != nil {
		log.Fatal("Failed to ensure system user", "error", err)
	}

	// Check and create database user if needed
	if err := ensureDatabaseUser(jamieUsername, jamiePassword); err != nil {
		log.Fatal("Failed to ensure database user", "error", err)
	}

	// Check and create database if needed
	if err := ensureDatabase(dbName, jamieUsername); err != nil {
		log.Fatal("Failed to ensure database", "error", err)
	}

	// Initialize database connection
	dbPool, dbQueries, err := db.OpenDatabase(true)
	if err != nil {
		log.Fatal("Failed to connect to the database", "error", err)
	}
	defer dbPool.Close()

	log.Info("Successfully connected to the database")

	// Initialize config
	cfg := config.New(dbQueries)

	// Prompt for API keys and tokens
	apiKeys, err := promptForAPIKeys()
	if err != nil {
		log.Fatal("Error during API key setup", "error", err)
	}

	// Save the configuration
	if err := saveConfiguration(cfg, apiKeys); err != nil {
		log.Fatal("Error saving configuration", "error", err)
	}

	log.Info("Setup completed successfully!")
}

func ensureSystemUser(username string) error {
	exists, err := systemUserExists(username)
	if err != nil {
		return fmt.Errorf("failed to check system user: %w", err)
	}

	if !exists {
		log.Info("Creating system user", "username", username)
		cmd := exec.Command("useradd", "-r", "-s", "/bin/false", username)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() != 0 {
				// Retry with sudo if the command failed
				log.Warn("Failed to create user, retrying with sudo")
				cmd = exec.Command("sudo", "useradd", "-r", "-s", "/bin/false", username)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("failed to create system user with sudo: %w", err)
				}
			} else {
				return fmt.Errorf("failed to create system user: %w", err)
			}
		}
		log.Info("System user created successfully", "username", username)
	} else {
		log.Info("System user already exists", "username", username)
	}

	return nil
}

func ensureDatabaseUser(username, password string) error {
	exists, err := databaseUserExists(username)
	if err != nil {
		return fmt.Errorf("failed to check database user: %w", err)
	}

	if !exists {
		log.Info("Creating database user", "username", username)
		cmd := exec.Command("createuser", "-s", username)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() != 0 {
				// Retry with sudo if the command failed
				log.Warn("Failed to create database user, retrying with sudo")
				cmd = exec.Command("sudo", "-u", "postgres", "createuser", "-s", username)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("failed to create database user with sudo: %w", err)
				}
			} else {
				return fmt.Errorf("failed to create database user: %w", err)
			}
		}

		// Set password for the user
		alterCmd := exec.Command("psql", "-c", fmt.Sprintf("ALTER USER %s WITH PASSWORD '%s';", username, password))
		alterCmd.Stdout = os.Stdout
		alterCmd.Stderr = os.Stderr

		if err := alterCmd.Run(); err != nil {
			if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() != 0 {
				// Retry with sudo if the command failed
				log.Warn("Failed to set database user password, retrying with sudo")
				alterCmd = exec.Command("sudo", "-u", "postgres", "psql", "-c", fmt.Sprintf("ALTER USER %s WITH PASSWORD '%s';", username, password))
				alterCmd.Stdout = os.Stdout
				alterCmd.Stderr = os.Stderr
				if err := alterCmd.Run(); err != nil {
					return fmt.Errorf("failed to set database user password with sudo: %w", err)
				}
			} else {
				return fmt.Errorf("failed to set database user password: %w", err)
			}
		}

		log.Info("Database user created successfully", "username", username)
	} else {
		log.Info("Database user already exists", "username", username)
	}

	return nil
}

func ensureDatabase(dbName, owner string) error {
	exists, err := databaseExists(dbName)
	if err != nil {
		return fmt.Errorf("failed to check database: %w", err)
	}

	if !exists {
		log.Info("Creating database", "name", dbName)
		cmd := exec.Command("createdb", "-O", owner, dbName)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() != 0 {
				// Retry with sudo if the command failed
				log.Warn("Failed to create database, retrying with sudo")
				cmd = exec.Command("sudo", "-u", "postgres", "createdb", "-O", owner, dbName)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("failed to create database with sudo: %w", err)
				}
			} else {
				return fmt.Errorf("failed to create database: %w", err)
			}
		}

		log.Info("Database created successfully", "name", dbName)

		// Initialize the database schema
		log.Info("Initializing database schema...")
		schemaCmd := exec.Command("psql", "-d", dbName, "-f", "db/db_init.sql")
		schemaCmd.Stdout = os.Stdout
		schemaCmd.Stderr = os.Stderr

		if err := schemaCmd.Run(); err != nil {
			if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() != 0 {
				// Retry with sudo if the command failed
				log.Warn("Failed to initialize database schema, retrying with sudo")
				schemaCmd = exec.Command("sudo", "-u", owner, "psql", "-d", dbName, "-f", "db/db_init.sql")
				schemaCmd.Stdout = os.Stdout
				schemaCmd.Stderr = os.Stderr
				if err := schemaCmd.Run(); err != nil {
					return fmt.Errorf("failed to initialize database schema with sudo: %w", err)
				}
			} else {
				return fmt.Errorf("failed to initialize database schema: %w", err)
			}
		}

		log.Info("Database schema initialized successfully")
	} else {
		log.Info("Database already exists", "name", dbName)
	}

	return nil
}

func systemUserExists(username string) (bool, error) {
	_, err := user.Lookup(username)
	if err != nil {
		if _, ok := err.(user.UnknownUserError); ok {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func databaseUserExists(username string) (bool, error) {
	cmd := exec.Command("psql", "-tAc", fmt.Sprintf("SELECT 1 FROM pg_roles WHERE rolname='%s'", username))
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check database user: %w", err)
	}
	return string(output) == "1\n", nil
}

func databaseExists(dbName string) (bool, error) {
	cmd := exec.Command("psql", "-lqt", "|", "cut", "-d", "|", "-f", "1", "|", "grep", "-cw", dbName)
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check database: %w", err)
	}
	return string(output) == "1\n", nil
}

func promptForAPIKeys() (map[string]string, error) {
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

	err := form.Run()
	if err != nil {
		return nil, fmt.Errorf("error during API key setup: %w", err)
	}

	return map[string]string{
		"DISCORD_TOKEN":      discordToken,
		"GEMINI_API_KEY":     geminiAPIKey,
		"SPEECHMATICS_API_KEY": speechmaticsAPIKey,
	}, nil
}

func saveConfiguration(cfg *config.Config, apiKeys map[string]string) error {
	ctx := context.Background()
	for key, value := range apiKeys {
		if err := cfg.Set(ctx, key, value); err != nil {
			return fmt.Errorf("error saving %s: %w", key, err)
		}
	}
	return nil
}

func main() {
	RunSetup()
}
