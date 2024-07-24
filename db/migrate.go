package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	_ "github.com/mattn/go-sqlite3"
)

type Migration struct {
	Version int
	Schema  string
	Migrate string
}

func LoadMigrations(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var migrations []Migration
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		version, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		schemaPath := filepath.Join(dir, entry.Name(), "schema.sql")
		migratePath := filepath.Join(dir, entry.Name(), "migrate.sql")

		schema, err := os.ReadFile(schemaPath)
		if err != nil {
			return nil, err
		}

		var migrate []byte
		if _, err := os.Stat(migratePath); err == nil {
			migrate, err = os.ReadFile(migratePath)
			if err != nil {
				return nil, err
			}
		}

		migrations = append(migrations, Migration{
			Version: version,
			Schema:  string(schema),
			Migrate: string(migrate),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

func Migrate(db *sql.DB, migrations []Migration, logger *log.Logger) error {
	var currentVersion int
	err := db.QueryRow("PRAGMA user_version").Scan(&currentVersion)
	if err != nil {
		return err
	}

	logger.Info("Current database version", "version", currentVersion)

	for _, migration := range migrations {
		if migration.Version <= currentVersion {
			logger.Info(
				"Skipping migration (already applied)",
				"version",
				migration.Version,
			)
			continue
		}

		var confirm bool
		err := huh.NewConfirm().
			Title(fmt.Sprintf("New migration found (version %d)", migration.Version)).
			Description("Do you want to apply this migration?").
			Value(&confirm).
			Run()

		if err != nil {
			return fmt.Errorf("error getting user confirmation: %w", err)
		}

		if !confirm {
			logger.Info("Migration skipped", "version", migration.Version)
			continue
		}

		logger.Info("Applying migration", "version", migration.Version)

		tx, err := db.Begin()
		if err != nil {
			return err
		}

		if migration.Migrate != "" {
			_, err = tx.Exec(migration.Migrate)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("error executing migrate script: %w", err)
			}
		}

		_, err = tx.Exec(migration.Schema)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("error executing schema script: %w", err)
		}

		_, err = tx.Exec(
			fmt.Sprintf("PRAGMA user_version = %d", migration.Version),
		)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("error updating user_version: %w", err)
		}

		err = tx.Commit()
		if err != nil {
			return fmt.Errorf("error committing transaction: %w", err)
		}

		logger.Info("Successfully migrated", "version", migration.Version)
	}

	logger.Info("Migration process completed")
	return nil
}
