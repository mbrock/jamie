package db

import (
	"database/sql"
	"log"

	"jamie/etc"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
	stmts map[string]*sql.Stmt
}

var db *DB

func InitDB() {
	var err error
	sqlDB, err := sql.Open("sqlite3", "./001.db")
	if err != nil {
		log.Fatal(err)
	}

	db = &DB{
		DB:    sqlDB,
		stmts: make(map[string]*sql.Stmt),
	}

	err = db.prepareStatements()
	if err != nil {
		log.Fatal(err)
	}
}

func (db *DB) prepareStatements() error {
	statements := map[string]string{
		"createStream": `
			INSERT INTO streams (
				id,
				packet_seq_offset,
				sample_idx_offset
			) VALUES (?, ?, ?)`,
		"savePacket": `
			INSERT INTO packets (
				id,
				stream,
				packet_seq,
				sample_idx,
				payload
			) VALUES (?, ?, ?, ?, ?)`,
		"createSpeaker": `
			INSERT INTO speakers (
				id,
				stream,
				emoji
			) VALUES (?, ?, ?)`,
		"createDiscordSpeaker": `
			INSERT INTO discord_speakers (
				id,
				speaker,
				discord_id
			) VALUES (?, ?, ?)`,
		"createDiscordChannelStream": `
			INSERT INTO discord_channel_streams (
				id,
				stream,
				discord_guild,
				discord_channel
			) VALUES (?, ?, ?, ?)`,
		"createAttribution": `
			INSERT INTO attributions (
				id,
				stream,
				speaker
			) VALUES (?, ?, ?)`,
		"saveRecognition": `
			INSERT INTO recognitions (
				id,
				stream,
				sample_idx,
				sample_len,
				text,
				confidence
			) VALUES (?, ?, ?, ?, ?, ?)`,
		"getStreamForDiscordChannel": `
			SELECT s.id 
			FROM streams s
			JOIN discord_channel_streams dcs ON s.id = dcs.stream
			WHERE dcs.discord_guild = ? AND dcs.discord_channel = ?
			ORDER BY s.created_at DESC
			LIMIT 1`,
		"createStreamForDiscordChannel1": `
			INSERT INTO streams (id, packet_seq_offset, sample_idx_offset) VALUES (?, ?, ?)`,
		"createStreamForDiscordChannel2": `
			INSERT INTO discord_channel_streams (id, stream, discord_guild, discord_channel) VALUES (?, ?, ?, ?)`,
		"createSpeakerForStream": `
			INSERT INTO speakers (id, stream, emoji) VALUES (?, ?, ?)`,
		"checkSpeechRecognitionSessionExists": `
			SELECT EXISTS(SELECT 1 FROM speech_recognition_sessions WHERE stream = ?)`,
		"saveSpeechRecognitionSession": `
			INSERT INTO speech_recognition_sessions (stream, session_data) VALUES (?, ?)`,
		"getChannelAndEmojiForStream": `
			SELECT dcs.discord_channel, s.emoji 
			FROM discord_channel_streams dcs
			JOIN streams st ON dcs.stream = st.id
			JOIN speakers s ON st.id = s.stream
			WHERE st.id = ?`,
		"updateSpeakerEmoji": `
			UPDATE speakers SET emoji = ? WHERE stream = ?`,
		"getChannelIDForStream": `
			SELECT discord_channel FROM discord_channel_streams WHERE stream = ?`,
		"endStreamForChannel": `
			UPDATE streams s
			SET ended_at = CURRENT_TIMESTAMP
			WHERE s.id IN (
				SELECT dcs.stream
				FROM discord_channel_streams dcs
				WHERE dcs.discord_guild = ? AND dcs.discord_channel = ?
			) AND s.ended_at IS NULL`,
	}

	for name, query := range statements {
		stmt, err := db.Prepare(query)
		if err != nil {
			return err
		}
		db.stmts[name] = stmt
	}

	return nil
}

func GetDB() *DB {
	return db
}

func CreateStream(id string, packetSeqOffset int, sampleIdxOffset int) error {
	_, err := db.stmts["createStream"].Exec(id, packetSeqOffset, sampleIdxOffset)
	return err
}

func SavePacket(id string, stream string, packetSeq int, sampleIdx int, payload []byte) error {
	_, err := db.stmts["savePacket"].Exec(id, stream, packetSeq, sampleIdx, payload)
	return err
}

func CreateSpeaker(id, stream, emoji string) error {
	_, err := db.stmts["createSpeaker"].Exec(id, stream, emoji)
	return err
}

func CreateDiscordSpeaker(id, speaker, discordID string) error {
	_, err := db.stmts["createDiscordSpeaker"].Exec(id, speaker, discordID)
	return err
}

func CreateDiscordChannelStream(id, stream, discordGuild, discordChannel string) error {
	_, err := db.stmts["createDiscordChannelStream"].Exec(id, stream, discordGuild, discordChannel)
	return err
}

func CreateAttribution(id, stream, speaker string) error {
	_, err := db.stmts["createAttribution"].Exec(id, stream, speaker)
	return err
}

func SaveRecognition(id, stream string, sampleIdx, sampleLen int, text string, confidence float64) error {
	_, err := db.stmts["saveRecognition"].Exec(id, stream, sampleIdx, sampleLen, text, confidence)
	return err
}

func Close() {
	for _, stmt := range db.stmts {
		stmt.Close()
	}
	if db.DB != nil {
		db.DB.Close()
	}
}

func GetStreamForDiscordChannel(guildID, channelID string) (string, error) {
	var streamID string
	err := db.stmts["getStreamForDiscordChannel"].QueryRow(guildID, channelID).Scan(&streamID)
	return streamID, err
}

func CreateStreamForDiscordChannel(streamID, guildID, channelID string, packetSequence, packetTimestamp uint16) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Stmt(db.stmts["createStreamForDiscordChannel1"]).Exec(streamID, packetSequence, packetTimestamp)
	if err != nil {
		return err
	}

	_, err = tx.Stmt(db.stmts["createStreamForDiscordChannel2"]).Exec(etc.Gensym(), streamID, guildID, channelID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func CreateSpeakerForStream(speakerID, streamID, emoji string) error {
	_, err := db.stmts["createSpeakerForStream"].Exec(speakerID, streamID, emoji)
	return err
}

func CheckSpeechRecognitionSessionExists(streamID string) (bool, error) {
	var exists bool
	err := db.stmts["checkSpeechRecognitionSessionExists"].QueryRow(streamID).Scan(&exists)
	return exists, err
}

func SaveSpeechRecognitionSession(streamID, sessionData string) error {
	_, err := db.stmts["saveSpeechRecognitionSession"].Exec(streamID, sessionData)
	return err
}

func GetChannelAndEmojiForStream(streamID string) (string, string, error) {
	var channelID, emoji string
	err := db.stmts["getChannelAndEmojiForStream"].QueryRow(streamID).Scan(&channelID, &emoji)
	return channelID, emoji, err
}

func UpdateSpeakerEmoji(streamID, newEmoji string) error {
	_, err := db.stmts["updateSpeakerEmoji"].Exec(newEmoji, streamID)
	return err
}

func GetChannelIDForStream(streamID string) (string, error) {
	var channelID string
	err := db.stmts["getChannelIDForStream"].QueryRow(streamID).Scan(&channelID)
	return channelID, err
}

func EndStreamForChannel(guildID, channelID string) error {
	_, err := db.stmts["endStreamForChannel"].Exec(guildID, channelID)
	return err
}
