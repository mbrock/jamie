package tts

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"node.town/db"
	"node.town/snd"
	"node.town/speechmatics"
)

var TranscribeCmd = &cobra.Command{
	Use:   "transcribe",
	Short: "Transcribe audio from Opus packets",
	Long:  `This command listens for Opus packets, transcribes them, and updates the database with transcription data.`,
	Run:   runTranscribe,
}

var StreamCmd = &cobra.Command{
	Use:   "stream",
	Short: "Stream transcriptions with UI",
	Long:  `This command displays a UI for watching real-time transcriptions.`,
	Run:   runStream,
}

func init() {
	TranscribeCmd.Flags().
		Bool("speechmatics", false, "Use Speechmatics API for transcription (default is Gemini)")
}

func runTranscribe(cmd *cobra.Command, args []string) {
	sqlDB, queries, err := db.OpenDatabase()
	if err != nil {
		log.Fatal("Failed to open database", "error", err)
	}
	defer sqlDB.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	packetChan, ssrcCache, err := snd.StreamOpusPackets(ctx, sqlDB, queries)
	if err != nil {
		log.Fatal("Error setting up opus packet stream", "error", err)
	}

	streamChan := snd.DemuxOpusPackets(ctx, packetChan, ssrcCache)

	useSpeechmatics, _ := cmd.Flags().GetBool("speechmatics")

	log.Info(
		"Listening for demuxed Opus packet streams. Press CTRL-C to exit.",
	)
	log.Info("Real-time transcription enabled")

	for stream := range streamChan {
		go handleStreamWithTranscription(
			ctx,
			stream,
			queries,
			sqlDB,
			useSpeechmatics,
		)
	}

	// Wait for CTRL-C
	<-ctx.Done()
}

