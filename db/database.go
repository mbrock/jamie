package db

import (
	"context"
	"database/sql"
	"fmt"

	"jamie/etc"

	"github.com/charmbracelet/log"
	_ "github.com/mattn/go-sqlite3"
)

var sqlLogger *log.Logger

type DB struct {
	*sql.DB
	stmts map[string]*sql.Stmt
}

var db *DB

func InitDB(logger *log.Logger) {
	var err error
	sqlDB, err := sql.Open("sqlite3", "./001.db")
	if err != nil {
		logger.Fatal(err)
	}

	db = &DB{
		DB:    sqlDB,
		stmts: make(map[string]*sql.Stmt),
	}

	sqlLogger = logger
}

func (db *DB) PrepareStatements() error {

	statements := map[string]string{
		"insertStream": `
			INSERT INTO streams (
				id,
				packet_seq_offset,
				sample_idx_offset
			) VALUES (?, ?, ?)`,
		"insertPacket": `
			INSERT INTO packets (
				id,
				stream,
				packet_seq,
				sample_idx,
				payload
			) VALUES (?, ?, ?, ?, ?)`,
		"insertSpeaker": `
			INSERT INTO speakers (
				id,
				stream,
				emoji
			) VALUES (?, ?, ?)`,
		"insertDiscordSpeaker": `
			INSERT INTO discord_speakers (
				id,
				speaker,
				discord_id
			) VALUES (?, ?, ?)`,
		"insertDiscordChannelStream": `
			INSERT INTO discord_channel_streams (
				id,
				stream,
				discord_guild,
				discord_channel
			) VALUES (?, ?, ?, ?)`,
		"insertAttribution": `
			INSERT INTO attributions (
				id,
				stream,
				speaker
			) VALUES (?, ?, ?)`,
		"insertRecognition": `
			INSERT INTO recognitions (
				id,
				stream,
				sample_idx,
				sample_len,
				text,
				confidence
			) VALUES (?, ?, ?, ?, ?, ?)`,
		"selectStreamForDiscordChannelAndSpeaker": `
			SELECT s.id 
			FROM streams s
			JOIN discord_channel_streams dcs ON s.id = dcs.stream
			JOIN discord_speakers ds ON s.id = ds.stream
			WHERE dcs.discord_guild = ? AND dcs.discord_channel = ? AND ds.discord_id = ?
			ORDER BY s.created_at DESC
			LIMIT 1`,
		"insertStreamForDiscordChannel": `
			INSERT INTO streams (id, packet_seq_offset, sample_idx_offset) VALUES (?, ?, ?)`,
		"insertDiscordChannelStreamForStream": `
			INSERT INTO discord_channel_streams (id, stream, discord_guild, discord_channel) VALUES (?, ?, ?, ?)`,
		"insertSpeakerForStream": `
			INSERT INTO speakers (id, stream, emoji) VALUES (?, ?, ?)`,
		"checkSpeechRecognitionSessionExists": `
			SELECT EXISTS(SELECT 1 FROM speech_recognition_sessions WHERE stream = ?)`,
		"insertSpeechRecognitionSession": `
			INSERT INTO speech_recognition_sessions (stream, session_data) VALUES (?, ?)`,
		"getSpeechRecognitionSession": `
			SELECT session_data FROM speech_recognition_sessions WHERE stream = ?`,
		"selectChannelAndEmojiForStream": `
			SELECT dcs.discord_channel, s.emoji 
			FROM discord_channel_streams dcs
			JOIN streams st ON dcs.stream = st.id
			JOIN speakers s ON st.id = s.stream
			WHERE st.id = ?`,
		"updateSpeakerEmoji": `
			UPDATE speakers SET emoji = ? WHERE stream = ?`,
		"selectChannelIDForStream": `
			SELECT discord_channel FROM discord_channel_streams WHERE stream = ?`,
		"updateStreamEndTimeForChannel": `
			UPDATE streams
			SET ended_at = CURRENT_TIMESTAMP
			WHERE id IN (
				SELECT stream
				FROM discord_channel_streams
				WHERE discord_guild = ? AND discord_channel = ?
			) AND ended_at IS NULL`,
	}

	for name, query := range statements {
		sqlLogger.Info("Preparing statement", "name", name)
		stmt, err := db.Prepare(query)
		if err != nil {
			sqlLogger.Error(
				"Failed to prepare statement",
				"name",
				name,
				"error",
				err,
			)
			return err
		}
		db.stmts[name] = stmt
		sqlLogger.Info("Statement prepared successfully", "name", name)
	}

	return nil
}

func GetDB() *DB {
	return db
}

