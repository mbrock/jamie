package tts

import (
	"bytes"
	"context"
	"os"
	"time"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"node.town/db"
	"node.town/snd"
	"node.town/speechmatics"
)

var StreamCmd = &cobra.Command{
	Use:   "stream",
	Short: "Stream demuxed Opus packets",
	Long:  `This command streams demuxed Opus packets and prints information about each stream.`,
	Run:   runStream,
}

func init() {
	StreamCmd.Flags().
		Bool("transcribe", false, "Enable real-time transcription using Speechmatics API")
}

func runStream(cmd *cobra.Command, args []string) {
	sqlDB, queries, err := db.OpenDatabase()
	if err != nil {
		log.Fatal("Failed to open database", "error", err)
	}
	defer sqlDB.Close(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	packetChan, ssrcCache, err := snd.StreamOpusPackets(ctx, sqlDB, queries)
	if err != nil {
		log.Fatal("Error setting up opus packet stream", "error", err)
	}

	streamChan := snd.DemuxOpusPackets(ctx, packetChan, ssrcCache)

	log.Info(
		"Listening for demuxed Opus packet streams. Press CTRL-C to exit.",
	)

	transcribe, _ := cmd.Flags().GetBool("transcribe")
	if transcribe {
		log.Info("Real-time transcription enabled")
	}

	for stream := range streamChan {
		if transcribe {
			go handleStreamWithTranscription(ctx, stream)
		} else {
			go handleStream(stream)
		}
	}

	// Wait for CTRL-C
	<-ctx.Done()
}

func handleStreamWithTranscription(
	ctx context.Context,
	stream <-chan snd.OpusPacketNotification,
) {
	client := speechmatics.NewClient(os.Getenv("SPEECHMATICS_API_KEY"))
	config := speechmatics.TranscriptionConfig{
		Language:       "en",
		EnablePartials: true,
	}
	audioFormat := speechmatics.AudioFormat{
		Type: "file",
	}

	err := client.ConnectWebSocket(ctx, config, audioFormat)
	if err != nil {
		log.Error("Failed to connect to Speechmatics WebSocket", "error", err)
		return
	}
	defer client.CloseWebSocket()

	transcriptChan, errChan := client.ReceiveTranscript(ctx)

	go handleTranscriptAndErrors(ctx, transcriptChan, errChan)

	var buffer bytes.Buffer
	oggWriter, err := snd.NewOgg(
		0,
		time.Now(),
		time.Now().Add(24*time.Hour),
		&buffer,
		4096,
	)
	if err != nil {
		log.Error("Failed to create Ogg writer", "error", err)
		return
	}
	defer oggWriter.Close()

	seqNo := 0
	for packet := range stream {
		createdAt, err := time.Parse(time.RFC3339Nano, packet.CreatedAt)
		if err != nil {
			log.Error("Failed to parse createdAt", "error", err)
			continue
		}
		opusPacket := snd.OpusPacket{
			ID:        int(packet.ID),
			Sequence:  uint16(packet.Sequence),
			Timestamp: uint32(packet.Timestamp),
			CreatedAt: createdAt,
			OpusData:  []byte(packet.OpusData),
		}

		err = oggWriter.WritePacket(opusPacket)
		if err != nil {
			log.Error("Failed to write packet to Ogg", "error", err)
			return
		}

		if buffer.Len() >= 4096 {
			err = client.SendAudio(buffer.Bytes())
			if err != nil {
				log.Error(
					"Failed to send audio to Speechmatics",
					"error",
					err,
				)
				return
			}
			buffer.Reset()
		}

		seqNo++
	}

	// Send any remaining audio data
	if buffer.Len() > 0 {
		err = client.SendAudio(buffer.Bytes())
		if err != nil {
			log.Error(
				"Failed to send final audio to Speechmatics",
				"error",
				err,
			)
		}
	}

	err = client.EndStream(seqNo)
	if err != nil {
		log.Error("Failed to end Speechmatics stream", "error", err)
	}
}

func handleTranscriptAndErrors(
	ctx context.Context,
	transcriptChan <-chan speechmatics.RTTranscriptResponse,
	errChan <-chan error,
) {
	for {
		select {
		case transcript, ok := <-transcriptChan:
			if !ok {
				return
			}
			for _, result := range transcript.Results {
				if len(result.Alternatives) > 0 {
					log.Info(
						"Transcription",
						"text",
						result.Alternatives[0].Content,
					)
				}
			}
		case err, ok := <-errChan:
			if !ok {
				return
			}
			log.Error("Transcription error", "error", err)
		case <-ctx.Done():
			return
		}
	}
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