func runStream(cmd *cobra.Command, args []string) {
	sqlDB, queries, err := db.OpenDatabase()
	if err != nil {
		log.Fatal("Failed to open database", "error", err)
	}
	defer sqlDB.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	InitLogger()
	defer CloseLogger()

	log.Info("UI enabled")
	transcriptChan := make(chan TranscriptMessage, 100)

	go func() {
		p := tea.NewProgram(initialModel(transcriptChan))
		if _, err := p.Run(); err != nil {
			log.Fatal("Error running program", "error", err)
		}
	}()

	// Listen for transcription updates from the database
	updates, err := snd.ListenForTranscriptionChanges(ctx, sqlDB)
	if err != nil {
		log.Fatal(
			"Failed to set up transcription change listener",
			"error",
			err,
		)
	}

	go func() {
		for update := range updates {
			handleTranscriptionUpdate(ctx, update, queries, transcriptChan)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Wait for CTRL-C
	<-ctx.Done()
}

func handleTranscript(
	ctx context.Context,
	transcript speechmatics.RTTranscriptResponse,
	sessionID int64,
	queries *db.Queries,
	pool *pgxpool.Pool,
) {
	if len(transcript.Results) == 0 {
		return
	}

	log.Info(
		"Handling transcript",
		"isPartial",
		transcript.IsPartial(),
		"resultCount",
		len(transcript.Results),
	)

	// Begin a new transaction
	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Error("Failed to begin transaction", "error", err)
		return
	}
	defer tx.Rollback(ctx) // Rollback if not committed

	// Create a new Queries instance with the transaction
	qtx := queries.WithTx(tx)

	row, err := qtx.UpsertTranscriptionSegment(
		ctx,
		db.UpsertTranscriptionSegmentParams{
			SessionID: sessionID,
			IsFinal:   !transcript.IsPartial(),
		},
	)
	if err != nil {
		log.Error("Failed to upsert transcription segment", "error", err)
		return
	}
	segmentID := row.ResultSegmentID
	currentVersion := row.ResultVersion

	for _, result := range transcript.Results {
		wordID, err := qtx.InsertTranscriptionWord(
			ctx,
			db.InsertTranscriptionWordParams{
				SegmentID: segmentID,
				StartTime: result.StartTime,
				Duration:  result.EndTime - result.StartTime,
				IsEos:     result.IsEOS,
				Version:   int32(currentVersion),
				AttachesTo: pgtype.Text{
					String: result.AttachesTo,
					Valid:  true,
				},
			},
		)
		if err != nil {
			log.Error("Failed to insert transcription word", "error", err)
			return
		}

		for _, alt := range result.Alternatives {
			err = qtx.InsertWordAlternative(
				ctx,
				db.InsertWordAlternativeParams{
					WordID:     wordID,
					Content:    alt.Content,
					Confidence: alt.Confidence,
				},
			)
			if err != nil {
				log.Error("Failed to insert word alternative", "error", err)
				return
			}
		}
	}

	// Commit the transaction
	err = tx.Commit(ctx)
	if err != nil {
		log.Error("Failed to commit transaction", "error", err)
		return
	}

	log.Info(
		"Processed transcript",
		"segmentID", segmentID,
		"wordCount", len(transcript.Results),
		"isPartial", transcript.IsPartial(),
		"version", currentVersion,
	)
}

func handleTranscriptionUpdate(
	ctx context.Context,
	update snd.TranscriptionUpdate,
	queries *db.Queries,
	transcriptChan chan<- TranscriptMessage,
) {
	segment, err := queries.GetTranscriptSegment(ctx, update.ID)
	if err != nil {
		log.Error("Failed to fetch transcript segment", "error", err)
		return
	}

	log.Info("update", "id", update.ID, "segment", segment)
	var words []TranscriptWord
	for _, row := range segment {
		word := TranscriptWord{
			StartTime: float64(row.StartTime.Microseconds) / 1000000,
			EndTime: float64(
				row.StartTime.Microseconds+row.Duration.Microseconds,
			) / 1000000,
			IsEOS:      row.IsEos,
			Content:    row.Content,
			Confidence: row.Confidence,
			AttachesTo: row.AttachesTo.String,
		}
		words = append(words, word)
	}

	transcriptMessage := TranscriptMessage{
		Words:     words,
		IsPartial: !update.IsFinal,
	}

	select {
	case transcriptChan <- transcriptMessage:
	default:
		log.Warn("Transcript channel full, dropping message")
	}

	transcriptText := formatTranscriptWords(words)
	log.Info(
		"Processed transcript update",
		"text", transcriptText,
		"wordCount", len(words),
		"isPartial", !update.IsFinal,
	)
}

func handleTranscriptAndErrorsWithUI(
	ctx context.Context,
	transcriptChan <-chan speechmatics.RTTranscriptResponse,
	errChan <-chan error,
	uiChan chan<- TranscriptMessage,
) {
	for {
		select {
		case transcript, ok := <-transcriptChan:
			if !ok {
				return
			}
			var words []TranscriptWord
			for _, result := range transcript.Results {
				word := TranscriptWord{
					StartTime:  result.StartTime,
					EndTime:    result.EndTime,
					IsEOS:      result.IsEOS,
					Type:       result.Type,
					AttachesTo: result.AttachesTo,
				}

				for _, alt := range result.Alternatives {
					word.Alternatives = append(word.Alternatives, Alternative{
						Content:    alt.Content,
						Confidence: alt.Confidence,
					})
				}

				if len(word.Alternatives) > 0 {
					word.Content = word.Alternatives[0].Content
					word.Confidence = word.Alternatives[0].Confidence
				}

				words = append(words, word)
			}
			if len(words) > 0 {
				transcriptText := formatTranscriptWords(words)
				log.Info("Transcription",
					"text", transcriptText,
					"words", len(words),
					"isPartial", transcript.IsPartial(),
				)
				uiChan <- TranscriptMessage{
					Words:     words,
					IsPartial: transcript.IsPartial(),
				}
			}
		case err, ok := <-errChan:
			if !ok {
				return
			}
			log.Error("Transcription error", "error", err)
			uiChan <- TranscriptMessage{
				Words: []TranscriptWord{{
					Content:    fmt.Sprintf("Error: %v", err),
					Confidence: 1.0,
					StartTime:  0,
					EndTime:    0,
				}},
				IsPartial: false,
			}
		case <-ctx.Done():
			return
		}
	}
}

func formatTranscriptWords(words []TranscriptWord) string {
	var result strings.Builder
	for i, word := range words {
		if i > 0 && word.Type == "word" {
			result.WriteString(" ")
		}
		result.WriteString(word.Content)
	}
	return strings.TrimSpace(result.String())
}

type TranscriptWord struct {
	Content      string
	Confidence   float64
	StartTime    float64
	EndTime      float64
	IsEOS        bool
	Type         string
	AttachesTo   string
	Alternatives []Alternative
}

type Alternative struct {
	Content    string
	Confidence float64
}

type TranscriptMessage struct {
	SessionID int64
	Words     []TranscriptWord
	IsPartial bool
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
func handleStreamWithTranscription(
	ctx context.Context,
	stream <-chan snd.OpusPacketNotification,
	queries *db.Queries,
	pool *pgxpool.Pool,
	useSpeechmatics bool,
) {
	log.Info("Starting handleStreamWithTranscription")

	client := speechmatics.NewClient(viper.GetString("SPEECHMATICS_API_KEY"))
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

	var sessionID int64
	var sessionStarted bool

	tmpDir := "tmp"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		log.Error("Failed to create tmp directory", "error", err)
		return
	}

	var oggWriter *snd.Ogg
	var buffer bytes.Buffer
	var oggFile *os.File
	var seqNo int
	var lastPacketTime time.Time
	silenceTimer := time.NewTicker(100 * time.Millisecond)
	defer silenceTimer.Stop()

	defer func() {
		if oggFile != nil {
			err := oggFile.Close()
			if err != nil {
				log.Error("Failed to close Ogg file", "error", err)
			}
		}
		if oggWriter != nil {
			err := oggWriter.Close()
			if err != nil {
				log.Error("Failed to close Ogg writer", "error", err)
			}
		}
	}()

	go func() {
		for {
			select {
			case transcript, ok := <-transcriptChan:
				if !ok {
					return
				}
				handleTranscript(ctx, transcript, sessionID, queries, pool)
			case err := <-errChan:
				log.Error("Received error from Speechmatics", "error", err)
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case packet, ok := <-stream:
			if !ok {
				log.Info("Stream closed, ending transcription")
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
					log.Error(
						"Failed to end Speechmatics stream",
						"error",
						err,
					)
				}
				return
			}

			if !sessionStarted {
				sessionID, err = queries.InsertTranscriptionSession(
					ctx,
					db.InsertTranscriptionSessionParams{
						Ssrc: packet.Ssrc,
						StartTime: pgtype.Timestamptz{
							Time:  time.Now(),
							Valid: true,
						},
						GuildID:   packet.GuildID,
						ChannelID: packet.ChannelID,
						UserID:    packet.UserID,
					},
				)
				if err != nil {
					log.Error(
						"Failed to insert transcription session",
						"error",
						err,
					)
					return
				}
				log.Info(
					"Inserted transcription session",
					"sessionID",
					sessionID,
				)
				sessionStarted = true
			}

			if oggWriter == nil {
				oggFilePath := filepath.Join(
					tmpDir,
					fmt.Sprintf("%d.ogg", packet.Ssrc),
				)
				oggFile, err = os.Create(oggFilePath)
				if err != nil {
					log.Error("Failed to create Ogg file", "error", err)
					return
				}

				oggWriter, err = snd.NewOgg(
					packet.Ssrc,
					time.Now(),
					time.Now().Add(24*time.Hour),
					io.MultiWriter(oggFile, &buffer),
				)
				if err != nil {
					log.Error("Failed to create Ogg writer", "error", err)
					return
				}

				log.Info("Created Ogg file", "path", oggFilePath)
			}

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

			err = client.SendAudio(buffer.Bytes())
			log.Debug("Sent audio to Speechmatics", "bytes", buffer.Len())
			if err != nil {
				log.Error(
					"Failed to send audio to Speechmatics",
					"error",
					err,
				)
				return
			}
			buffer.Reset()

			seqNo++
			lastPacketTime = time.Now()

		case <-silenceTimer.C:
			if time.Since(lastPacketTime) >= 100*time.Millisecond {
				err = oggWriter.WriteSilence(time.Since(lastPacketTime))
				if err != nil {
					log.Error("Failed to write silence to Ogg", "error", err)
					return
				}

				err = client.SendAudio(buffer.Bytes())
				log.Debug(
					"Sent silence to Speechmatics",
					"bytes",
					buffer.Len(),
				)
				if err != nil {
					log.Error(
						"Failed to send silence to Speechmatics",
						"error",
						err,
					)
					return
				}
				buffer.Reset()

				lastPacketTime = time.Now()
			}

		case <-ctx.Done():
			log.Info("Context cancelled, ending transcription")
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
			return
		}
	}
}
