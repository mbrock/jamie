package db

import (
	"database/sql"
	"embed"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed 001/schema.sql
var schemaFS embed.FS

var db *sql.DB

func InitDB() {
	var err error
	db, err = sql.Open("sqlite3", "./002.db")
	if err != nil {
		log.Fatal(err)
	}
}

func GetDB() *sql.DB {
	return db
}

func CreateVoiceStream(
	guildID, channelID, streamID, userID string,
	ssrc uint32,
	firstOpusTimestamp uint32,
	firstReceiveTime int64,
	firstSequence uint16,
) error {
	stmt, err := db.Prepare(`
		INSERT INTO discord_voice_stream (
			guild_id,
			channel_id,
			stream_id,
			ssrc,
			user_id,
			first_opus_timestamp,
			first_receive_time,
			first_sequence
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(
		guildID,
		channelID,
		streamID,
		ssrc,
		userID,
		firstOpusTimestamp,
		firstReceiveTime,
		firstSequence,
	)
	return err
}

func SaveDiscordVoicePacket(
	streamID string,
	packet []byte,
	relativeSequence uint16,
	relativeOpusTimestamp uint32,
	receiveTime int64,
) error {
	stmt, err := db.Prepare(`
		INSERT INTO discord_voice_packet (
			stream_id, 
			packet, 
			relative_sequence, 
			relative_opus_timestamp, 
			receive_time
		) VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(
		streamID,
		packet,
		relativeSequence,
		relativeOpusTimestamp,
		receiveTime,
	)
	return err
}

func SaveTranscript(guildID, channelID, transcript string) error {
	stmt, err := db.Prepare(`
		INSERT INTO transcripts (
			guild_id, 
			channel_id, 
			transcript, 
			timestamp
		) VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(guildID, channelID, transcript, time.Now())
	return err
}

func Close() {
	if db != nil {
		db.Close()
	}
}
