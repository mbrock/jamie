package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"jamie/etc"

	"github.com/charmbracelet/log"
	_ "github.com/mattn/go-sqlite3"
)

// DB represents the database connection and prepared statements cache
type DB struct {
	*sql.DB
	stmtCache sync.Map
	logger    *log.Logger
}

// Transcription represents a single transcription entry
type Transcription struct {
	Emoji     string
	Text      string
	Timestamp time.Time
	SampleIdx int
}

// Stream represents a single stream entry
type Stream struct {
	ID                 string
	CreatedAt          time.Time
	TranscriptionCount int
}

var db *DB

// InitDB initializes the database connection
func InitDB(logger *log.Logger) error {
	sqlDB, err := sql.Open("sqlite3", "./001.db")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	db = &DB{
		DB:     sqlDB,
		logger: logger,
	}

	if err := runMigrations(); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	if err := createSystemPromptsTable(); err != nil {
		return fmt.Errorf("create system_prompts table: %w", err)
	}

	return nil
}

func runMigrations() error {
	migrations, err := LoadMigrations("db")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	db.logger.Info("Starting database migration process...")
	if err := Migrate(db.DB, migrations, db.logger); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	db.logger.Info("Database migration process completed")
	return nil
}

func createSystemPromptsTable() error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS system_prompts (
			name TEXT PRIMARY KEY,
			prompt TEXT NOT NULL
		);
	`)
	return err
}

// GetDB returns the database instance
func GetDB() *DB {
	return db
}

// Close closes the database connection and clears the statement cache
func Close() {
	if db != nil {
		db.stmtCache.Range(func(_, value interface{}) bool {
			if stmt, ok := value.(*sql.Stmt); ok {
				stmt.Close()
			}
			return true
		})
		db.DB.Close()
	}
}

// prepareStmt prepares and caches a statement
func (db *DB) prepareStmt(query string) (*sql.Stmt, error) {
	if stmt, ok := db.stmtCache.Load(query); ok {
		return stmt.(*sql.Stmt), nil
	}

	stmt, err := db.Prepare(query)
	if err != nil {
		return nil, err
	}

	db.stmtCache.Store(query, stmt)
	return stmt, nil
}

// exec executes a query with logging
func (db *DB) exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	db.logger.Debug("Executing SQL statement", "query", query, "args", args)
	stmt, err := db.prepareStmt(query)
	if err != nil {
		return nil, err
	}
	return stmt.ExecContext(ctx, args...)
}

// queryRow executes a query that returns a single row with logging
func (db *DB) queryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	db.logger.Debug("Executing SQL query", "query", query, "args", args)
	stmt, err := db.prepareStmt(query)
	if err != nil {
		return db.QueryRowContext(ctx, query, args...)
	}
	return stmt.QueryRowContext(ctx, args...)
}

// CreateStream creates a new stream entry
func (db *DB) CreateStream(id string, packetSeqOffset int, sampleIdxOffset int) error {
	query := `
		INSERT INTO streams (id, packet_seq_offset, sample_idx_offset)
		VALUES (?, ?, ?)
	`
	_, err := db.exec(context.Background(), query, id, packetSeqOffset, sampleIdxOffset)
	return err
}

// SavePacket saves a packet entry
func (db *DB) SavePacket(id string, stream string, packetSeq int, sampleIdx int, payload []byte) error {
	query := `
		INSERT INTO packets (id, stream, packet_seq, sample_idx, payload)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := db.exec(context.Background(), query, id, stream, packetSeq, sampleIdx, payload)
	return err
}

// CreateSpeaker creates a new speaker entry
func (db *DB) CreateSpeaker(id, stream, emoji string) error {
	query := `
		INSERT INTO speakers (id, stream, emoji)
		VALUES (?, ?, ?)
	`
	_, err := db.exec(context.Background(), query, id, stream, emoji)
	return err
}

// CreateDiscordSpeaker creates a new Discord speaker entry
func (db *DB) CreateDiscordSpeaker(id, speaker, discordID string) error {
	query := `
		INSERT INTO discord_speakers (id, speaker, discord_id)
		VALUES (?, ?, ?)
	`
	_, err := db.exec(context.Background(), query, id, speaker, discordID)
	return err
}

// CreateDiscordChannelStream creates a new Discord channel stream entry
func (db *DB) CreateDiscordChannelStream(id, stream, discordGuild, discordChannel string) error {
	query := `
		INSERT INTO discord_channel_streams (id, stream, discord_guild, discord_channel)
		VALUES (?, ?, ?, ?)
	`
	_, err := db.exec(context.Background(), query, id, stream, discordGuild, discordChannel)
	return err
}

