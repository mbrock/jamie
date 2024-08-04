package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"node.town/bot"
	"node.town/snd"
	"node.town/tts"

	"encoding/hex"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/google/generative-ai-go/genai"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/olekukonko/tablewriter"

	_ "github.com/lib/pq"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/api/option"
	"node.town/db"
	"node.town/gemini"
	"node.town/speechmatics"
)

func initConfig() {
	viper.SetConfigFile(".env")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.Warn("Error reading config file", "error", err)
	}
}

func handleError(err error, message string) {
	if err != nil {
		log.Fatal(message, "error", err)
	}
}

var rootCmd = &cobra.Command{
	Use:   "jamie",
	Short: "Jamie is a Discord bot for voice channel interactions",
	Long:  `Jamie is a Discord bot that can join voice channels and perform various operations.`,
}

var listenCmd = &cobra.Command{
	Use:   "listen",
	Short: "Start listening in Discord voice channels",
	Long:  `This command starts the Jamie bot and makes it listen in Discord voice channels.`,
	Run: func(cmd *cobra.Command, args []string) {
		sqlDB, queries, err := db.OpenDatabase()
		handleError(err, "Failed to open database")
		defer sqlDB.Close()

		discord, err := discordgo.New(
			fmt.Sprintf("Bot %s", viper.GetString("DISCORD_TOKEN")),
		)
		handleError(err, "Error creating Discord session")

		discord.LogLevel = discordgo.LogInformational

		bot := &bot.Bot{
			Discord: discord,
			Queries: queries,
		}

		discord.AddHandler(bot.HandleEvent)
		discord.AddHandler(bot.HandleGuildCreate)
		discord.AddHandler(bot.HandleVoiceStateUpdate)
		discord.AddHandler(bot.HandleVoiceServerUpdate)
		discord.AddHandler(bot.HandleInteractionCreate)

		err = discord.Open()
		handleError(err, "Error opening Discord session")

		defer func() {
			err := discord.Close()
			if err != nil {
				log.Error("discord", "close", err)
			}
		}()

		log.Info("discord", "status", discord.State.User.Username)

		// Insert a record into the discord_sessions table
		sessionID, err := bot.Queries.InsertDiscordSession(
			context.Background(),
			db.InsertDiscordSessionParams{
				BotToken: viper.GetString("DISCORD_TOKEN"),
				UserID:   discord.State.User.ID,
			},
		)

		if err != nil {
			log.Error("Failed to insert discord session", "error", err)
		}
		bot.SessionID = sessionID

		// wait for CTRL-C
		log.Info("Jamie is now listening. Press CTRL-C to exit.")
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		<-sig
	},
}

var listenPacketsCmd = &cobra.Command{
	Use:   "packets",
	Short: "Listen for new opus packets",
	Long:  `This command listens for new opus packets and prints information about each new packet.`,
	Run: func(cmd *cobra.Command, args []string) {
		sqlDB, queries, err := db.OpenDatabase()
		if err != nil {
			log.Fatal("Failed to open database", "error", err)
		}

		ctx := context.Background()
		defer sqlDB.Close()

		packetChan, _, err := snd.StreamOpusPackets(ctx, sqlDB, queries)
		if err != nil {
			log.Fatal("Error setting up opus packet stream", "error", err)
		}

		log.Info("Listening for new opus packets. Press CTRL-C to exit.")

		var lastPrintTime time.Time
		packetCount := 0

		for packet := range packetChan {
			packetCount++
			now := time.Now()

			if lastPrintTime.IsZero() ||
				now.Sub(lastPrintTime) >= time.Second {
				log.Info(
					"Opus packets received",
					"count",
					packetCount,
					"last_packet",
					packet.ID,
				)
				lastPrintTime = now
				packetCount = 0
			}
		}
	},
}

func parseTimeRange(
	startTimeStr, endTimeStr string,
) (time.Time, time.Time, error) {
	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf(
			"error parsing start time: %w",
			err,
		)
	}

	endTime, err := time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf(
			"error parsing end time: %w",
			err,
		)
	}

	return startTime, endTime, nil
}

func fetchOpusPackets(
	queries *db.Queries,
	ssrc int64,
	startTime, endTime time.Time,
) ([]db.OpusPacket, error) {
	return queries.GetOpusPackets(
		context.Background(),
		db.GetOpusPacketsParams{
			Ssrc:        ssrc,
			CreatedAt:   pgtype.Timestamptz{Time: startTime, Valid: true},
			CreatedAt_2: pgtype.Timestamptz{Time: endTime, Valid: true},
		},
	)
}

