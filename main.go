package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"encoding/hex"
	"encoding/json"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/google/generative-ai-go/genai"
	pgx "github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/spf13/cobra"
	"google.golang.org/api/option"
	"node.town/speechmatics"
	"node.town/transcription"
)

//go:embed db_init.sql
var sqlFS embed.FS

type Bot struct {
	Discord   *discordgo.Session
	DBPool    *pgxpool.Pool
	SessionID int
}

func handleError(err error, message string) {
	if err != nil {
		log.Fatal(message, "error", err)
	}
}

func connectToDatabase() (*pgxpool.Pool, error) {
	dbpool, err := pgxpool.Connect(
		context.Background(),
		os.Getenv("DATABASE_URL"),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to database: %w", err)
	}
	return dbpool, nil
}

func (b *Bot) handleEvent(s *discordgo.Session, m *discordgo.Event) {
	log.Info("message", "op", m.Operation, "type", m.Type)
}

func (b *Bot) handleGuildCreate(
	s *discordgo.Session,
	m *discordgo.GuildCreate,
) {
	log.Info("guild", "id", m.ID, "name", m.Name)
	for _, channel := range m.Guild.Channels {
		log.Info("channel", "id", channel.ID, "name", channel.Name)
	}
	for _, voice := range m.Guild.VoiceStates {
		log.Info("voice", "id", voice.UserID, "channel", voice.ChannelID)
	}

	cmd, err := s.ApplicationCommandCreate(
		b.Discord.State.User.ID,
		m.ID,
		&discordgo.ApplicationCommand{
			Name:        "jamie",
			Description: "Summon Jamie to this channel",
		},
	)
	if err != nil {
		log.Error("command", "error", err)
	}
	log.Info("app command", "id", cmd.ID)

	// Check if we should join a voice channel in this guild
	var channelID string
	err = b.DBPool.QueryRow(context.Background(),
		`SELECT bvj.channel_id
		 FROM bot_voice_joins bvj
		 JOIN discord_sessions ds ON bvj.session_id = ds.id
		 WHERE bvj.guild_id = $1 AND ds.bot_token = $2
		 ORDER BY bvj.joined_at DESC
		 LIMIT 1`,
		m.ID, os.Getenv("DISCORD_TOKEN")).Scan(&channelID)

	if err == nil && channelID != "" {
		// We have a record of joining a channel in this guild, so let's join it
		vc, err := s.ChannelVoiceJoin(m.ID, channelID, false, false)
		if err != nil {
			log.Error(
				"Failed to join voice channel",
				"guild",
				m.ID,
				"channel",
				channelID,
				"error",
				err,
			)
		} else {
			log.Info("Rejoined voice channel", "guild", m.ID, "channel", channelID)
			vc.AddHandler(b.handleVoiceSpeakingUpdate)
			go b.handleOpusPackets(vc)
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		log.Info("No bot voice joins found for guild", "guild", m.ID)
	} else {
		log.Error("Failed to query bot voice joins", "error", err)
	}
}

func (b *Bot) handleVoiceStateUpdate(
	s *discordgo.Session,
	m *discordgo.VoiceStateUpdate,
) {
	log.Info("voice", "user", m.UserID, "channel", m.ChannelID)

	_, err := b.DBPool.Exec(
		context.Background(),
		`INSERT INTO voice_state_events (
			guild_id, channel_id, user_id, session_id, 
			deaf, mute, self_deaf, self_mute, 
			self_stream, self_video, suppress, 
			request_to_speak_timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		m.GuildID,
		m.ChannelID,
		m.UserID,
		b.SessionID,
		m.Deaf,
		m.Mute,
		m.SelfDeaf,
		m.SelfMute,
		m.SelfStream,
		m.SelfVideo,
		m.Suppress,
		m.RequestToSpeakTimestamp,
	)

	if err != nil {
		log.Error("Failed to insert voice state event", "error", err)
	}
}

func (b *Bot) handleVoiceServerUpdate(
	s *discordgo.Session,
	m *discordgo.VoiceServerUpdate,
) {
	log.Info("voice", "server", m.Endpoint, "token", m.Token)
}

func (b *Bot) handleInteractionCreate(
	s *discordgo.Session,
	m *discordgo.InteractionCreate,
) {
	s.InteractionRespond(
		m.Interaction,
		&discordgo.InteractionResponse{
			Type: discordgo.InteractionResponsePong,
		},
	)

	vc, err := s.ChannelVoiceJoin(m.GuildID, m.ChannelID, false, false)
	if err != nil {
		log.Error("voice", "error", err)
		return
	}

	vc.AddHandler(b.handleVoiceSpeakingUpdate)

	go b.handleOpusPackets(vc)

	// Save or update the channel join information
	err = b.Queries.UpsertBotVoiceJoin(context.Background(), db.UpsertBotVoiceJoinParams{
		GuildID:   m.GuildID,
		ChannelID: m.ChannelID,
		SessionID: b.SessionID,
	})
	if err != nil {
		log.Error("Failed to upsert bot voice join", "error", err)
	}
}

func (b *Bot) handleVoiceSpeakingUpdate(
	vc *discordgo.VoiceConnection,
	m *discordgo.VoiceSpeakingUpdate,
) {
	err := b.Queries.UpsertSSRCMapping(context.Background(), db.UpsertSSRCMappingParams{
		GuildID:   vc.GuildID,
		ChannelID: vc.ChannelID,
		UserID:    m.UserID,
		Ssrc:      int64(m.SSRC),
		SessionID: b.SessionID,
	})
	if err != nil {
		log.Error("Failed to insert/update SSRC mapping", "error", err)
	}
}

func (b *Bot) handleOpusPackets(vc *discordgo.VoiceConnection) {
	for pkt := range vc.OpusRecv {
		err := b.Queries.InsertOpusPacket(context.Background(), db.InsertOpusPacketParams{
			GuildID:   vc.GuildID,
			ChannelID: vc.ChannelID,
			Ssrc:      int64(pkt.SSRC),
			Sequence:  int32(pkt.Sequence),
			Timestamp: int64(pkt.Timestamp),
			OpusData:  pkt.Opus,
			SessionID: b.SessionID,
		})

		if err != nil {
			log.Error("Failed to insert opus packet", "error", err)
		}
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
		queries, err := connectToDatabase()
		handleError(err, "Unable to connect to database")

		// Read and execute the embedded SQL file to create tables
		sqlFile, err := sqlFS.ReadFile("db_init.sql")
		handleError(err, "Failed to read embedded db_init.sql")

		_, err = queries.db.Exec(context.Background(), string(sqlFile))
		handleError(err, "Failed to execute embedded db_init.sql")

		discord, err := discordgo.New(
			fmt.Sprintf("Bot %s", os.Getenv("DISCORD_TOKEN")),
		)
		handleError(err, "Error creating Discord session")

		discord.LogLevel = discordgo.LogInformational

		bot := &Bot{
			Discord: discord,
			Queries: queries,
		}

		discord.AddHandler(bot.handleEvent)
		discord.AddHandler(bot.handleGuildCreate)
		discord.AddHandler(bot.handleVoiceStateUpdate)
		discord.AddHandler(bot.handleVoiceServerUpdate)
		discord.AddHandler(bot.handleInteractionCreate)

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
		sessionID, err := bot.Queries.InsertDiscordSession(context.Background(), db.InsertDiscordSessionParams{
			BotToken: os.Getenv("DISCORD_TOKEN"),
			UserID:   discord.State.User.ID,
		})
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
		// Connect to PostgreSQL
		dbpool, err := pgxpool.Connect(
			context.Background(),
			os.Getenv("DATABASE_URL"),
		)
		if err != nil {
			log.Fatal("Unable to connect to database", "error", err)
		}
		defer dbpool.Close()

		conn, err := dbpool.Acquire(context.Background())
		if err != nil {
			log.Fatal("Error acquiring connection", "error", err)
		}
		defer conn.Release()

		_, err = conn.Exec(context.Background(), "LISTEN new_opus_packet")
		if err != nil {
			log.Fatal("Error listening to channel", "error", err)
		}

		log.Info("Listening for new opus packets. Press CTRL-C to exit.")

		var lastPrintTime time.Time
		packetCount := 0

		for {
			notification, err := conn.Conn().
				WaitForNotification(context.Background())
			if err != nil {
				log.Error("Error waiting for notification", "error", err)
				continue
			}

			var packet map[string]interface{}
			err = json.Unmarshal([]byte(notification.Payload), &packet)
			if err != nil {
				log.Error("Error unmarshalling payload", "error", err)
				continue
			}

			packetCount++
			now := time.Now()

			if lastPrintTime.IsZero() ||
				now.Sub(lastPrintTime) >= time.Second {
				log.Info("Opus packets received", "count", packetCount)
				lastPrintTime = now
				packetCount = 0
			}
		}
	},
}

func parseTimeRange(startTimeStr, endTimeStr string) (time.Time, time.Time, error) {
	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("error parsing start time: %w", err)
	}

	endTime, err := time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("error parsing end time: %w", err)
	}

	return startTime, endTime, nil
}

func fetchOpusPackets(dbpool *pgxpool.Pool, ssrc int64, startTime, endTime time.Time) (pgx.Rows, error) {
	return dbpool.Query(context.Background(), `
		SELECT id, sequence, timestamp, created_at, opus_data
		FROM opus_packets
		WHERE ssrc = $1 AND created_at BETWEEN $2 AND $3
		ORDER BY created_at
	`, ssrc, startTime, endTime)
}

func processOpusPackets(rows pgx.Rows, ogg *Ogg) error {
	defer rows.Close()
	for rows.Next() {
		var packet OpusPacket
		err := rows.Scan(
			&packet.ID,
			&packet.Sequence,
			&packet.Timestamp,
			&packet.CreatedAt,
			&packet.OpusData,
		)
		if err != nil {
			log.Error("Error scanning row", "error", err)
			continue
		}
		ogg.WritePacket(packet)
	}
	return rows.Err()
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

		dbpool, err := connectToDatabase()
		handleError(err, "Unable to connect to database")
		defer dbpool.Close()

		rows, err := fetchOpusPackets(dbpool, ssrc, startTime, endTime)
		handleError(err, "Error querying database")

		ogg, err := NewOgg(ssrc, startTime, endTime, outputFile)
		handleError(err, "Error creating Ogg")

		err = processOpusPackets(rows, ogg)
		handleError(err, "Error processing opus packets")

		err = ogg.Close()
		handleError(err, "Error closing Ogg")

		// Convert OGG to MP3
		mp3OutputFile := strings.TrimSuffix(outputFile, filepath.Ext(outputFile)) + ".mp3"
		err = convertOggToMp3(outputFile, mp3OutputFile)
		handleError(err, "Error converting OGG to MP3")

		// Transcribe
		ctx := context.Background()
		transcriptionService, _ := cmd.Flags().GetString("transcription-service")
		transcription, err := transcribeAudio(ctx, mp3OutputFile, transcriptionService)
		handleError(err, "Error transcribing")

		fmt.Println("Transcription:")
		fmt.Println(transcription)
	},
}

func init() {
	rootCmd.AddCommand(listenCmd)
	rootCmd.AddCommand(listenPacketsCmd)
	rootCmd.AddCommand(packetInfoCmd)

	packetInfoCmd.Flags().Int64P("ssrc", "s", 0, "SSRC to filter packets")
	packetInfoCmd.Flags().
		StringP("start", "f", time.Now().Add(-2*time.Minute).Format(time.RFC3339), "Start time (RFC3339 format)")
	packetInfoCmd.Flags().
		StringP("end", "t", time.Now().Format(time.RFC3339), "End time (RFC3339 format)")
	packetInfoCmd.Flags().
		StringP("output", "o", "output.ogg", "Output Ogg file path")
	packetInfoCmd.Flags().
		StringP("transcription-service", "r", "gemini", "Transcription service to use (gemini or speechmatics)")
}

func uploadFile(
	ctx context.Context,
	client *genai.Client,
	dbpool *pgxpool.Pool,
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

	var remoteURI string
	err = dbpool.QueryRow(context.Background(), "SELECT remote_uri FROM uploaded_files WHERE hash = $1", hashString).
		Scan(&remoteURI)
	if err == nil {
		return remoteURI, false, nil
	} else if err != pgx.ErrNoRows {
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

	_, err = dbpool.Exec(
		context.Background(),
		"INSERT INTO uploaded_files (hash, file_name, remote_uri) VALUES ($1, $2, $3)",
		hashString,
		fileName,
		gfile.URI,
	)
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
		"-i",
		inputFile,
		"-acodec",
		"libmp3lame",
		"-b:a",
		"128k",
		outputFile,
	)
	return cmd.Run()
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal("Error executing root command", "error", err)
	}
}

func transcribeAudio(
	ctx context.Context,
	audioFilePath string,
	transcriptionService string,
) (string, error) {
	switch transcriptionService {
	case "gemini":
		client, err := genai.NewClient(
			ctx,
			option.WithAPIKey(os.Getenv("GEMINI_API_KEY")),
		)
		if err != nil {
			return "", fmt.Errorf("error initializing Gemini client: %w", err)
		}
		defer client.Close()

		tm := transcription.NewTranscriptionManager(client, os.Stdout, nil)
		var transcription strings.Builder
		err = tm.TranscribeSegment(ctx, audioFilePath, false, &transcription)
		if err != nil {
			return "", fmt.Errorf("error transcribing with Gemini: %w", err)
		}
		return transcription.String(), nil

	case "speechmatics":
		client := speechmatics.NewClient(os.Getenv("SPEECHMATICS_API_KEY"))
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
