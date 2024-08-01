package main

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"jamie/db"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	_ "github.com/mattn/go-sqlite3"

	"jamie/ai"
	"jamie/discord"
)

var (
	logger *log.Logger
	bot    *discord.Bot
)

func init() {
	cobra.OnInitialize(initConfig)
	discordCmd.Flags().
		String("guild", "", "Specify a guild ID to join voice channels in")
	discordCmd.Flags().
		Bool("talk", false, "Enable talk mode on startup")
	rootCmd.AddCommand(discordCmd)

	// Add persistent flags
	rootCmd.PersistentFlags().String("discord-token", "", "Discord bot token")
	rootCmd.PersistentFlags().
		String("deepgram-api-key", "", "Deepgram API key")
	rootCmd.PersistentFlags().Int("web-port", 8080, "Web server port")
	rootCmd.PersistentFlags().String("openai-api-key", "", "OpenAI API key")
	rootCmd.PersistentFlags().
		String("elevenlabs-api-key", "", "ElevenLabs API key")
	rootCmd.PersistentFlags().Int("http-port", 8081, "HTTP server port")

	// Bind flags to viper
	viper.BindPFlag(
		"discord_token",
		rootCmd.PersistentFlags().Lookup("discord-token"),
	)
	viper.BindPFlag(
		"deepgram_api_key",
		rootCmd.PersistentFlags().Lookup("deepgram-api-key"),
	)
	viper.BindPFlag("web_port", rootCmd.PersistentFlags().Lookup("web-port"))
	viper.BindPFlag(
		"openai_api_key",
		rootCmd.PersistentFlags().Lookup("openai-api-key"),
	)
	viper.BindPFlag(
		"elevenlabs_api_key",
		rootCmd.PersistentFlags().Lookup("elevenlabs-api-key"),
	)
	viper.BindPFlag(
		"http_port",
		rootCmd.PersistentFlags().Lookup("http-port"),
	)
}

func initConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()

	err := viper.ReadInConfig()
	if err != nil {
		fmt.Printf("Error reading config file: %s\n", err)
	}

	logger = log.New(os.Stdout)
}

var rootCmd = &cobra.Command{
	Use:   "jamie",
	Short: "Jamie is a Discord bot for voice channel transcription",
	Long:  `Jamie is a Discord bot that transcribes voice channels and provides various utilities.`,
}

var discordCmd = &cobra.Command{
	Use:   "discord",
	Short: "Start the Discord bot",
	Run:   runDiscord,
}

// var httpServerCmd = &cobra.Command{
// 	Use:   "http",
// 	Short: "Start the HTTP server",
// 	Run:   RunHTTPServer,
// }

// func RunHTTPServer(_ *cobra.Command, _ []string) {
// 	mainLogger, _, _, _ := createLoggers()

// 	queries, err := InitDB()
// 	if err != nil {
// 		mainLogger.Fatal("initialize database", "error", err.Error())
// 	}

// 	r := mux.NewRouter()

// 	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
// 		streams, err := queries.GetAllStreamsWithDetails(r.Context())
// 		if err != nil {
// 			http.Error(
// 				w,
// 				"Failed to fetch streams",
// 				http.StatusInternalServerError,
// 			)
// 			return
// 		}

// 		transcriptions, err := queries.GetRecentRecognitions(r.Context(), 100)
// 		if err != nil {
// 			http.Error(
// 				w,
// 				"Failed to fetch transcriptions",
// 				http.StatusInternalServerError,
// 			)
// 			return
// 		}

// 		component := html.Index(streams, transcriptions)
// 		err = component.Render(r.Context(), w)
// 		if err != nil {
// 			http.Error(
// 				w,
// 				"Failed to render template",
// 				http.StatusInternalServerError,
// 			)
// 			return
// 		}
// 	})

// 	// Helper function to convert samples to duration
// 	samplesToDuration := func(samples int64) string {
// 		duration := time.Duration(samples) * time.Second / 48000
// 		hours := int(duration.Hours())
// 		minutes := int(duration.Minutes()) % 60
// 		seconds := int(duration.Seconds()) % 60
// 		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
// 	}

// 	r.HandleFunc(
// 		"/stream/{id}/debug",
// 		func(w http.ResponseWriter, r *http.Request) {
// 			vars := mux.Vars(r)
// 			streamID := vars["id"]

// 			stream, err := queries.GetStream(r.Context(), streamID)
// 			if err != nil {
// 				http.Error(w, "Stream not found", http.StatusNotFound)
// 				return
// 			}

