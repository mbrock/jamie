package tts

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"node.town/db"
	"node.town/snd"
)

type Config struct {
	SpeechmaticsAPIKey string
	DatabaseURL        string
}

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
	cfg := loadConfig()

	pgPool, queries, err := db.OpenDatabase()
	if err != nil {
		log.Fatal("Failed to open database", "error", err)
	}
	defer pgPool.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service := NewSpeechmaticsService(cfg.SpeechmaticsAPIKey)
	handler := NewTranscriptionHandler(queries, pgPool, service)

	err = streamAndTranscribe(ctx, pgPool, queries, handler)
	if err != nil {
		log.Fatal("Error in streamAndTranscribe", "error", err)
	}
}

func streamAndTranscribe(
	ctx context.Context,
	pgPool *pgxpool.Pool,
	queries *db.Queries,
	handler *TranscriptionHandler,
) error {
	cache := snd.NewSSRCUserIDCache(queries)
	streamer := snd.NewPostgresPacketStreamer(pgPool, cache, log.Default())
	packetChan, err := snd.StreamOpusPackets(ctx, streamer)
	if err != nil {
		return fmt.Errorf("error setting up opus packet stream: %w", err)
	}

	demuxer := snd.NewDefaultPacketDemuxer(cache, log.Default())
	streamChan := snd.DemuxOpusPackets(ctx, demuxer, packetChan)

	log.Info(
		"Listening for demuxed Opus packet streams. Press CTRL-C to exit.",
	)
	log.Info("Real-time transcription enabled")

	for stream := range streamChan {
		go func(s <-chan snd.OpusPacketNotification) {
			sessionID, err := queries.InsertTranscriptionSession(
				ctx,
				db.InsertTranscriptionSessionParams{
					Ssrc: s.Ssrc,
					StartTime: pgtype.Timestamptz{
						Time:  time.Now(),
						Valid: true,
					},
					GuildID:   s.GuildID,
					ChannelID: s.ChannelID,
					UserID:    s.UserID,
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

			err = handler.ProcessAudioStream(ctx, s, sessionID)
			if err != nil {
				log.Error("Error processing audio stream", "error", err)
			}
		}(stream)
	}

	return nil
}

func runStream(cmd *cobra.Command, args []string) {
	pgPool, queries, err := db.OpenDatabase()
	if err != nil {
		log.Fatal("Failed to open database", "error", err)
	}
	defer pgPool.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	InitLogger()
	defer CloseLogger()

	log.Info("UI enabled")
	transcriptChan := make(chan TranscriptMessage, 100)

	go func() {
		p := tea.NewProgram(initialModel(transcriptChan, queries))
		if _, err := p.Run(); err != nil {
			log.Fatal("Error running program", "error", err)
		}
	}()

	updates, err := snd.ListenForTranscriptionChanges(ctx, pgPool)
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

	<-ctx.Done()
}

func loadConfig() Config {
	return Config{
		SpeechmaticsAPIKey: viper.GetString("SPEECHMATICS_API_KEY"),
		DatabaseURL:        viper.GetString("DATABASE_URL"),
	}
}

func handleTranscriptionUpdate(
	ctx context.Context,
	update snd.TranscriptionUpdate,
	queries *db.Queries,
	transcriptChan chan<- TranscriptMessage,
) {
	if update.Operation != "INSERT" && update.Operation != "UPDATE" {
		return
	}

	segment, err := queries.GetTranscriptionSegment(ctx, update.ID)
	if err != nil {
		log.Error("Failed to get transcription segment", "error", err)
		return
	}

	words, err := queries.GetTranscriptionWords(ctx, update.ID)
	if err != nil {
		log.Error("Failed to get transcription words", "error", err)
		return
	}

	formattedWords := formatTranscriptWords(words)

	transcriptChan <- TranscriptMessage{
		SessionID: segment.SessionID,
		IsFinal:   segment.IsFinal,
		Words:     formattedWords,
	}
}

func formatTranscriptWords(
	words []db.GetTranscriptionWordsRow,
) []TranscriptWord {
	var formattedWords []TranscriptWord
	for _, word := range words {
		formattedWords = append(formattedWords, TranscriptWord{
			Word:      word.Content,
			StartTime: float64(word.StartTime),
			EndTime:   float64(word.StartTime + word.Duration),
		})
	}
	return formattedWords
}
