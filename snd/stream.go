package snd

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v5/pgxpool"
	"node.town/db"
)

type UserIDCache interface {
	Get(ssrc int64) (string, error)
}

type PacketStreamer interface {
	Stream(ctx context.Context) (<-chan OpusPacketNotification, error)
}

type PacketDemuxer interface {
	Demux(
		ctx context.Context,
		inputChan <-chan OpusPacketNotification,
	) <-chan (<-chan OpusPacketNotification)
}

type TranscriptionChangeListener interface {
	Listen(ctx context.Context) (<-chan TranscriptionUpdate, error)
}

type SSRCUserIDCache struct {
	mu      sync.RWMutex
	cache   map[int64]string
	queries *db.Queries
}

func NewSSRCUserIDCache(queries *db.Queries) *SSRCUserIDCache {
	return &SSRCUserIDCache{
		cache:   make(map[int64]string),
		queries: queries,
	}
}

func (c *SSRCUserIDCache) Get(ssrc int64) (string, error) {
	c.mu.RLock()
	userID, ok := c.cache[ssrc]
	c.mu.RUnlock()

	if ok {
		return userID, nil
	}

	// If not in cache, look up in the database
	dbUserID, err := c.queries.GetUserIDBySSRC(context.Background(), ssrc)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil // No error, but no user ID found
		}
		return "", err
	}

	// If found in database, add to cache
	c.mu.Lock()
	c.cache[ssrc] = dbUserID
	c.mu.Unlock()

	return dbUserID, nil
}

type OpusPacketNotification struct {
	ID        int64  `json:"id"`
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	Ssrc      int64  `json:"ssrc"`
	Sequence  int32  `json:"sequence"`
	Timestamp int64  `json:"timestamp"`
	OpusData  string `json:"opus_data"`
	CreatedAt string `json:"created_at"`
}

type TranscriptionUpdate struct {
	Operation string `json:"operation"`
	ID        int64  `json:"id"`
	SessionID int64  `json:"session_id"`
	IsFinal   bool   `json:"is_final"`
}

type PostgresPacketStreamer struct {
	pool   *pgxpool.Pool
	cache  UserIDCache
	logger Logger
}

func NewPostgresPacketStreamer(
	pool *pgxpool.Pool,
	cache UserIDCache,
	logger Logger,
) *PostgresPacketStreamer {
	return &PostgresPacketStreamer{
		pool:   pool,
		cache:  cache,
		logger: logger,
	}
}

func (s *PostgresPacketStreamer) Stream(
	ctx context.Context,
) (<-chan OpusPacketNotification, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to acquire database connection: %w",
			err,
		)
	}

	_, err = conn.Exec(ctx, "LISTEN new_opus_packet")
	if err != nil {
		conn.Release()
		return nil, fmt.Errorf(
			"failed to listen for new_opus_packet: %w",
			err,
		)
	}

	packetChan := make(chan OpusPacketNotification)

	go func() {
		defer close(packetChan)
		defer conn.Release()

		for {
			notification, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				if err == context.Canceled {
					return
				}
				s.logger.Error("Error waiting for notification", "error", err)
				return
			}

			packet, err := s.parseNotificationPayload(notification.Payload)
			if err != nil {
				s.logger.Error("Error parsing notification payload", "error", err)
				continue
			}

			select {
			case packetChan <- *packet:
			case <-ctx.Done():
				return
			}
		}
	}()

	return packetChan, nil
}

func (s *PostgresPacketStreamer) getUserIDFromCache(ssrc int64) string {
	userID, err := s.cache.Get(ssrc)
	if err != nil {
		s.logger.Error("Error looking up user ID in cache", "error", err)
		return ""
	}
	return userID
}

func (s *PostgresPacketStreamer) parseNotificationPayload(payload string) (*OpusPacketNotification, error) {
	var packet OpusPacketNotification
	err := json.Unmarshal([]byte(payload), &packet)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling payload: %w", err)
	}

	// Decode opus_data from hex string
	decodedData, err := hex.DecodeString(strings.TrimPrefix(packet.OpusData, "\\x"))
	if err != nil {
		return nil, fmt.Errorf("error decoding opus data: %w", err)
	}
	packet.OpusData = string(decodedData)

	packet.UserID = s.getUserIDFromCache(packet.Ssrc)

	if len(packet.OpusData) > 0 {
		s.logger.Debug(
			"Decoded opus packet data",
			"first_bytes",
			fmt.Sprintf(
				"%x",
				packet.OpusData[:min(4, len(packet.OpusData))],
			),
		)
	}

	return &packet, nil
}

type DiscordEventNotification struct {
	ID        int32           `json:"id"`
	Operation int32           `json:"operation"`
	Sequence  pgtype.Int4     `json:"sequence"`
	Type      string          `json:"type"`
	RawData   json.RawMessage `json:"raw_data"`
	BotToken  string          `json:"bot_token"`
	CreatedAt time.Time       `json:"created_at"`
}

type DiscordEventStreamer struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	logger  Logger
}

func NewDiscordEventStreamer(pool *pgxpool.Pool, queries *db.Queries, logger Logger) *DiscordEventStreamer {
	return &DiscordEventStreamer{
		pool:    pool,
		queries: queries,
		logger:  logger,
	}
}

