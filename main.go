package main

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"jamie/etc"
	"jamie/llm"
	"jamie/tts"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"jamie/db"
	"jamie/discordbot"
	"jamie/ogg"
	"jamie/stt"
)

var (
	logger *log.Logger
	bot    *discordbot.Bot
)

func init() {
	cobra.OnInitialize(initConfig)
	discordCmd.Flags().
		String("guild", "", "Specify a guild ID to join voice channels in")
	discordCmd.Flags().
		Bool("talk", false, "Enable talk mode on startup")
	rootCmd.AddCommand(discordCmd)
	rootCmd.AddCommand(summarizeTranscriptCmd)
	rootCmd.AddCommand(generateAudioCmd)
	rootCmd.AddCommand(generateOggCmd)
	rootCmd.AddCommand(listStreamsCmd)
	rootCmd.AddCommand(httpServerCmd)

	// Add persistent flags
	rootCmd.PersistentFlags().String("discord-token", "", "Discord bot token")
	rootCmd.PersistentFlags().
		String("deepgram-api-key", "", "Deepgram API key")
	rootCmd.PersistentFlags().Int("web-port", 8080, "Web server port")
	rootCmd.PersistentFlags().String("openai-api-key", "", "OpenAI API key")
	rootCmd.PersistentFlags().
		String("elevenlabs-api-key", "", "ElevenLabs API key")

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
	viper.BindPFlag("http_port", rootCmd.PersistentFlags().Lookup("http-port"))
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

var summarizeTranscriptCmd = &cobra.Command{
	Use:   "summarize",
	Short: "Summarize today's transcript using OpenAI",
	Run:   runSummarizeTranscript,
}

var generateAudioCmd = &cobra.Command{
	Use:   "generateaudio",
	Short: "Generate an audio file from a stream",
	Long:  `Generate an OGG Opus audio file from a specified stream ID, start time, and end time`,
	Run:   runGenerateAudio,
}

var generateOggCmd = &cobra.Command{
	Use:   "generateogg <streamID>",
	Short: "Generate an OGG file from a given stream ID",
	Long:  `Generate an OGG Opus audio file from a specified stream ID`,
	Args:  cobra.ExactArgs(1),
	Run:   runGenerateOgg,
}

var listStreamsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List streams in a cool table",
	Long:  `List all streams with their details in a formatted table`,
	Run:   runListStreams,
}

var httpServerCmd = &cobra.Command{
	Use:   "http",
	Short: "Start the HTTP server",
	Run:   runHTTPServer,
}

//go:embed schema.sql
var ddl string