func processOpusPackets(packets []db.OpusPacket, ogg *snd.Ogg) error {
	for _, dbPacket := range packets {
		packet := snd.OpusPacket{
			ID:        int(dbPacket.ID),
			Sequence:  uint16(dbPacket.Sequence),
			Timestamp: uint32(dbPacket.Timestamp),
			CreatedAt: dbPacket.CreatedAt.Time,
			OpusData:  dbPacket.OpusData,
		}
		ogg.WritePacket(packet)
	}
	return nil
}

var packetInfoCmd = &cobra.Command{
	Use:   "packetInfo",
	Short: "Get information about opus packets and generate Ogg file",
	Long:  `This command retrieves information about opus packets for a given SSRC within a specified time range and generates an Ogg file.`,
	Run: func(cmd *cobra.Command, args []string) {
		ssrc, _ := cmd.Flags().GetInt64("ssrc")
		startTimeStr, _ := cmd.Flags().GetString("start")
		endTimeStr, _ := cmd.Flags().GetString("end")
		outputFile, _ := cmd.Flags().GetString("output")

		startTime, endTime, err := parseTimeRange(startTimeStr, endTimeStr)
		handleError(err, "Error parsing time range")

		sqlDB, queries, err := db.OpenDatabase()
		handleError(err, "Failed to open database")
		defer sqlDB.Close()

		packets, err := fetchOpusPackets(queries, ssrc, startTime, endTime)
		handleError(err, "Error querying database")

		file, err := os.Create(outputFile)
		handleError(err, "Error creating output file")
		defer file.Close()

		ogg, err := snd.NewOgg(
			ssrc,
			startTime,
			endTime,
			snd.NewOggWriter(file),
			&snd.RealTimeProvider{},
			log.Default(),
		)
		handleError(err, "Error creating Ogg")

		err = processOpusPackets(packets, ogg)
		handleError(err, "Error processing opus packets")

		err = ogg.Close()
		handleError(err, "Error closing Ogg")

		// Convert OGG to MP3
		mp3OutputFile := strings.TrimSuffix(
			outputFile,
			filepath.Ext(outputFile),
		) + ".mp3"
		err = convertOggToMp3(outputFile, mp3OutputFile)
		handleError(err, "Error converting OGG to MP3")

		// Transcribe
		ctx := context.Background()
		transcriptionService, _ := cmd.Flags().
			GetString("transcription-service")
		transcription, err := transcribeAudio(
			ctx,
			queries,
			mp3OutputFile,
			transcriptionService,
		)
		handleError(err, "Error transcribing")

		fmt.Println("Transcription:")
		fmt.Println(transcription)
	},
}

func init() {
	rootCmd.AddCommand(listenCmd)
	rootCmd.AddCommand(listenPacketsCmd)
	rootCmd.AddCommand(packetInfoCmd)
	rootCmd.AddCommand(tts.TranscribeCmd)
	rootCmd.AddCommand(tts.StreamCmd)

	packetInfoCmd.Flags().Int64P("ssrc", "s", 0, "SSRC to filter packets")
	packetInfoCmd.Flags().
		StringP("start", "f", time.Now().Add(-2*time.Minute).Format(time.RFC3339), "Start time (RFC3339 format)")
	packetInfoCmd.Flags().
		StringP("end", "t", time.Now().Format(time.RFC3339), "End time (RFC3339 format)")
	packetInfoCmd.Flags().
		StringP("output", "o", "output.ogg", "Output Ogg file path")
	packetInfoCmd.Flags().
		StringP("transcription-service", "r", "gemini", "Transcription service to use (gemini or speechmatics)")

	reportCmd := &cobra.Command{
		Use:   "report",
		Short: "Generate a voice activity report",
		Long:  `This command generates a report of voice activity within a specified time range.`,
		Run:   runReport,
	}
	reportCmd.Flags().
		StringP("start", "s", time.Now().Add(-24*time.Hour).Format(time.RFC3339), "Start time (RFC3339 format)")
	reportCmd.Flags().
		StringP("end", "e", time.Now().Format(time.RFC3339), "End time (RFC3339 format)")

	rootCmd.AddCommand(reportCmd)
}

