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

// Helper functions
func (db *DB) execContext(query string, args ...interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := db.exec(ctx, query, args...)
	return err
}

func (db *DB) queryRows(
	query string,
	args []interface{},
	parser func(*sql.Rows) (interface{}, error),
) ([]interface{}, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []interface{}
	for rows.Next() {
		result, err := parser(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	return results, rows.Err()
}

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
	StreamID  string
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
func (db *DB) exec(
	ctx context.Context,
	query string,
	args ...interface{},
) (sql.Result, error) {
	db.logger.Debug("query+", "args", args)
	stmt, err := db.prepareStmt(query)
	if err != nil {
		return nil, err
	}
	return stmt.ExecContext(ctx, args...)
}

// queryRow executes a query that returns a single row with logging
func (db *DB) queryRow(
	ctx context.Context,
	query string,
	args ...interface{},
) *sql.Row {
	db.logger.Debug("query1", "args", args)
	stmt, err := db.prepareStmt(query)
	if err != nil {
		return db.QueryRowContext(ctx, query, args...)
	}
	return stmt.QueryRowContext(ctx, args...)
}

// CreateStream creates a new stream entry
func (db *DB) CreateStream(
	id string,
	packetSeqOffset int,
	sampleIdxOffset int,
) error {
	query := `
		INSERT INTO streams (id, packet_seq_offset, sample_idx_offset)
		VALUES (?, ?, ?)
	`
	return db.execContext(query, id, packetSeqOffset, sampleIdxOffset)
}

// SavePacket saves a packet entry
func (db *DB) SavePacket(
	id string,
	stream string,
	packetSeq int,
	sampleIdx int,
	payload []byte,
) error {
	query := `
		INSERT INTO packets (id, stream, packet_seq, sample_idx, payload)
		VALUES (?, ?, ?, ?, ?)
	`
	return db.execContext(query, id, stream, packetSeq, sampleIdx, payload)
}

// CreateSpeaker creates a new speaker entry
func (db *DB) CreateSpeaker(id, stream, emoji string) error {
	query := `
		INSERT INTO speakers (id, stream, emoji)
		VALUES (?, ?, ?)
	`
	return db.execContext(query, id, stream, emoji)
}

// CreateDiscordSpeaker creates a new Discord speaker entry
func (db *DB) CreateDiscordSpeaker(id, speaker, discordID string) error {
	query := `
		INSERT INTO discord_speakers (id, speaker, discord_id)
		VALUES (?, ?, ?)
	`
	return db.execContext(query, id, speaker, discordID)
}

// CreateDiscordChannelStream creates a new Discord channel stream entry
func (db *DB) CreateDiscordChannelStream(
	id, stream, discordGuild, discordChannel string,
) error {
	query := `
		INSERT INTO discord_channel_streams (id, stream, discord_guild, discord_channel)
		VALUES (?, ?, ?, ?)
	`
	return db.execContext(query, id, stream, discordGuild, discordChannel)
}

// CreateAttribution creates a new attribution entry
func (db *DB) CreateAttribution(id, stream, speaker string) error {
	query := `
		INSERT INTO attributions (id, stream, speaker)
		VALUES (?, ?, ?)
	`
	return db.execContext(query, id, stream, speaker)
}

// SaveRecognition saves a recognition entry
func (db *DB) SaveRecognition(
	id, stream string,
	sampleIdx, sampleLen int,
	text string,
	confidence float64,
) error {
	query := `
		INSERT INTO recognitions (id, stream, sample_idx, sample_len, text, confidence)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	return db.execContext(
		query,
		id,
		stream,
		sampleIdx,
		sampleLen,
		text,
		confidence,
	)
}

// queryRowsGeneric is a generic helper function that executes a query and processes the rows using a provided parser function
func queryRowsGeneric[T any](
	db *DB,
	query string,
	args []interface{},
	parser func(*sql.Rows) (T, error),
) ([]T, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []T
	for rows.Next() {
		result, err := parser(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	return results, rows.Err()
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

	return queryRowsGeneric(
		db,
		query,
		nil,
		func(rows *sql.Rows) (Transcription, error) {
			var t Transcription
			var timestampStr string
			err := rows.Scan(&t.Emoji, &t.Text, &timestampStr)
			if err != nil {
				return Transcription{}, err
			}
			t.Timestamp, err = time.Parse("2006-01-02 15:04:05", timestampStr)
			if err != nil {
				return Transcription{}, fmt.Errorf("parse timestamp: %w", err)
			}
			return t, nil
		},
	)
}

// GetRecentStreamsWithTranscriptionCount retrieves recent streams with transcription count
func (db *DB) GetRecentStreamsWithTranscriptionCount(
	guildID, channelID string,
	limit int,
) ([]Stream, error) {
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
	results, err := db.queryRows(
		query,
		[]interface{}{guildID, guildID, channelID, channelID, limit},
		func(rows *sql.Rows) (interface{}, error) {
			var s Stream
			err := rows.Scan(&s.ID, &s.CreatedAt, &s.TranscriptionCount)
			if err != nil {
				return nil, err
			}
			return s, nil
		},
	)
	if err != nil {
		return nil, err
	}

	streams := make([]Stream, len(results))
	for i, result := range results {
		streams[i] = result.(Stream)
	}
	return streams, nil
}

// GetTranscriptionsForStream retrieves transcriptions for a specific stream
func (db *DB) GetTranscriptionsForStream(
	streamID string,
) ([]Transcription, error) {
	query := `
		SELECT s.emoji, r.text, r.created_at, r.sample_idx, r.stream
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
		err := rows.Scan(
			&t.Emoji,
			&t.Text,
			&timestampStr,
			&t.SampleIdx,
			&t.StreamID,
		)
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

// GetStreamForDiscordChannelAndSpeaker retrieves a stream for a Discord channel and speaker
func (db *DB) GetStreamForDiscordChannelAndSpeaker(
	guildID, channelID, discordID string,
) (string, error) {
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
	err := db.queryRow(context.Background(), query, guildID, channelID, discordID).
		Scan(&streamID)
	return streamID, err
}

// CreateStreamForDiscordChannel creates a new stream for a Discord channel
func (db *DB) CreateStreamForDiscordChannel(
	streamID, guildID, channelID string,
	packetSequence, packetTimestamp uint16,
	speakerID, discordID, emoji string,
) error {
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
		db.logger.Debug("tx exec", "args", q.args)
		if _, err := tx.Exec(q.query, q.args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// CheckSpeechRecognitionSessionExists checks if a speech recognition session exists
func (db *DB) CheckSpeechRecognitionSessionExists(
	streamID string,
) (bool, error) {
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
func (db *DB) SaveSpeechRecognitionSession(
	streamID, sessionData string,
) error {
	query := `
		INSERT INTO speech_recognition_sessions (stream, session_data)
		VALUES (?, ?)
	`
	_, err := db.exec(context.Background(), query, streamID, sessionData)
	return err
}

// GetSpeechRecognitionSession retrieves a speech recognition session
func (db *DB) GetSpeechRecognitionSession(streamID string) (string, error) {
	query := `
		SELECT session_data FROM speech_recognition_sessions WHERE stream = ?
	`
	var sessionData string
	err := db.queryRow(context.Background(), query, streamID).
		Scan(&sessionData)
	return sessionData, err
}

// GetChannelAndEmojiForStream retrieves the channel ID and emoji for a stream
func (db *DB) GetChannelAndEmojiForStream(
	streamID string,
) (string, string, error) {
	query := `
		SELECT dcs.discord_channel, s.emoji 
		FROM discord_channel_streams dcs
		JOIN streams st ON dcs.stream = st.id
		JOIN speakers s ON st.id = s.stream
		WHERE st.id = ?
	`
	var channelID, emoji string
	err := db.queryRow(context.Background(), query, streamID).
		Scan(&channelID, &emoji)
	return channelID, emoji, err
}

// UpdateSpeakerEmoji updates the emoji for a speaker
func (db *DB) UpdateSpeakerEmoji(streamID, newEmoji string) error {
	query := `
		UPDATE speakers SET emoji = ? WHERE stream = ?
	`
	return db.execContext(query, newEmoji, streamID)
}

// GetChannelIDForStream retrieves the channel ID for a stream
func (db *DB) GetChannelIDForStream(streamID string) (string, error) {
	query := `
		SELECT discord_channel FROM discord_channel_streams WHERE stream = ?
	`
	var channelID string
	err := db.queryRow(context.Background(), query, streamID).Scan(&channelID)
	return channelID, err
}

// EndStreamForChannel ends a stream for a channel
func (db *DB) EndStreamForChannel(guildID, channelID string) error {
	query := `
		UPDATE streams
		SET ended_at = CURRENT_TIMESTAMP
		WHERE id IN (
			SELECT stream
			FROM discord_channel_streams
			WHERE discord_guild = ? AND discord_channel = ?
		) AND ended_at IS NULL
	`
	return db.execContext(query, guildID, channelID)
}

// GetTodayTranscriptions retrieves transcriptions for today
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
			return nil, fmt.Errorf("parse timestamp: %w", err)
		}
		transcriptions = append(transcriptions, t)
	}

	return transcriptions, rows.Err()
}

// GetTranscriptionsForDuration retrieves transcriptions for a specific duration
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
			return nil, fmt.Errorf("parse timestamp: %w", err)
		}
		transcriptions = append(transcriptions, t)
	}

	return transcriptions, rows.Err()
}