func (s *DiscordEventStreamer) Stream(ctx context.Context) (<-chan DiscordEventNotification, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire database connection: %w", err)
	}

	_, err = conn.Exec(ctx, "LISTEN new_discord_event")
	if err != nil {
		conn.Release()
		return nil, fmt.Errorf("failed to listen for new_discord_event: %w", err)
	}

	eventChan := make(chan DiscordEventNotification)

	go func() {
		defer close(eventChan)
		defer conn.Release()

		for {
			notification, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				s.logger.Error("Error waiting for notification", "error", err)
				return
			}

			var discordEvent DiscordEventNotification
			err = json.Unmarshal([]byte(notification.Payload), &discordEvent)
			if err != nil {
				s.logger.Error("Error unmarshalling payload", "error", err)
				continue
			}

			select {
			case eventChan <- discordEvent:
			case <-ctx.Done():
				return
			}
		}
	}()

	return eventChan, nil
}

type DefaultPacketDemuxer struct {
	cache  UserIDCache
	logger Logger
}

func NewDefaultPacketDemuxer(
	cache UserIDCache,
	logger Logger,
) *DefaultPacketDemuxer {
	return &DefaultPacketDemuxer{
		cache:  cache,
		logger: logger,
	}
}

func (d *DefaultPacketDemuxer) Demux(
	ctx context.Context,
	inputChan <-chan OpusPacketNotification,
) <-chan (<-chan OpusPacketNotification) {
	outputChan := make(chan (<-chan OpusPacketNotification))

	go func() {
		defer close(outputChan)

		streams := make(map[int64]chan OpusPacketNotification)

		for {
			select {
			case packet, ok := <-inputChan:
				if !ok {
					// Close all existing streams when input channel is closed
					for _, streamChan := range streams {
						close(streamChan)
					}
					return
				}

				streamChan, exists := streams[packet.Ssrc]
				if !exists {
					streamChan = make(
						chan OpusPacketNotification,
						1000,
					) // Buffer size of 1000, adjust as needed
					streams[packet.Ssrc] = streamChan
					outputChan <- streamChan

					// Log the new stream with UserID from cache
					userID, _ := d.cache.Get(packet.Ssrc)
					if userID != "" {
						d.logger.Info(
							"New stream started",
							"ssrc", packet.Ssrc,
							"userID", userID,
						)
					} else {
						d.logger.Info("New stream started", "ssrc", packet.Ssrc, "userID", "unknown")
					}
				}

				select {
				case streamChan <- packet:
				default:
					d.logger.Warn(
						"Stream channel buffer full, dropping packet",
						"ssrc", packet.Ssrc,
					)
				}

			case <-ctx.Done():
				// Close all existing streams when context is cancelled
				for _, streamChan := range streams {
					close(streamChan)
				}
				return
			}
		}
	}()

	return outputChan
}

func StreamOpusPackets(
	ctx context.Context,
	streamer PacketStreamer,
) (<-chan OpusPacketNotification, error) {
	return streamer.Stream(ctx)
}

func StreamDiscordEvents(
	ctx context.Context,
	streamer *DiscordEventStreamer,
) (<-chan DiscordEventNotification, error) {
	return streamer.Stream(ctx)
}

func DemuxOpusPackets(
	ctx context.Context,
	demuxer PacketDemuxer,
	inputChan <-chan OpusPacketNotification,
) <-chan (<-chan OpusPacketNotification) {
	return demuxer.Demux(ctx, inputChan)
}

type PostgresTranscriptionChangeListener struct {
	pool   *pgxpool.Pool
	logger Logger
}

func NewPostgresTranscriptionChangeListener(
	pool *pgxpool.Pool,
	logger Logger,
) *PostgresTranscriptionChangeListener {
	return &PostgresTranscriptionChangeListener{
		pool:   pool,
		logger: logger,
	}
}

func (l *PostgresTranscriptionChangeListener) Listen(
	ctx context.Context,
) (<-chan TranscriptionUpdate, error) {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to acquire database connection: %w",
			err,
		)
	}

	_, err = conn.Exec(ctx, "LISTEN transcription_change")
	if err != nil {
		conn.Release()
		return nil, fmt.Errorf(
			"failed to listen for transcription_change: %w",
			err,
		)
	}

	updateChan := make(
		chan TranscriptionUpdate,
		100,
	) // Buffered channel with capacity of 100

	go func() {
		defer close(updateChan)
		defer conn.Release()

		for {
			notification, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				l.logger.Error("Error waiting for notification", "error", err)
				return
			}

			var update TranscriptionUpdate
			err = json.Unmarshal([]byte(notification.Payload), &update)
			if err != nil {
				l.logger.Error("Error unmarshalling payload", "error", err)
				continue
			}

			l.logger.Info("Received update", "update", update)

			select {
			case updateChan <- update:
				l.logger.Debug("Sent update to channel", "update", update)
			case <-ctx.Done():
				return
			default:
				l.logger.Warn(
					"Update channel full, dropping update",
					"update", update,
				)
			}
		}
	}()

	return updateChan, nil
}

func ListenForTranscriptionChanges(
	ctx context.Context,
	pool *pgxpool.Pool,
) (<-chan TranscriptionUpdate, error) {
	listener := NewPostgresTranscriptionChangeListener(pool, log.Default())
	return listener.Listen(ctx)
}