// 			packets, err := queries.GetPacketsForStreamInSampleRange(
// 				r.Context(),
// 				db.GetPacketsForStreamInSampleRangeParams{
// 					Stream:      streamID,
// 					SampleIdx:   stream.SampleIdxOffset,
// 					SampleIdx_2: stream.SampleIdxOffset + 1000000000, // Arbitrary large number to get all packets
// 				},
// 			)
// 			if err != nil {
// 				http.Error(
// 					w,
// 					"Failed to fetch packets",
// 					http.StatusInternalServerError,
// 				)
// 				return
// 			}

// 			recognitions, err := queries.GetTranscriptionsForStream(
// 				r.Context(),
// 				streamID,
// 			)
// 			if err != nil {
// 				http.Error(
// 					w,
// 					"Failed to fetch recognitions",
// 					http.StatusInternalServerError,
// 				)
// 				return
// 			}

// 			viewModel := html.DebugViewModel{
// 				Stream: stream,
// 			}

// 			createdTime := etc.JulianDayToTime(stream.CreatedAt)

// 			for _, packet := range packets {
// 				duration := time.Duration(
// 					packet.SampleIdx-stream.SampleIdxOffset,
// 				) * time.Second / 48000
// 				viewModel.Packets = append(
// 					viewModel.Packets,
// 					html.PacketViewModel{
// 						SampleIdx:         packet.SampleIdx,
// 						RelativeSampleIdx: packet.SampleIdx - stream.SampleIdxOffset,
// 						Timestamp: createdTime.Add(duration).
// 							Format(time.RFC3339Nano),
// 						Duration: samplesToDuration(
// 							packet.SampleIdx - stream.SampleIdxOffset,
// 						),
// 					},
// 				)
// 			}

// 			for _, recognition := range recognitions {
// 				duration := time.Duration(
// 					recognition.SampleIdx-stream.SampleIdxOffset,
// 				) * time.Second / 48000
// 				viewModel.Recognitions = append(
// 					viewModel.Recognitions,
// 					html.RecognitionViewModel{
// 						SampleIdx:         recognition.SampleIdx,
// 						RelativeSampleIdx: recognition.SampleIdx - stream.SampleIdxOffset,
// 						Timestamp: createdTime.Add(duration).
// 							Format(time.RFC3339Nano),
// 						Duration: samplesToDuration(
// 							recognition.SampleIdx - stream.SampleIdxOffset,
// 						),
// 						Text:      recognition.Text,
// 						SampleLen: recognition.SampleLen,
// 					},
// 				)
// 			}

// 			if len(viewModel.Packets) > 0 {
// 				viewModel.EndSample = viewModel.Packets[len(viewModel.Packets)-1].SampleIdx
// 			} else {
// 				viewModel.EndSample = stream.SampleIdxOffset
// 			}

// 			component := html.Debug(viewModel)
// 			err = component.Render(r.Context(), w)
// 			if err != nil {
// 				http.Error(
// 					w,
// 					"Failed to render template",
// 					http.StatusInternalServerError,
// 				)
// 				return
// 			}
// 		},
// 	)

// 	r.HandleFunc(
// 		"/stream/{id}",
// 		func(w http.ResponseWriter, r *http.Request) {
// 			vars := mux.Vars(r)
// 			streamID := vars["id"]

// 			startSample, _ := strconv.ParseInt(
// 				r.URL.Query().Get("start"),
// 				10,
// 				64,
// 			)
// 			endSample, _ := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)

// 			stream, err := queries.GetStream(r.Context(), streamID)
// 			if err != nil {
// 				http.Error(w, "Stream not found", http.StatusNotFound)
// 				return
// 			}

// 			if startSample == 0 || endSample == 0 {
// 				startSample = 0
// 				endSample = 10000 * 48000 // 10000 seconds of audio
// 			}

// 			// startSample = startSample
// 			// endSample = endSample

// 			oggData, err := audio.GenerateOggOpusBlob(
// 				mainLogger,
// 				queries,
// 				streamID,
// 				startSample+stream.SampleIdxOffset,
// 				endSample+stream.SampleIdxOffset,
// 			)
// 			if err != nil {
// 				http.Error(
// 					w,
// 					"Failed to generate OGG file",
// 					http.StatusInternalServerError,
// 				)
// 				return
// 			}

// 			w.Header().
// 				Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s_%d_%d.ogg\"", streamID, startSample, endSample))
// 			w.Header().Set("Content-Type", "audio/ogg")
// 			w.Header().Set("Content-Length", strconv.Itoa(len(oggData)))
// 			w.Write(oggData)
// 		},
// 	)

