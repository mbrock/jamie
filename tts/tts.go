package tts

import (
	"context"
	"time"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"node.town/db"
	"node.town/snd"
)

var StreamCmd = &cobra.Command{
	Use:   "stream",
	Short: "Stream demuxed Opus packets",
	Long:  `This command streams demuxed Opus packets and prints information about each stream.`,
	Run:   runStream,
}

func runStream(cmd *cobra.Command, args []string) {
	sqlDB, _, err := db.OpenDatabase()
	if err != nil {
		log.Fatal("Failed to open database", "error", err)
	}
	defer sqlDB.Close(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	packetChan, ssrcCache, err := snd.StreamOpusPackets(ctx, sqlDB)
	if err != nil {
		log.Fatal("Error setting up opus packet stream", "error", err)
	}

	streamChan := snd.DemuxOpusPackets(ctx, packetChan, ssrcCache)

	log.Info(
		"Listening for demuxed Opus packet streams. Press CTRL-C to exit.",
	)

	for stream := range streamChan {
		go handleStream(stream)
	}

	// Wait for CTRL-C
	<-ctx.Done()
}

func handleStream(stream <-chan snd.OpusPacketNotification) {
	var lastPrintTime time.Time
	packetCount := 0
	var firstPacket, lastPacket snd.OpusPacketNotification

	for packet := range stream {
		if packetCount == 0 {
			firstPacket = packet
		}
		lastPacket = packet
		packetCount++

		now := time.Now()
		if lastPrintTime.IsZero() || now.Sub(lastPrintTime) >= time.Second {
			firstTime, _ := time.Parse(
				time.RFC3339Nano,
				firstPacket.CreatedAt,
			)
			lastTime, _ := time.Parse(time.RFC3339Nano, lastPacket.CreatedAt)
			duration := lastTime.Sub(firstTime)
			log.Info("Stream info",
				"ssrc", packet.Ssrc,
				"packets", packetCount,
				"duration", duration.Round(time.Second),
				"guild_id", packet.GuildID,
				"channel_id", packet.ChannelID,
				"user_id", packet.UserID,
			)
			lastPrintTime = now
		}
	}

	// Print final summary when the stream ends
	lastTime, _ := time.Parse(time.RFC3339, lastPacket.CreatedAt)
	firstTime, _ := time.Parse(time.RFC3339, firstPacket.CreatedAt)
	duration := lastTime.Sub(firstTime)
	log.Info("Stream ended",
		"ssrc", lastPacket.Ssrc,
		"total_packets", packetCount,
		"total_duration", duration.Round(time.Second),
		"guild_id", lastPacket.GuildID,
		"channel_id", lastPacket.ChannelID,
		"user_id", lastPacket.UserID,
	)
}