func InitDB(logger *log.Logger) (*db.Queries, error) {
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

func runGenerateAudio(cmd *cobra.Command, args []string) {
	mainLogger, _, _, sqlLogger := createLoggers()

	queries, err := InitDB(sqlLogger)
	if err != nil {
		mainLogger.Fatal("initialize database", "error", err.Error())
	}

	ctx := context.Background()

	// Fetch recent streams
	streams, err := queries.
		GetRecentStreamsWithTranscriptionCount(
			ctx,
			db.GetRecentStreamsWithTranscriptionCountParams{
				Limit: 100,
			},
		)
	if err != nil {
		mainLogger.Fatal("fetch recent streams", "error", err.Error())
	}

	mainLogger.Info("Fetched streams", "count", len(streams))

	if len(streams) == 0 {
		mainLogger.Fatal("no recent streams found")
	}

	// Prepare stream options for selection
	streamOptions := make([]huh.Option[string], len(streams))
	for i, stream := range streams {
		t := etc.JulianDayToTime(stream.CreatedAt)
		streamOptions[i] = huh.NewOption(
			fmt.Sprintf(
				"%s (%s) - %d transcriptions",
				stream.ID,
				t.Format(time.RFC3339),
				stream.TranscriptionCount,
			),
			stream.ID,
		)
	}

	var selectedStreamID string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Choose a stream").
				Options(streamOptions...).
				Value(&selectedStreamID),
		),
	)

	err = form.Run()
	if err != nil {
		mainLogger.Fatal("form input", "error", err.Error())
	}

	// Fetch transcriptions for the selected stream
	transcriptions, err := queries.
		GetTranscriptionsForStream(ctx, selectedStreamID)
	if err != nil {
		mainLogger.Fatal("fetch transcriptions", "error", err.Error())
	}

	if len(transcriptions) == 0 {
		mainLogger.Fatal("no transcriptions found for the selected stream")
	}

	// Prepare transcription options for selection
	startOptions := make([]string, len(transcriptions))
	for i, t := range transcriptions {
		startOptions[i] = fmt.Sprintf(
			"%s: %s",
			etc.JulianDayToTime(t.CreatedAt).Format("15:04:05"),
			t.Text,
		)
	}

	var startOption, endOption string

	timeSelectionForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Choose start transcription").
				Options(huh.NewOptions(startOptions...)...).
				Value(&startOption),
			huh.NewSelect[string]().
				Title("Choose end transcription").
				Options(huh.NewOptions(startOptions...)...).
				Value(&endOption),
		),
	)

	err = timeSelectionForm.Run()
	if err != nil {
		mainLogger.Fatal("time selection form input", "error", err.Error())
	}

	startIndex := -1
	endIndex := -1
	for i, option := range startOptions {
		if option == startOption {
			startIndex = i
		}
		if option == endOption {
			endIndex = i
		}
	}

	if startIndex == -1 || endIndex == -1 {
		mainLogger.Fatal("Invalid selection")
	}

	if endIndex < startIndex {
		mainLogger.Fatal(
			"end transcription must be after start transcription",
		)
	}

	startSample := transcriptions[startIndex].SampleIdx
	endSample := transcriptions[endIndex].SampleIdx

	oggData, err := generateOggOpusBlob(
		mainLogger,
		queries,
		selectedStreamID,
		startSample,
		endSample,
	)
	if err != nil {
		mainLogger.Fatal("generate OGG Opus blob", "error", err.Error())
	}

	outputFileName := fmt.Sprintf(
		"audio_%s_%d_%d.ogg",
		selectedStreamID,
		startSample,
		endSample,
	)
	err = os.WriteFile(outputFileName, oggData, 0644)
	if err != nil {
		mainLogger.Fatal("write audio file", "error", err.Error())
	}

	fmt.Printf("Audio file generated: %s\n", outputFileName)
}

func runGenerateOgg(cmd *cobra.Command, args []string) {
	mainLogger, _, _, sqlLogger := createLoggers()

	queries, err := InitDB(sqlLogger)
	if err != nil {
		mainLogger.Fatal("initialize database", "error", err.Error())
	}

	streamID := args[0]

	// Fetch the stream details
	stream, err := queries.GetStream(context.Background(), streamID)
	if err != nil {
		mainLogger.Fatal("fetch stream", "error", err.Error())
	}

	oggData, err := generateOggOpusBlob(
		mainLogger,
		queries,
		streamID,
		stream.SampleIdxOffset,
		stream.SampleIdxOffset+10000*48000,
	)
	if err != nil {
		mainLogger.Fatal("generate OGG Opus blob", "error", err.Error())
	}

	outputFileName := fmt.Sprintf("audio_%s.ogg", streamID)
	err = os.WriteFile(outputFileName, oggData, 0644)
	if err != nil {
		mainLogger.Fatal("write audio file", "error", err.Error())
	}

	fmt.Printf("OGG file generated: %s\n", outputFileName)
}