func CreateStream(id string, packetSeqOffset int, sampleIdxOffset int) error {
	_, err := db.execWithLog(
		context.Background(),
		"insertStream",
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
	_, err := db.execWithLog(
		context.Background(),
		"insertPacket",
		id,
		stream,
		packetSeq,
		sampleIdx,
		payload,
	)
	return err
}

func CreateSpeaker(id, stream, emoji string) error {
	_, err := db.execWithLog(
		context.Background(),
		"insertSpeaker",
		id,
		stream,
		emoji,
	)
	return err
}

func CreateDiscordSpeaker(id, speaker, discordID string) error {
	_, err := db.execWithLog(
		context.Background(),
		"insertDiscordSpeaker",
		id,
		speaker,
		discordID,
	)
	return err
}

func CreateDiscordChannelStream(
	id, stream, discordGuild, discordChannel string,
) error {
	_, err := db.execWithLog(
		context.Background(),
		"insertDiscordChannelStream",
		id,
		stream,
		discordGuild,
		discordChannel,
	)
	return err
}

func CreateAttribution(id, stream, speaker string) error {
	_, err := db.execWithLog(
		context.Background(),
		"insertAttribution",
		id,
		stream,
		speaker,
	)
	return err
}

func SaveRecognition(
	id, stream string,
	sampleIdx, sampleLen int,
	text string,
	confidence float64,
) error {
	_, err := db.execWithLog(
		context.Background(),
		"insertRecognition",
		id,
		stream,
		sampleIdx,
		sampleLen,
		text,
		confidence,
	)
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

func (db *DB) execWithLog(
	ctx context.Context,
	stmtName string,
	args ...interface{},
) (sql.Result, error) {
	stmt := db.stmts[stmtName]
	sqlLogger.Debug("Executing SQL statement", "name", stmtName, "args", args)
	return stmt.ExecContext(ctx, args...)
}

func (db *DB) queryRowWithLog(
	ctx context.Context,
	stmtName string,
	args ...interface{},
) *sql.Row {
	stmt := db.stmts[stmtName]
	sqlLogger.Debug("Executing SQL query", "name", stmtName, "args", args)
	return stmt.QueryRowContext(ctx, args...)
}

func GetStreamForDiscordChannelAndSpeaker(guildID, channelID, discordID string) (string, error) {
	var streamID string
	row := db.queryRowWithLog(
		context.Background(),
		"selectStreamForDiscordChannelAndSpeaker",
		guildID,
		channelID,
		discordID,
	)
	if row == nil {
		return "", fmt.Errorf("no row returned from query")
	}
	err := row.Scan(&streamID)
	return streamID, err
}

func CreateStreamForDiscordChannel(
	streamID, guildID, channelID string,
	packetSequence, packetTimestamp uint16,
	speakerID, discordID, emoji string,
) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	sqlLogger.Debug(
		"Executing SQL statement in transaction",
		"name",
		"insertStreamForDiscordChannel",
		"args",
		[]interface{}{streamID, packetSequence, packetTimestamp},
	)
	_, err = tx.Stmt(db.stmts["insertStreamForDiscordChannel"]).
		Exec(streamID, packetSequence, packetTimestamp)
	if err != nil {
		return err
	}

	sqlLogger.Debug(
		"Executing SQL statement in transaction",
		"name",
		"insertDiscordChannelStreamForStream",
		"args",
		[]interface{}{etc.Gensym(), streamID, guildID, channelID},
	)
	_, err = tx.Stmt(db.stmts["insertDiscordChannelStreamForStream"]).
		Exec(etc.Gensym(), streamID, guildID, channelID)
	if err != nil {
		return err
	}

	sqlLogger.Debug(
		"Executing SQL statement in transaction",
		"name",
		"insertSpeakerForStream",
		"args",
		[]interface{}{speakerID, streamID, emoji},
	)
	_, err = tx.Stmt(db.stmts["insertSpeakerForStream"]).
		Exec(speakerID, streamID, emoji)
	if err != nil {
		return err
	}

	sqlLogger.Debug(
		"Executing SQL statement in transaction",
		"name",
		"insertDiscordSpeaker",
		"args",
		[]interface{}{etc.Gensym(), speakerID, discordID},
	)
	_, err = tx.Stmt(db.stmts["insertDiscordSpeaker"]).
		Exec(etc.Gensym(), speakerID, discordID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func CreateSpeakerForStream(speakerID, streamID, emoji string) error {
	_, err := db.execWithLog(
		context.Background(),
		"insertSpeakerForStream",
		speakerID,
		streamID,
		emoji,
	)
	return err
}

func CheckSpeechRecognitionSessionExists(streamID string) (bool, error) {
	var exists bool
	err := db.queryRowWithLog(context.Background(), "checkSpeechRecognitionSessionExists", streamID).
		Scan(&exists)
	return exists, err
}

func SaveSpeechRecognitionSession(streamID, sessionData string) error {
	_, err := db.execWithLog(
		context.Background(),
		"insertSpeechRecognitionSession",
		streamID,
		sessionData,
	)
	return err
}

func GetSpeechRecognitionSession(streamID string) (string, error) {
	var sessionData string
	err := db.queryRowWithLog(context.Background(), "getSpeechRecognitionSession", streamID).
		Scan(&sessionData)
	return sessionData, err
}

func GetChannelAndEmojiForStream(streamID string) (string, string, error) {
	var channelID, emoji string
	err := db.queryRowWithLog(context.Background(), "selectChannelAndEmojiForStream", streamID).
		Scan(&channelID, &emoji)
	return channelID, emoji, err
}

func UpdateSpeakerEmoji(streamID, newEmoji string) error {
	_, err := db.execWithLog(
		context.Background(),
		"updateSpeakerEmoji",
		newEmoji,
		streamID,
	)
	return err
}

func GetChannelIDForStream(streamID string) (string, error) {
	var channelID string
	err := db.queryRowWithLog(context.Background(), "selectChannelIDForStream", streamID).
		Scan(&channelID)
	return channelID, err
}

func EndStreamForChannel(guildID, channelID string) error {
	_, err := db.execWithLog(
		context.Background(),
		"updateStreamEndTimeForChannel",
		guildID,
		channelID,
	)
	return err
}
