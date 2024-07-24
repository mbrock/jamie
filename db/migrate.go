package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

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

func Migrate(db *sql.DB, migrations []Migration) error {
	var currentVersion int
	err := db.QueryRow("PRAGMA user_version").Scan(&currentVersion)
	if err != nil {
		return err
	}

	fmt.Printf("Current database version: %d\n", currentVersion)

	for _, migration := range migrations {
		if migration.Version <= currentVersion {
			fmt.Printf("Skipping migration %d (already applied)\n", migration.Version)
			continue
		}

		fmt.Printf("New migration found: version %d\n", migration.Version)
		fmt.Print("Do you want to apply this migration? (y/n): ")
		var response string
		_, err := fmt.Scanln(&response)
		if err != nil {
			return fmt.Errorf("error reading user input: %w", err)
		}

		if response != "y" && response != "Y" {
			fmt.Println("Migration skipped.")
			continue
		}

		fmt.Printf("Applying migration %d...\n", migration.Version)

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

		_, err = tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", migration.Version))
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("error updating user_version: %w", err)
		}

		err = tx.Commit()
		if err != nil {
			return fmt.Errorf("error committing transaction: %w", err)
		}

		fmt.Printf("Successfully migrated to version %d\n", migration.Version)
	}

	fmt.Println("Migration process completed.")
	return nil
}