// SetSystemPrompt sets a system prompt
func (db *DB) SetSystemPrompt(name, prompt string) error {
	query := `
		INSERT OR REPLACE INTO system_prompts (name, prompt)
		VALUES (?, ?)
	`
	_, err := db.exec(context.Background(), query, name, prompt)
	return err
}

// GetSystemPrompt retrieves a system prompt
func (db *DB) GetSystemPrompt(name string) (string, error) {
	query := `
		SELECT prompt FROM system_prompts WHERE name = ?
	`
	var prompt string
	err := db.queryRow(context.Background(), query, name).Scan(&prompt)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("no prompt found with name: %s", name)
	}
	return prompt, err
}

// ListSystemPrompts lists all system prompts
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

// GetPacketsForStreamInSampleRange retrieves packets for a stream within a sample range
func (db *DB) GetPacketsForStreamInSampleRange(
	streamID string,
	startSample, endSample int,
) ([]struct {
	Payload   []byte
	SampleIdx int
}, error) {
	query := `
		SELECT payload, sample_idx
		FROM packets
		WHERE stream = ? AND sample_idx BETWEEN ? AND ?
		ORDER BY sample_idx ASC
	`
	rows, err := db.Query(query, streamID, startSample, endSample)
	if err != nil {
		return nil, err
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
			return nil, err
		}
		packets = append(packets, p)
	}

	return packets, rows.Err()
}
func (db *DB) GetConversationTimeRanges(minSilence time.Duration) ([]struct {
	StartTime time.Time
	EndTime   time.Time
}, error) {
	query := `
		WITH ranked_recognitions AS (
			SELECT 
				created_at,
				LAG(created_at) OVER (ORDER BY created_at) AS prev_created_at,
				LEAD(created_at) OVER (ORDER BY created_at) AS next_created_at
			FROM recognitions
			ORDER BY created_at
		),
		gaps AS (
			SELECT 
				created_at,
				JULIANDAY(created_at) - JULIANDAY(prev_created_at) AS prev_gap,
				JULIANDAY(next_created_at) - JULIANDAY(created_at) AS next_gap
			FROM ranked_recognitions
		),
		conversation_boundaries AS (
			SELECT 
				created_at,
				CASE 
					WHEN prev_gap IS NULL OR prev_gap * 24 * 60 * 60 >= ? THEN 'start'
					WHEN next_gap IS NULL OR next_gap * 24 * 60 * 60 >= ? THEN 'end'
					ELSE NULL 
				END AS boundary_type
			FROM gaps
			WHERE prev_gap IS NULL OR next_gap IS NULL OR 
				  prev_gap * 24 * 60 * 60 >= ? OR 
				  next_gap * 24 * 60 * 60 >= ?
		),
		conversation_ranges AS (
			SELECT 
				MAX(CASE WHEN boundary_type = 'start' THEN created_at END) AS start_time,
				MIN(CASE WHEN boundary_type = 'end' THEN created_at END) AS end_time
			FROM (
				SELECT 
					created_at, 
					boundary_type,
					SUM(CASE WHEN boundary_type = 'start' THEN 1 ELSE 0 END) OVER (ORDER BY created_at) AS conversation_group
				FROM conversation_boundaries
			)
			GROUP BY conversation_group
			HAVING start_time IS NOT NULL AND end_time IS NOT NULL
		)
		SELECT start_time, end_time
		FROM conversation_ranges
		ORDER BY start_time
	`

	minSilenceSeconds := minSilence.Seconds()

	rows, err := db.Query(
		query,
		minSilenceSeconds,
		minSilenceSeconds,
		minSilenceSeconds,
		minSilenceSeconds,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []struct {
		StartTime time.Time
		EndTime   time.Time
	}

	for rows.Next() {
		var startTime, endTime string
		if err := rows.Scan(&startTime, &endTime); err != nil {
			return nil, err
		}

		start, err := time.Parse("2006-01-02 15:04:05", startTime)
		if err != nil {
			return nil, fmt.Errorf("parsing start time: %w", err)
		}

		end, err := time.Parse("2006-01-02 15:04:05", endTime)
		if err != nil {
			return nil, fmt.Errorf("parsing end time: %w", err)
		}

		duration := end.Sub(start)
		if duration > 8*time.Hour {
			return nil, fmt.Errorf(
				"conversation duration exceeds 8 hours: %v to %v",
				start,
				end,
			)
		}

		results = append(results, struct {
			StartTime time.Time
			EndTime   time.Time
		}{
			StartTime: start,
			EndTime:   end,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

func (db *DB) GetTranscriptionsForTimeRange(
	startTime, endTime time.Time,
) ([]Transcription, error) {
	query := `
		WITH ranked_recognitions AS (
			SELECT s.emoji, r.text, r.created_at, r.stream, r.sample_idx,
				   LEAD(r.created_at) OVER (PARTITION BY r.stream ORDER BY r.created_at) AS next_created_at
			FROM recognitions r
			JOIN speakers s ON r.stream = s.stream
			WHERE r.created_at >= ?
			ORDER BY r.created_at ASC
		)
		SELECT emoji, text, created_at, stream, sample_idx
		FROM ranked_recognitions
		WHERE created_at <= ? OR (next_created_at IS NULL AND created_at <= datetime(?, '+10 seconds'))
		ORDER BY created_at ASC
	`

	db.logger.Debug(
		"Fetching transcriptions",
		"startTime",
		startTime,
		"endTime",
		endTime,
	)

	rows, err := db.Query(
		query,
		startTime.Format("2006-01-02 15:04:05"),
		endTime.Format("2006-01-02 15:04:05"),
		endTime.Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		db.logger.Error("Error querying transcriptions", "error", err)
		return nil, err
	}
	defer rows.Close()

	var transcriptions []Transcription
	for rows.Next() {
		var t Transcription
		var timestampStr string
		err := rows.Scan(&t.Emoji, &t.Text, &timestampStr)
		if err != nil {
			db.logger.Error("Error scanning row", "error", err)
			return nil, err
		}
		t.Timestamp, err = time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			db.logger.Error(
				"Error parsing timestamp",
				"error",
				err,
				"timestampStr",
				timestampStr,
			)
			return nil, fmt.Errorf("parse timestamp: %w", err)
		}
		transcriptions = append(transcriptions, t)
	}

	db.logger.Debug("Fetched transcriptions", "count", len(transcriptions))

	return transcriptions, rows.Err()
}
