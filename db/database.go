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
