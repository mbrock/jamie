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

	createTranscriptsTable := `
	CREATE TABLE IF NOT EXISTS transcripts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		guild_id TEXT,
		channel_id TEXT,
		transcript TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

	createDiscordVoicePacketTable := `
	CREATE TABLE IF NOT EXISTS discord_voice_packet (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		stream_id TEXT,
		packet BLOB,
		sequence INTEGER,
		opus_timestamp INTEGER,
		FOREIGN KEY (stream_id) REFERENCES discord_voice_stream(stream_id)
	);
	`

	createVoiceStreamTable := `
	CREATE TABLE IF NOT EXISTS discord_voice_stream (
		stream_id TEXT PRIMARY KEY,
		guild_id TEXT,
		channel_id TEXT,
		ssrc INTEGER,
		user_id TEXT,
		first_opus_timestamp INTEGER,
		first_receive_time DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err = db.Exec(createTranscriptsTable)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(createDiscordVoicePacketTable)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(createVoiceStreamTable)
	if err != nil {
		log.Fatal(err)
	}
}

func CreateVoiceStream(guildID, channelID, streamID, userID string, ssrc uint32, firstOpusTimestamp uint32) error {
	stmt, err := db.Prepare("INSERT INTO discord_voice_stream(guild_id, channel_id, stream_id, ssrc, user_id, first_opus_timestamp) VALUES(?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(guildID, channelID, streamID, ssrc, userID, firstOpusTimestamp)
	return err
}

func GetVoiceStream(ssrc uint32) (string, error) {
	var streamID string
	err := db.QueryRow("SELECT stream_id FROM discord_voice_stream WHERE ssrc = ?", ssrc).Scan(&streamID)
	if err != nil {
		return "", err
	}
	return streamID, nil
}

func SaveDiscordVoicePacket(streamID string, packet []byte, sequence uint16, opusTimestamp uint32) error {
	stmt, err := db.Prepare("INSERT INTO discord_voice_packet(stream_id, packet, sequence, opus_timestamp) VALUES(?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(streamID, packet, sequence, opusTimestamp)
	return err
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
	return time.Parse("2006-01-02 15:04:05.999999-07:00", lastTimestampStr)
}

func Close() {
	if db != nil {
		db.Close()
	}
}

func GetAllTranscripts(guildID, channelID string) ([]string, error) {
	rows, err := db.Query("SELECT transcript FROM transcripts WHERE guild_id = ? AND channel_id = ? ORDER BY timestamp", guildID, channelID)
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
