package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func initDB() {
	var err error
	db, err = sql.Open("sqlite3", "./transcripts.db")
	if err != nil {
		log.Fatal(err)
	}

	createTable := `
	CREATE TABLE IF NOT EXISTS transcripts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		guild_id TEXT,
		channel_id TEXT,
		transcript TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err = db.Exec(createTable)
	if err != nil {
		log.Fatal(err)
	}
}

func saveTranscript(guildID, channelID, transcript string) error {
	stmt, err := db.Prepare("INSERT INTO transcripts(guild_id, channel_id, transcript) VALUES(?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(guildID, channelID, transcript)
	return err
}

func getTranscripts(guildID, channelID string) ([]string, error) {
	rows, err := db.Query("SELECT transcript FROM transcripts WHERE guild_id = ? AND channel_id = ? ORDER BY timestamp DESC LIMIT 100", guildID, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transcripts []string
	for rows.Next() {
		var transcript string
		if err := rows.Scan(&transcript); err != nil {
			return nil, err
		}
		transcripts = append(transcripts, transcript)
	}

	return transcripts, nil
}