func runReport(cmd *cobra.Command, args []string) {
	startTimeStr, _ := cmd.Flags().GetString("start")
	endTimeStr, _ := cmd.Flags().GetString("end")

	startTime, endTime, err := parseTimeRange(startTimeStr, endTimeStr)
	handleError(err, "Error parsing time range")

	sqlDB, queries, err := db.OpenDatabase()
	handleError(err, "Failed to open database")
	defer sqlDB.Close()

	report, err := queries.GetVoiceActivityReport(
		context.Background(),
		db.GetVoiceActivityReportParams{
			CreatedAt:   pgtype.Timestamptz{Time: startTime, Valid: true},
			CreatedAt_2: pgtype.Timestamptz{Time: endTime, Valid: true},
		},
	)
	handleError(err, "Error generating report")

	if len(report) == 0 {
		fmt.Println("No voice activity found in the specified time range.")
		return
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(
		[]string{
			"User ID",
			"Packet Count",
			"First Packet",
			"Last Packet",
			"Total Bytes",
		},
	)

	for _, r := range report {
		table.Append([]string{
			r.UserID,
			fmt.Sprintf("%d", r.PacketCount),
			r.FirstPacket.Time.Format(time.RFC3339),
			r.LastPacket.Time.Format(time.RFC3339),
			fmt.Sprintf("%d", r.TotalBytes),
		})
	}

	fmt.Printf(
		"Voice Activity Report from %s to %s\n\n",
		startTime.Format(time.RFC3339),
		endTime.Format(time.RFC3339),
	)
	table.Render()
}

func uploadFile(
	ctx context.Context,
	client *genai.Client,
	queries *db.Queries,
	fileName string,
) (string, bool, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return "", false, fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	content, err := os.ReadFile(fileName)
	if err != nil {
		return "", false, fmt.Errorf("error reading file: %w", err)
	}

	contentHash := sha256.Sum256(content)
	hashString := hex.EncodeToString(contentHash[:])

	remoteURI, err := queries.GetUploadedFileByHash(ctx, hashString)
	if err == nil {
		return remoteURI, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("error checking for existing file: %w", err)
	}

	gfile, err := client.UploadFile(
		ctx,
		"",
		file,
		&genai.UploadFileOptions{
			DisplayName: filepath.Base(fileName),
		},
	)
	if err != nil {
		return "", false, fmt.Errorf("error uploading file: %w", err)
	}

	err = queries.InsertUploadedFile(ctx, db.InsertUploadedFileParams{
		Hash:      hashString,
		FileName:  fileName,
		RemoteUri: gfile.URI,
	})
	if err != nil {
		return "", false, fmt.Errorf(
			"error saving uploaded file info: %w",
			err,
		)
	}

	return gfile.URI, true, nil
}

func convertOggToMp3(inputFile, outputFile string) error {
	cmd := exec.Command(
		"ffmpeg", "-y",
		"-i", inputFile,
		"-acodec", "libmp3lame",
		"-b:a", "128k",
		outputFile,
	)
	return cmd.Run()
}

func main() {
	initConfig()
	if err := rootCmd.Execute(); err != nil {
		log.Fatal("Error executing root command", "error", err)
	}
}

func transcribeAudio(
	ctx context.Context,
	queries *db.Queries,
	audioFilePath string,
	transcriptionService string,
) (string, error) {
	switch transcriptionService {
	case "gemini":
		client, err := genai.NewClient(
			ctx,
			option.WithAPIKey(viper.GetString("GEMINI_API_KEY")),
		)
		if err != nil {
			return "", fmt.Errorf("error initializing Gemini client: %w", err)
		}
		defer client.Close()

		remoteURI, _, err := uploadFile(ctx, client, queries, audioFilePath)
		if err != nil {
			return "", fmt.Errorf("error uploading file: %w", err)
		}

		tm := gemini.New(client, os.Stdout, nil)
		var transcription strings.Builder
		err = tm.TranscribeSegment(ctx, remoteURI, true, &transcription)
		if err != nil {
			return "", fmt.Errorf("error transcribing with Gemini: %w", err)
		}
		return transcription.String(), nil

	case "speechmatics":
		client := speechmatics.NewClient(
			viper.GetString("SPEECHMATICS_API_KEY"),
		)
		transcription, err := client.SubmitAndWaitForTranscript(
			ctx,
			audioFilePath,
			speechmatics.TranscriptionConfig{
				Language: "en",
			},
			time.Second*1,
		)
		if err != nil {
			return "", fmt.Errorf(
				"error transcribing with Speechmatics: %w",
				err,
			)
		}
		return transcription, nil

	default:
		return "", fmt.Errorf(
			"unknown transcription service: %s",
			transcriptionService,
		)
	}
}
