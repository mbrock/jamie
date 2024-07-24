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

	for _, migration := range migrations {
		if migration.Version <= currentVersion {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}

		if migration.Migrate != "" {
			_, err = tx.Exec(migration.Migrate)
			if err != nil {
				err := tx.Rollback()
				if err != nil {
					return err
				}
				return err
			}
		}

		_, err = tx.Exec(migration.Schema)
		if err != nil {
			err := tx.Rollback()
			if err != nil {
				return err
			}
			return err
		}

		_, err = tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", migration.Version))
		if err != nil {
			err := tx.Rollback()
			if err != nil {
				return err
			}
			return err
		}

		err = tx.Commit()
		if err != nil {
			return err
		}

		fmt.Printf("Migrated to version %d\n", migration.Version)
	}

	return nil
}
