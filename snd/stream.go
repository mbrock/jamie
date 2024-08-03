package snd

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v5/pgxpool"
	"node.town/db"
)

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

func getUserIDFromCache(cache *SSRCUserIDCache, ssrc int64) string {
	userID, err := cache.Get(ssrc)
	if err != nil {
		log.Error("Error looking up user ID in database", "error", err)
		return ""
	}
	return userID
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
    Operation  string `json:"operation"`
    ID         int64  `json:"id"`
    SessionID  int64  `json:"session_id"`
    IsFinal    bool   `json:"is_final"`
}

func StreamOpusPackets(
	ctx context.Context,
	pool *pgxpool.Pool,
	queries *db.Queries,
) (<-chan OpusPacketNotification, *SSRCUserIDCache, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"failed to acquire database connection: %w",
			err,
		)
	}

	_, err = conn.Exec(ctx, "LISTEN new_opus_packet")
	if err != nil {
		conn.Release()
		return nil, nil, fmt.Errorf(
			"failed to listen for new_opus_packet: %w",
			err,
		)
	}

	packetChan := make(chan OpusPacketNotification)
	cache := NewSSRCUserIDCache(queries)

	go func() {
		defer close(packetChan)
		defer conn.Release()

		for {
			notification, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				if err == context.Canceled {
					return
				}
				log.Error("Error waiting for notification", "error", err)
				return
			}

			var packet OpusPacketNotification
			err = json.Unmarshal([]byte(notification.Payload), &packet)
			if err != nil {
				log.Error("Error unmarshalling payload", "error", err)
				continue
			}

			// Decode the hex-encoded opus data
			decodedData, err := hex.DecodeString(
				strings.TrimPrefix(packet.OpusData, "\\x"),
			)
			if err != nil {
				log.Fatal("Error decoding hex string", "error", err)
			}
			packet.OpusData = string(decodedData)

			// Log the first few bytes of the decoded opus data
			if len(packet.OpusData) > 0 {
				log.Debug(
					"Decoded opus packet data",
					"first_bytes",
					fmt.Sprintf(
						"%x",
						packet.OpusData[:min(4, len(packet.OpusData))],
					),
				)
			}

			packet.UserID = getUserIDFromCache(cache, packet.Ssrc)

			select {
			case packetChan <- packet:
			case <-ctx.Done():
				return
			}
		}
	}()

	return packetChan, cache, nil
}

func DemuxOpusPackets(
	ctx context.Context,
	inputChan <-chan OpusPacketNotification,
	cache *SSRCUserIDCache,
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
					) // Buffer size of 100, adjust as needed
					streams[packet.Ssrc] = streamChan
					outputChan <- streamChan

					// Log the new stream with UserID from cache
					userID := getUserIDFromCache(cache, packet.Ssrc)
					if userID != "" {
						log.Info(
							"New stream started",
							"ssrc",
							packet.Ssrc,
							"userID",
							userID,
						)
					} else {
						log.Info("New stream started", "ssrc", packet.Ssrc, "userID", "unknown")
					}

					streamChan <- packet
				}

				select {
				case streamChan <- packet:
				default:
					log.Warn(
						"Stream channel buffer full, dropping packet",
						"ssrc",
						packet.Ssrc,
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

func ListenForTranscriptionChanges(ctx context.Context, pool *pgxpool.Pool) (<-chan TranscriptionUpdate, error) {
    conn, err := pool.Acquire(ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to acquire database connection: %w", err)
    }

    _, err = conn.Exec(ctx, "LISTEN transcription_change")
    if err != nil {
        conn.Release()
        return nil, fmt.Errorf("failed to listen for transcription_change: %w", err)
    }

    updateChan := make(chan TranscriptionUpdate)

    go func() {
        defer close(updateChan)
        defer conn.Release()

        for {
            notification, err := conn.Conn().WaitForNotification(ctx)
            if err != nil {
                if err == context.Canceled {
                    return
                }
                log.Error("Error waiting for notification", "error", err)
                return
            }

            var update TranscriptionUpdate
            err = json.Unmarshal([]byte(notification.Payload), &update)
            if err != nil {
                log.Error("Error unmarshalling payload", "error", err)
                continue
            }

            select {
            case updateChan <- update:
            case <-ctx.Done():
                return
            }
        }
    }()

    return updateChan, nil
}