// CreateAttribution creates a new attribution entry
func (db *DB) CreateAttribution(id, stream, speaker string) error {
	query := `
		INSERT INTO attributions (id, stream, speaker)
		VALUES (?, ?, ?)
	`
	_, err := db.exec(context.Background(), query, id, stream, speaker)
	return err
}

// SaveRecognition saves a recognition entry
func (db *DB) SaveRecognition(id, stream string, sampleIdx, sampleLen int, text string, confidence float64) error {
	query := `
		INSERT INTO recognitions (id, stream, sample_idx, sample_len, text, confidence)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	_, err := db.exec(context.Background(), query, id, stream, sampleIdx, sampleLen, text, confidence)
	return err
}

// GetRecentTranscriptions retrieves recent transcriptions
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
		err := rows.Scan(&t.Emoji, &t.Text, &timestampStr)
		if err != nil {
			return nil, err
		}
		t.Timestamp, err = time.Parse("2006-01-02 15:04:05", timestampStr)
		if err != nil {
			return nil, fmt.Errorf("parse timestamp: %w", err)
		}
		transcriptions = append(transcriptions, t)
	}

	return transcriptions, rows.Err()
}

// GetRecentStreamsWithTranscriptionCount retrieves recent streams with transcription count
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

// GetTranscriptionsForStream retrieves transcriptions for a specific stream
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
		err := rows.Scan(&t.Emoji, &t.Text, &timestampStr, &t.SampleIdx)
		if err != nil {
			return nil, err
		}
		t.Timestamp, err = time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			return nil, fmt.Errorf("parse timestamp: %w", err)
		}
		transcriptions = append(transcriptions, t)
	}

	return transcriptions, rows.Err()
}

// GetStreamForDiscordChannelAndSpeaker retrieves a stream for a Discord channel and speaker
func (db *DB) GetStreamForDiscordChannelAndSpeaker(guildID, channelID, discordID string) (string, error) {
	query := `
		SELECT s.id 
		FROM streams s
		JOIN discord_channel_streams dcs ON s.id = dcs.stream
		JOIN speakers spk ON s.id = spk.stream
		JOIN discord_speakers ds ON spk.id = ds.speaker
		WHERE dcs.discord_guild = ? AND dcs.discord_channel = ? AND ds.discord_id = ?
		ORDER BY s.created_at DESC
		LIMIT 1
	`
	var streamID string
	err := db.queryRow(context.Background(), query, guildID, channelID, discordID).Scan(&streamID)
	return streamID, err
}

// CreateStreamForDiscordChannel creates a new stream for a Discord channel
func (db *DB) CreateStreamForDiscordChannel(streamID, guildID, channelID string, packetSequence, packetTimestamp uint16, speakerID, discordID, emoji string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	queries := []struct {
		query string
		args  []interface{}
	}{
		{
			query: `
				INSERT INTO streams (id, packet_seq_offset, sample_idx_offset)
				VALUES (?, ?, ?)
			`,
			args: []interface{}{streamID, packetSequence, packetTimestamp},
		},
		{
			query: `
				INSERT INTO discord_channel_streams (id, stream, discord_guild, discord_channel)
				VALUES (?, ?, ?, ?)
			`,
			args: []interface{}{etc.Gensym(), streamID, guildID, channelID},
		},
		{
			query: `
				INSERT INTO speakers (id, stream, emoji)
				VALUES (?, ?, ?)
			`,
			args: []interface{}{speakerID, streamID, emoji},
		},
		{
			query: `
				INSERT INTO discord_speakers (id, speaker, discord_id)
				VALUES (?, ?, ?)
			`,
			args: []interface{}{etc.Gensym(), speakerID, discordID},
		},
	}

	for _, q := range queries {
		db.logger.Debug("Executing SQL statement in transaction", "query", q.query, "args", q.args)
		if _, err := tx.Exec(q.query, q.args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// CheckSpeechRecognitionSessionExists checks if a speech recognition session exists
func (db *DB) CheckSpeechRecognitionSessionExists(streamID string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM speech_recognition_sessions WHERE stream = ?
		)
	`
	var exists bool
	err := db.queryRow(context.Background(), query, streamID).Scan(&exists)
	return exists, err
}

// SaveSpeechRecognitionSession saves a speech recognition session
func (db *DB) SaveSpeechRecognitionSession(streamID, sessionData string) error {
	query := `
		INSERT INTO speech_recognition_sessions (stream, session_data)
		VALUES (?, ?)
	`
	_, err := db.exec(context.Background(), query, streamID, sessionData)
	return err
}

// GetSpeechRecognitionSession retrieves a speech recognition session
func (db *DB) GetS