// 	port := viper.GetInt("http_port")
// 	mainLogger.Info(fmt.Sprintf("Starting HTTP server on port %d", port))
// 	err = http.ListenAndServe(fmt.Sprintf(":%d", port), r)
// 	if err != nil {
// 		mainLogger.Fatal("start HTTP server", "error", err.Error())
// 	}
// }

//go:embed schema.sql
var ddl string

func InitDB() (*db.Queries, error) {
	ctx := context.Background()
	sqldb, err := sql.Open("sqlite3", "jamie.db")
	if err != nil {
		return nil, err
	}

	if _, err := sqldb.ExecContext(ctx, ddl); err != nil {
		return nil, err
	}

	queries := db.New(sqldb)

	return queries, nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func runDiscord(cmd *cobra.Command, _ []string) {
	mainLogger, discordLogger, deepgramLogger, _ := createLoggers()

	discordToken := viper.GetString("discord_token")
	deepgramAPIKey := viper.GetString("deepgram_api_key")
	elevenlabsAPIKey := viper.GetString("elevenlabs_api_key")
	guildID, _ := cmd.Flags().GetString("guild")
	talkMode, _ := cmd.Flags().GetBool("talk")

	if discordToken == "" {
		mainLogger.Fatal("missing DISCORD_TOKEN or --discord-token=")
	}

	if deepgramAPIKey == "" {
		mainLogger.Fatal("missing DEEPGRAM_API_KEY or --deepgram-api-key=")
	}

	if elevenlabsAPIKey == "" {
		mainLogger.Fatal(
			"missing ELEVENLABS_API_KEY or --elevenlabs-api-key=",
		)
	}

	queries, err := InitDB()
	if err != nil {
		mainLogger.Fatal("initialize database", "error", err.Error())
	}

	transcriptionService, err := ai.NewDeepgramClient(
		deepgramAPIKey,
		deepgramLogger,
	)
	if err != nil {
		mainLogger.Fatal("create deepgram client", "error", err.Error())
	}

	openaiAPIKey := viper.GetString("openai_api_key")
	if openaiAPIKey == "" {
		mainLogger.Fatal("missing OPENAI_API_KEY or --openai-api-key=")
	}

	// Create Discord session
	session, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		mainLogger.Fatal(
			"error creating Discord session",
			"error",
			err.Error(),
		)
	}

	// Wrap the discord session with our DiscordSession struct
	discordWrapper := &discord.DiscordSession{Session: session}

	speechGenerator := ai.NewElevenLabsSpeechGenerator(elevenlabsAPIKey)
	languageModel := ai.NewOpenAILanguageModel(openaiAPIKey)
	bot, err = discord.NewBot(
		discordWrapper,
		transcriptionService,
		speechGenerator,
		languageModel,
		discordLogger,
		queries,
		guildID,
		talkMode,
	)
	if err != nil {
		mainLogger.Fatal("start discord bot", "error", err.Error())
	}
	defer bot.Close()

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
}

func createLoggers() (mainLogger, discordLogger, deepgramLogger, sqlLogger *log.Logger) {
	logLevel := log.DebugLevel

	logger.SetLevel(logLevel)
	logger.SetReportCaller(true)
	logger.SetCallerFormatter(
		func(file string, line int, funcName string) string {
			path, err := filepath.Rel(".", file)
			if err != nil {
				path = file
			}
			return fmt.Sprintf("%s:%d", path, line)
		},
	)

	styles := log.DefaultStyles()
	styles.Prefix = styles.Prefix.MarginTop(1).
		Bold(false).Transform(func(s string) string {
		return strings.TrimSuffix(s, ":")
	})
	styles.Levels[log.InfoLevel] = styles.Levels[log.InfoLevel].
		MaxWidth(6).
		MarginRight(1).
		Bold(false)
	styles.Levels[log.ErrorLevel] = styles.Levels[log.ErrorLevel].
		MaxWidth(6).
		MarginRight(1).
		Bold(false)
	styles.Message = styles.Message.Bold(true).Width(24)
	styles.Key = styles.Key.MarginLeft(1).
		Bold(false).
		Foreground(lipgloss.Color("#ff8800"))

	logger.SetStyles(styles)

	mainLogger = logger.With().WithPrefix("main")
	discordLogger = logger.With().WithPrefix("discord")
	deepgramLogger = logger.With().WithPrefix("hear")
	sqlLogger = logger.With().WithPrefix("data")

	return
}