func runListStreams(cmd *cobra.Command, args []string) {
	mainLogger, _, _, sqlLogger := createLoggers()

	queries, err := InitDB(sqlLogger)
	if err != nil {
		mainLogger.Fatal("initialize database", "error", err.Error())
	}

	streams, err := queries.GetAllStreamsWithDetails(context.Background())
	if err != nil {
		mainLogger.Fatal("fetch streams", "error", err.Error())
	}

	if len(streams) == 0 {
		fmt.Println("No streams found.")
		return
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "Created At", "Channel", "Speaker", "Duration", "Transcriptions"})
	table.SetBorder(false)
	table.SetCenterSeparator("|")
	table.SetColumnSeparator("|")
	table.SetRowSeparator("-")
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)

	for _, stream := range streams {
		createdAt := etc.JulianDayToTime(stream.CreatedAt).Format("2006-01-02 15:04:05")
		duration := fmt.Sprintf("%.2f s", float64(stream.Duration)/48000.0) // Convert samples to seconds

		table.Append([]string{
			stream.ID,
			createdAt,
			stream.DiscordChannel,
			stream.Username,
			duration,
			fmt.Sprintf("%d", stream.TranscriptionCount),
		})
	}

	table.Render()
}

func generateOggOpusBlob(
	logger *log.Logger,
	queries *db.Queries,
	streamID string,
	startSample, endSample int64,
) ([]byte, error) {
	return ogg.GenerateOggOpusBlob(
		logger,
		queries,
		streamID,
		startSample,
		endSample,
	)
}

func runSummarizeTranscript(cmd *cobra.Command, args []string) {
	mainLogger, _, _, sqlLogger := createLoggers()

	queries, err := InitDB(sqlLogger)
	if err != nil {
		mainLogger.Fatal("initialize database", "error", err.Error())
	}

	// Get OpenAI API key
	openaiAPIKey := viper.GetString("openai_api_key")
	if openaiAPIKey == "" {
		mainLogger.Fatal("missing OPENAI_API_KEY or --openai-api-key=")
	}

	languageModel := llm.NewOpenAILanguageModel(openaiAPIKey)
	summaryChan, err := llm.SummarizeTranscript(
		queries,
		languageModel,
		"",
	)
	if err != nil {
		mainLogger.Fatal(
			"failed to start summary generation",
			"error",
			err.Error(),
		)
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(62),
	)
	if err != nil {
		mainLogger.Fatal("failed to create renderer", "error", err.Error())
	}

	var fullSummary strings.Builder
	for chunk := range summaryChan {
		fullSummary.WriteString(chunk)

		// Render and print the current chunk
		renderedChunk, err := renderer.Render(chunk)
		if err != nil {
			mainLogger.Error(
				"failed to render summary chunk",
				"error",
				err.Error(),
			)
			continue
		}
		fmt.Print(renderedChunk)
	}

	// Final rendering of the full summary (optional, as we've been printing chunks)
	renderedSummary, err := renderer.Render(fullSummary.String())
	if err != nil {
		mainLogger.Fatal(
			"failed to render full summary",
			"error",
			err.Error(),
		)
	}

	fmt.Print(renderedSummary)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func runDiscord(cmd *cobra.Command, args []string) {
	mainLogger, discordLogger, deepgramLogger, sqlLogger := createLoggers()

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

	queries, err := InitDB(sqlLogger)
	if err != nil {
		mainLogger.Fatal("initialize database", "error", err.Error())
	}

	transcriptionService, err := stt.NewDeepgramClient(
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
	discord, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		mainLogger.Fatal("error creating Discord session", "error", err.Error())
	}

	// Wrap the discord session with our DiscordSession struct
	discordWrapper := &discordbot.DiscordSession{Session: discord}

	speechGenerator := tts.NewElevenLabsSpeechGenerator(elevenlabsAPIKey)
	languageModel := llm.NewOpenAILanguageModel(openaiAPIKey)
	bot, err = discordbot.NewBot(
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
	discordLogger = logger.With().WithPrefix("chat")
	deepgramLogger = logger.With().WithPrefix("hear")
	sqlLogger = logger.With().WithPrefix("data")

	return
}
