package db

import (
	"database/sql"
	"embed"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaFS embed.FS

var db *sql.DB

func InitDB() {
	var err error
	db, err = sql.Open("sqlite3", "./transcripts.db")
	if err != nil {
		log.Fatal(err)
	}

	schemaSQL, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(string(schemaSQL))
	if err != nil {
		log.Fatal(err)
	}
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

func GetVoiceStream(ssrc uint32) (string, error) {
	var streamID string
	err := db.QueryRow(`
		SELECT stream_id 
		FROM discord_voice_stream 
		WHERE ssrc = ?
	`, ssrc).Scan(&streamID)
	if err != nil {
		return "", err
	}
	return streamID, nil
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

func GetNewTranscripts(
	guildID, channelID string,
	lastTimestamp time.Time,
) ([]string, error) {
	rows, err := db.Query(`
		SELECT transcript 
		FROM transcripts 
		WHERE guild_id = ? 
		AND channel_id = ? 
		AND timestamp > ? 
		ORDER BY timestamp
	`, guildID, channelID, lastTimestamp)
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
	err := db.QueryRow(`
		SELECT COALESCE(MAX(timestamp), '1970-01-01') 
		FROM transcripts 
		WHERE guild_id = ? 
		AND channel_id = ?
	`, guildID, channelID).Scan(&lastTimestampStr)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse("2006-01-02 15:04:05.999999-07:00", lastTimestampStr)
}

func Close() {
	if db != nil {
		db.Close()
	}
}

func GetAllTranscripts(guildID, channelID string) ([]string, error) {
	rows, err := db.Query(`
		SELECT transcript 
		FROM transcripts 
		WHERE guild_id = ? 
		AND channel_id = ? 
		ORDER BY timestamp
	`, guildID, channelID)
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
