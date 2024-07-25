package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	_ "github.com/mattn/go-sqlite3"
)

type Migration struct {
	ID          string
	Description string
	Up          func(*sql.Tx) error
	Down        func(*sql.Tx) error
}

var migrations = []Migration{
	{
		ID:          "001_initial_schema",
		Description: "Create initial schema",
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS streams (
					id TEXT PRIMARY KEY,
					packet_seq_offset INTEGER,
					sample_idx_offset INTEGER,
					created_at REAL DEFAULT (julianday('now')),
					ended_at REAL
				);

				CREATE TABLE IF NOT EXISTS packets (
					id TEXT PRIMARY KEY,
					stream TEXT,
					packet_seq INTEGER,
					sample_idx INTEGER,
					payload BLOB,
					received_at REAL DEFAULT (julianday('now')),
					FOREIGN KEY (stream) REFERENCES streams(id)
				);

				CREATE TABLE IF NOT EXISTS speakers (
					id TEXT PRIMARY KEY,
					stream TEXT,
					emoji TEXT,
					created_at REAL DEFAULT (julianday('now')),
					FOREIGN KEY (stream) REFERENCES streams(id)
				);

				CREATE TABLE IF NOT EXISTS discord_speakers (
					id TEXT PRIMARY KEY,
					speaker TEXT,
					discord_id TEXT,
					created_at REAL DEFAULT (julianday('now')),
					FOREIGN KEY (speaker) REFERENCES speakers(id)
				);

				CREATE TABLE IF NOT EXISTS discord_channel_streams (
					id TEXT PRIMARY KEY,
					stream TEXT,
					discord_guild TEXT,
					discord_channel TEXT,
					created_at REAL DEFAULT (julianday('now')),
					FOREIGN KEY (stream) REFERENCES streams(id)
				);

				CREATE TABLE IF NOT EXISTS attributions (
					id TEXT PRIMARY KEY,
					stream TEXT,
					speaker TEXT,
					created_at REAL DEFAULT (julianday('now')),
					FOREIGN KEY (stream) REFERENCES streams(id),
					FOREIGN KEY (speaker) REFERENCES speakers(id)
				);

				CREATE TABLE IF NOT EXISTS recognitions (
					id TEXT PRIMARY KEY,
					stream TEXT,
					sample_idx INTEGER,
					sample_len INTEGER,
					text TEXT,
					confidence REAL,
					created_at REAL DEFAULT (julianday('now')),
					FOREIGN KEY (stream) REFERENCES streams(id)
				);

				CREATE TABLE IF NOT EXISTS speech_recognition_sessions (
					stream TEXT PRIMARY KEY,
					session_data TEXT,
					created_at REAL DEFAULT (julianday('now')),
					FOREIGN KEY (stream) REFERENCES streams(id)
				);

				CREATE TABLE IF NOT EXISTS migration_history (
					id TEXT PRIMARY KEY,
					applied_at REAL DEFAULT (julianday('now'))
				);
			`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			_, err := tx.Exec(`
				DROP TABLE IF EXISTS speech_recognition_sessions;
				DROP TABLE IF EXISTS recognitions;
				DROP TABLE IF EXISTS attributions;
				DROP TABLE IF EXISTS discord_channel_streams;
				DROP TABLE IF EXISTS discord_speakers;
				DROP TABLE IF EXISTS speakers;
				DROP TABLE IF EXISTS packets;
				DROP TABLE IF EXISTS streams;
				DROP TABLE IF EXISTS migration_history;
			`)
			return err
		},
	},
	{
		ID:          "002_update_timestamp_columns",
		Description: "Update timestamp columns to use REAL type",
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`
				-- Update streams table
				ALTER TABLE streams ADD COLUMN created_at_new REAL DEFAULT (julianday('now'));
				UPDATE streams SET created_at_new = created_at;
				ALTER TABLE streams DROP COLUMN created_at;
				ALTER TABLE streams RENAME COLUMN created_at_new TO created_at;

				-- Update packets table
				ALTER TABLE packets ADD COLUMN received_at_new REAL DEFAULT (julianday('now'));
				UPDATE packets SET received_at_new = received_at;
				ALTER TABLE packets DROP COLUMN received_at;
				ALTER TABLE packets RENAME COLUMN received_at_new TO received_at;

				-- Update speakers table
				ALTER TABLE speakers ADD COLUMN created_at_new REAL DEFAULT (julianday('now'));
				UPDATE speakers SET created_at_new = created_at;
				ALTER TABLE speakers DROP COLUMN created_at;
				ALTER TABLE speakers RENAME COLUMN created_at_new TO created_at;

				-- Update discord_speakers table
				ALTER TABLE discord_speakers ADD COLUMN created_at_new REAL DEFAULT (julianday('now'));
				UPDATE discord_speakers SET created_at_new = created_at;
				ALTER TABLE discord_speakers DROP COLUMN created_at;
				ALTER TABLE discord_speakers RENAME COLUMN created_at_new TO created_at;

				-- Update discord_channel_streams table
				ALTER TABLE discord_channel_streams ADD COLUMN created_at_new REAL DEFAULT (julianday('now'));
				UPDATE discord_channel_streams SET created_at_new = created_at;
				ALTER TABLE discord_channel_streams DROP COLUMN created_at;
				ALTER TABLE discord_channel_streams RENAME COLUMN created_at_new TO created_at;

				-- Update attributions table
				ALTER TABLE attributions ADD COLUMN created_at_new REAL DEFAULT (julianday('now'));
				UPDATE attributions SET created_at_new = created_at;
				ALTER TABLE attributions DROP COLUMN created_at;
				ALTER TABLE attributions RENAME COLUMN created_at_new TO created_at;

				-- Update recognitions table
				ALTER TABLE recognitions ADD COLUMN created_at_new REAL DEFAULT (julianday('now'));
				UPDATE recognitions SET created_at_new = created_at;
				ALTER TABLE recognitions DROP COLUMN created_at;
				ALTER TABLE recognitions RENAME COLUMN created_at_new TO created_at;

				-- Update speech_recognition_sessions table
				ALTER TABLE speech_recognition_sessions ADD COLUMN created_at_new REAL DEFAULT (julianday('now'));
				UPDATE speech_recognition_sessions SET created_at_new = created_at;
				ALTER TABLE speech_recognition_sessions DROP COLUMN created_at;
				ALTER TABLE speech_recognition_sessions RENAME COLUMN created_at_new TO created_at;
			`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			// This is a complex migration to revert, so we'll just log a warning
			log.Warn("Reverting migration 002_update_timestamp_columns is not supported")
			return nil
		},
	},
}

func Migrate(db *sql.DB, logger *log.Logger) error {
	// Create migration_history table if it doesn't exist
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS migration_history (
			id TEXT PRIMARY KEY,
			applied_at REAL DEFAULT (julianday('now'))
		)
	`)
	if err != nil {
		return fmt.Errorf("error creating migration_history table: %w", err)
	}

	for _, migration := range migrations {
		var applied bool
		err := db.QueryRow("SELECT 1 FROM migration_history WHERE id = ?", migration.ID).Scan(&applied)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("error checking migration status: %w", err)
		}

		if applied {
			logger.Info("Skipping migration (already applied)", "id", migration.ID)
			continue
		}

		var confirm bool
		err = huh.NewConfirm().
			Title(fmt.Sprintf("New migration found: %s", migration.ID)).
			Description(migration.Description).
			Value(&confirm).
			Run()

		if err != nil {
			return fmt.Errorf("error getting user confirmation: %w", err)
		}

		if !confirm {
			logger.Info("Migration skipped", "id", migration.ID)
			continue
		}

		logger.Info("Applying migration", "id", migration.ID)

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("error starting transaction: %w", err)
		}

		err = migration.Up(tx)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("error applying migration %s: %w", migration.ID, err)
		}

		_, err = tx.Exec("INSERT INTO migration_history (id, applied_at) VALUES (?, julianday('now'))", migration.ID)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("error recording migration %s: %w", migration.ID, err)
		}

		err = tx.Commit()
		if err != nil {
			return fmt.Errorf("error committing migration %s: %w", migration.ID, err)
		}

		logger.Info("Successfully applied migration", "id", migration.ID)
	}

	logger.Info("Migration process completed")
	return nil
}
