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
	db, err = sql.Open("sqlite3", "./001.db")
	if err != nil {
		log.Fatal(err)
	}
}

func GetDB() *sql.DB {
	return db
}

func CreateStream(
	id string,
	packetSeqOffset int,
	sampleIdxOffset int,
) error {
	stmt, err := db.Prepare(`
		INSERT INTO streams (
			id,
			packet_seq_offset,
			sample_idx_offset
		) VALUES (?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(
		id,
		packetSeqOffset,
		sampleIdxOffset,
	)
	return err
}

func SavePacket(
	id string,
	stream string,
	packetSeq int,
	sampleIdx int,
	payload []byte,
) error {
	stmt, err := db.Prepare(`
		INSERT INTO packets (
			id,
			stream,
			packet_seq,
			sample_idx,
			payload
		) VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(
		id,
		stream,
		packetSeq,
		sampleIdx,
		payload,
	)
	return err
}

func CreateSpeaker(id, stream, emoji string) error {
	stmt, err := db.Prepare(`
		INSERT INTO speakers (
			id,
			stream,
			emoji
		) VALUES (?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(id, stream, emoji)
	return err
}

func CreateDiscordSpeaker(id, speaker, discordID string) error {
	stmt, err := db.Prepare(`
		INSERT INTO discord_speakers (
			id,
			speaker,
			discord_id
		) VALUES (?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(id, speaker, discordID)
	return err
}

func CreateDiscordChannelStream(id, stream, discordGuild, discordChannel string) error {
	stmt, err := db.Prepare(`
		INSERT INTO discord_channel_streams (
			id,
			stream,
			discord_guild,
			discord_channel
		) VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(id, stream, discordGuild, discordChannel)
	return err
}

func CreateAttribution(id, stream, speaker string) error {
	stmt, err := db.Prepare(`
		INSERT INTO attributions (
			id,
			stream,
			speaker
		) VALUES (?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(id, stream, speaker)
	return err
}

func SaveRecognition(id, stream string, sampleIdx, sampleLen int, text string, confidence float64) error {
	stmt, err := db.Prepare(`
		INSERT INTO recognitions (
			id,
			stream,
			sample_idx,
			sample_len,
			text,
			confidence
		) VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(id, stream, sampleIdx, sampleLen, text, confidence)
	return err
}

func Close() {
	if db != nil {
		db.Close()
	}
}
