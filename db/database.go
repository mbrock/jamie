package db

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func InitDB() {
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

func SaveTranscript(guildID, channelID, transcript string) error {
	stmt, err := db.Prepare("INSERT INTO transcripts(guild_id, channel_id, transcript, timestamp) VALUES(?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(guildID, channelID, transcript, time.Now())
	return err
}

func GetNewTranscripts(guildID, channelID string, lastTimestamp time.Time) ([]string, error) {
	rows, err := db.Query("SELECT transcript FROM transcripts WHERE guild_id = ? AND channel_id = ? AND timestamp > ? ORDER BY timestamp", guildID, channelID, lastTimestamp)
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

func GetLastTimestamp(guildID, channelID string) (time.Time, error) {
	var lastTimestampStr string
	err := db.QueryRow("SELECT COALESCE(MAX(timestamp), '1970-01-01') FROM transcripts WHERE guild_id = ? AND channel_id = ?", guildID, channelID).Scan(&lastTimestampStr)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse("2006-01-02 15:04:05.999999999-07:00", lastTimestampStr)
}

func Close() {
	if db != nil {
		db.Close()
	}
}
