package snd

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v5"
)

type SSRCUserIDCache struct {
	mu    sync.RWMutex
	cache map[int64]string
}

func NewSSRCUserIDCache() *SSRCUserIDCache {
	return &SSRCUserIDCache{
		cache: make(map[int64]string),
	}
}

func (c *SSRCUserIDCache) Set(ssrc int64, userID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[ssrc] = userID
}

func (c *SSRCUserIDCache) Get(ssrc int64) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	userID, ok := c.cache[ssrc]
	return userID, ok
}

type OpusPacketNotification struct {
	ID        int64   `json:"id"`
	GuildID   string  `json:"guild_id"`
	ChannelID string  `json:"channel_id"`
	UserID    string  `json:"user_id"`
	Ssrc      int64   `json:"ssrc"`
	Sequence  int32   `json:"sequence"`
	Timestamp int64   `json:"timestamp"`
	OpusData  string  `json:"opus_data"`
	CreatedAt string  `json:"created_at"`
}

func StreamOpusPackets(ctx context.Context, conn *pgx.Conn) (<-chan OpusPacketNotification, *SSRCUserIDCache, error) {
	_, err := conn.Exec(ctx, "LISTEN new_opus_packet")
	if err != nil {
		return nil, nil, err
	}

	packetChan := make(chan OpusPacketNotification)
	cache := NewSSRCUserIDCache()

	go func() {
		defer close(packetChan)

		for {
			notification, err := conn.WaitForNotification(ctx)
			if err != nil {
				log.Error("Error waiting for notification", "error", err)
				return
			}

			var packet OpusPacketNotification
			err = json.Unmarshal([]byte(notification.Payload), &packet)
			if err != nil {
				log.Error("Error unmarshalling payload", "error", err)
				continue
			}

			cache.Set(packet.Ssrc, packet.UserID)

			select {
			case packetChan <- packet:
			case <-ctx.Done():
				return
			}
		}
	}()

	return packetChan, cache, nil
}

func DemuxOpusPackets(ctx context.Context, inputChan <-chan OpusPacketNotification, cache *SSRCUserIDCache) <-chan (<-chan OpusPacketNotification) {
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
					streamChan = make(chan OpusPacketNotification, 100) // Buffer size of 100, adjust as needed
					streams[packet.Ssrc] = streamChan
					outputChan <- streamChan

					// Log the new stream with UserID from cache
					if userID, ok := cache.Get(packet.Ssrc); ok {
						log.Info("New stream started", "ssrc", packet.Ssrc, "userID", userID)
					} else {
						log.Info("New stream started", "ssrc", packet.Ssrc, "userID", "unknown")
					}
				}

				select {
				case streamChan <- packet:
				default:
					log.Warn("Stream channel buffer full, dropping packet", "ssrc", packet.Ssrc)
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
