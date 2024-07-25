package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"jamie/etc"

	"github.com/charmbracelet/log"
	_ "github.com/mattn/go-sqlite3"
)

type Transcription struct {
	Emoji     string
	Text      string
	Timestamp time.Time
	SampleIdx int
}

var sqlLogger *log.Logger

type DB struct {
	*sql.DB
	stmts map[string]*sql.Stmt
}

var db *DB

func InitDB(logger *log.Logger) error {
	var err error
	sqlDB, err := sql.Open("sqlite3", "./001.db")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	db = &DB{
		DB:    sqlDB,
		stmts: make(map[string]*sql.Stmt),
	}

	sqlLogger = logger

	// Load and apply migrations
	migrations, err := LoadMigrations("db")
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	err = Migrate(db.DB, migrations, sqlLogger)
	if err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	logger.Info("Creating system_prompts table...")
	_, err = db.Exec(`
			CREATE TABLE IF NOT EXISTS system_prompts (
				name TEXT PRIMARY KEY,
				prompt TEXT NOT NULL
			);
		`)
	if err != nil {
		return fmt.Errorf("create system_prompts table: %w", err)
	}

	err = db.PrepareStatements()
	if err != nil {
		return fmt.Errorf("failed to prepare statements: %w", err)
	}

	return nil
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
			JOIN speakers spk ON s.id = spk.stream
			JOIN discord_speakers ds ON spk.id = ds.speaker
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
		"getPacketsForStreamInSampleRange": `
			SELECT payload, sample_idx
			FROM packets
			WHERE stream = ? AND sample_idx BETWEEN ? AND ?
			ORDER BY sample_idx ASC`,
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

func (db *DB) GetRecentTranscriptions() ([]Transcription, error) {
	query := `
		WITH ranked_recognitions AS (
			SELECT 
				s.emoji,
				r.text,
				r.created_at,
				LAG(r.created_at, 1) OVER (ORDER BY r.created_at) AS prev_created_at,
				LAG(s.emoji, 1) OVER (ORDER BY r.created_at) AS prev_emoji
			FROM recognitions r
			JOIN speakers s ON r.stream = s.stream
			ORDER BY r.created_at DESC
		),
		grouped_recognitions AS (
			SELECT 
				emoji,
				text,
				created_at,
				CASE 
					WHEN prev_created_at IS NULL OR 
						 (JULIANDAY(created_at) - JULIANDAY(prev_created_at)) * 24 * 60 > 3 OR
						 emoji != prev_emoji
					THEN 1 
					ELSE 0 
				END AS new_group
			FROM ranked_recognitions
		),
		final_groups AS (
			SELECT 
				emoji,
				GROUP_CONCAT(text, ' ') AS text,
				MIN(created_at) AS created_at
			FROM (
				SELECT 
					emoji,
					text,
					created_at,
					SUM(new_group) OVER (ORDER BY created_at) AS group_id
				FROM grouped_recognitions
			)
			GROUP BY group_id
			ORDER BY created_at DESC
		)
		SELECT emoji, text, created_at
		FROM final_groups
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transcriptions []Transcription
	for rows.Next() {
		var t Transcription
		var timestampStr string
		err := rows.Scan(&t.Emoji, &t.Text, &timestampStr, &t.SampleIdx)
		if err != nil {
			return nil, err
		}
		t.Timestamp, err = time.Parse("2006-01-02 15:04:05", timestampStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timestamp: %w", err)
		}
		transcriptions = append(transcriptions, t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return transcriptions, nil
}

type Stream struct {
	ID                 string
	CreatedAt          time.Time
	TranscriptionCount int
}

func (db *DB) GetRecentStreamsWithTranscriptionCount(guildID, channelID string, limit int) ([]Stream, error) {
	query := `
		SELECT s.id, s.created_at, COUNT(r.id) as transcription_count
		FROM streams s
		LEFT JOIN discord_channel_streams dcs ON s.id = dcs.stream
		LEFT JOIN recognitions r ON s.id = r.stream
		WHERE (dcs.discord_guild = ? OR ? = '') AND (dcs.discord_channel = ? OR ? = '')
		GROUP BY s.id
		ORDER BY s.created_at DESC
		LIMIT ?
	`
	rows, err := db.Query(query, guildID, guildID, channelID, channelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var streams []Stream
	for rows.Next() {
		var s Stream
		err := rows.Scan(&s.ID, &s.CreatedAt, &s.TranscriptionCount)
		if err != nil {
			return nil, err
		}
		streams = append(streams, s)
	}
	return streams, rows.Err()
}

func (db *DB) GetTranscriptionsForStream(streamID string) ([]Transcription, error) {
	query := `
		SELECT s.emoji, r.text, r.created_at, r.sample_idx
		FROM recognitions r
		JOIN speakers s ON r.stream = s.stream
		WHERE r.stream = ?
		ORDER BY r.sample_idx ASC
	`
	rows, err := db.Query(query, streamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transcriptions []Transcription
	for rows.Next() {
		var t Transcription
		var timestampStr string
		err := rows.Scan(&t.Emoji, &t.Text, &timestampStr)
		if err != nil {
			return nil, err
		}
		t.Timestamp, err = time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timestamp: %w", err)
		}
		transcriptions = append(transcriptions, t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return transcriptions, nil
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

func GetStreamForDiscordChannelAndSpeaker(
	guildID, channelID, discordID string,
) (string, error) {
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

func (db *DB) GetTodayTranscriptions() ([]Transcription, error) {
	query := `
		SELECT s.emoji, r.text, r.created_at
		FROM recognitions r
		JOIN speakers s ON r.stream = s.stream
		WHERE DATE(r.created_at) = DATE('now')
		ORDER BY r.created_at ASC
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transcriptions []Transcription
	for rows.Next() {
		var t Transcription
		var timestampStr string
		err := rows.Scan(&t.Emoji, &t.Text, &timestampStr)
		if err != nil {
			return nil, err
		}
		t.Timestamp, err = time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timestamp: %w", err)
		}
		transcriptions = append(transcriptions, t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return transcriptions, nil
}

func (db *DB) GetTranscriptionsForDuration(
	duration time.Duration,
) ([]Transcription, error) {
	query := `
		SELECT s.emoji, r.text, r.created_at
		FROM recognitions r
		JOIN speakers s ON r.stream = s.stream
		WHERE r.created_at >= datetime('now', ?)
		ORDER BY r.created_at ASC
	`
	rows, err := db.Query(
		query,
		fmt.Sprintf("-%d seconds", int(duration.Seconds())),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transcriptions []Transcription
	for rows.Next() {
		var t Transcription
		var timestampStr string
		err := rows.Scan(&t.Emoji, &t.Text, &timestampStr)
		if err != nil {
			return nil, err
		}
		t.Timestamp, err = time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timestamp: %w", err)
		}
		transcriptions = append(transcriptions, t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return transcriptions, nil
}

func (db *DB) SetSystemPrompt(name, prompt string) error {
	query := `
		INSERT OR REPLACE INTO system_prompts (name, prompt)
		VALUES (?, ?)
	`
	_, err := db.Exec(query, name, prompt)
	return err
}

func (db *DB) GetSystemPrompt(name string) (string, error) {
	query := `
		SELECT prompt FROM system_prompts WHERE name = ?
	`
	var prompt string
	err := db.QueryRow(query, name).Scan(&prompt)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("no prompt found with name: %s", name)
	}
	return prompt, err
}

func (db *DB) ListSystemPrompts() (map[string]string, error) {
	query := `
		SELECT name, prompt FROM system_prompts
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prompts := make(map[string]string)
	for rows.Next() {
		var name, prompt string
		if err := rows.Scan(&name, &prompt); err != nil {
			return nil, err
		}
		prompts[name] = prompt
	}
	return prompts, rows.Err()
}

func RunMigrations(logger *log.Logger) error {
	migrations, err := LoadMigrations("db")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	logger.Info("Starting database migration process...")
	err = Migrate(db.DB, migrations, logger)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	logger.Info("Database migration process completed")
	return nil
}

func (db *DB) GetPacketsForStreamInSampleRange(
	streamID string,
	startSample, endSample int,
) ([]struct {
	Payload   []byte
	SampleIdx int
}, error) {
	rows, err := db.stmts["getPacketsForStreamInSampleRange"].Query(
		streamID,
		startSample,
		endSample,
	)
	if err != nil {
		return nil, fmt.Errorf("query packets: %w", err)
	}
	defer rows.Close()

	var packets []struct {
		Payload   []byte
		SampleIdx int
	}
	for rows.Next() {
		var p struct {
			Payload   []byte
			SampleIdx int
		}
		if err := rows.Scan(&p.Payload, &p.SampleIdx); err != nil {
			return nil, fmt.Errorf("scan packet data: %w", err)
		}
		packets = append(packets, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate packets: %w", err)
	}

	return packets, nil
}
