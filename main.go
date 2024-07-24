package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/log"
	"github.com/sashabaranov/go-openai"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"jamie/db"
	"jamie/discordbot"
	"jamie/llm"
	"jamie/stt"
	"jamie/web"
)

var (
	logger *log.Logger
	bot    *discordbot.Bot
)

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.AddCommand(discordCmd)
	rootCmd.AddCommand(webCmd)
	rootCmd.AddCommand(openaiChatCmd)
	rootCmd.AddCommand(summarizeTranscriptCmd)

	// Add persistent flags
	rootCmd.PersistentFlags().String("discord-token", "", "Discord bot token")
	rootCmd.PersistentFlags().
		String("deepgram-api-key", "", "Deepgram API key")
	rootCmd.PersistentFlags().Int("web-port", 8080, "Web server port")
	rootCmd.PersistentFlags().String("openai-api-key", "", "OpenAI API key")

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

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Start the web server",
	Run:   runWeb,
}

var openaiChatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start an OpenAI chat session",
	Run:   runOpenAIChat,
}

var summarizeTranscriptCmd = &cobra.Command{
	Use:   "summarize",
	Short: "Summarize today's transcript using OpenAI",
	Run:   runSummarizeTranscript,
}

func runSummarizeTranscript(cmd *cobra.Command, args []string) {
	mainLogger, _, _, sqlLogger := createLoggers()

	err := db.InitDB(sqlLogger)
	if err != nil {
		mainLogger.Fatal("initialize database", "error", err.Error())
	}
	defer db.Close()

	// Get OpenAI API key
	openaiAPIKey := viper.GetString("openai_api_key")
	if openaiAPIKey == "" {
		mainLogger.Fatal("missing OPENAI_API_KEY or --openai-api-key=")
	}

	summary, err := llm.SummarizeTranscript(openaiAPIKey, 24*time.Hour)
	if err != nil {
		mainLogger.Fatal(
			"failed to summarize transcript",
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

	renderedSummary, err := renderer.Render(summary)
	if err != nil {
		mainLogger.Fatal("failed to render summary", "error", err.Error())
	}

	fmt.Print(renderedSummary)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func runOpenAIChat(cmd *cobra.Command, args []string) {
	openaiAPIKey := viper.GetString("openai_api_key")
	if openaiAPIKey == "" {
		logger.Fatal("missing OPENAI_API_KEY or --openai-api-key=")
	}

	client := openai.NewClient(openaiAPIKey)
	ctx := context.Background()

	req := openai.ChatCompletionRequest{
		Model: openai.GPT3Dot5Turbo,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: "Tell me a short joke about programming.",
			},
		},
		Stream: true,
	}

	stream, err := client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		logger.Fatal("ChatCompletionStream error", "error", err)
	}
	defer stream.Close()

	fmt.Printf("AI: ")
	for {
		response, err := stream.Recv()
		if err == io.EOF {
			fmt.Println("\nStream finished")
			return
		}

		if err != nil {
			logger.Fatal("Stream error", "error", err)
		}

		fmt.Printf("%s", response.Choices[0].Delta.Content)
	}
}

func runDiscord(cmd *cobra.Command, args []string) {
	mainLogger, discordLogger, deepgramLogger, sqlLogger := createLoggers()

	discordToken := viper.GetString("discord_token")
	deepgramAPIKey := viper.GetString("deepgram_api_key")

	if discordToken == "" {
		mainLogger.Fatal("missing DISCORD_TOKEN or --discord-token=")
	}

	if deepgramAPIKey == "" {
		mainLogger.Fatal("missing DEEPGRAM_API_KEY or --deepgram-api-key=")
	}

	err := db.InitDB(sqlLogger)
	if err != nil {
		mainLogger.Fatal("initialize database", "error", err.Error())
	}
	defer db.Close()

	mainLogger.Info("Database initialized successfully")

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

	bot, err = discordbot.NewBot(
		discordToken,
		transcriptionService,
		discordLogger,
		openaiAPIKey,
	)
	if err != nil {
		mainLogger.Fatal("start discord bot", "error", err.Error())
	}
	defer bot.Close()

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
}

func runWeb(cmd *cobra.Command, args []string) {
	mainLogger, _, _, sqlLogger := createLoggers()

	db.InitDB(sqlLogger)
	defer db.Close()

	// Load and apply migrations
	migrations, err := db.LoadMigrations("db")
	if err != nil {
		mainLogger.Fatal("load migrations", "error", err.Error())
	}

	mainLogger.Info("Starting database migration process...")
	err = db.Migrate(db.GetDB().DB, migrations, sqlLogger)
	if err != nil {
		mainLogger.Error("apply migrations", "error", err.Error())
		os.Exit(1)
	}

	// Add new migration for system_prompts table
	systemPromptsTableMigration := db.Migration{
		Version: len(migrations) + 1,
		Up: `
			CREATE TABLE IF NOT EXISTS system_prompts (
				name TEXT PRIMARY KEY,
				prompt TEXT NOT NULL
			);
		`,
		Down: `
			DROP TABLE IF EXISTS system_prompts;
		`,
	}
	err = db.Migrate(db.GetDB().DB, []db.Migration{systemPromptsTableMigration}, sqlLogger)
	if err != nil {
		mainLogger.Error("apply system_prompts migration", "error", err.Error())
		os.Exit(1)
	}

	mainLogger.Info("Preparing database statements...")
	err = db.GetDB().PrepareStatements()
	if err != nil {
		mainLogger.Fatal("prepare statements", "error", err.Error())
	}
	mainLogger.Info("Database statements prepared successfully")

	port := viper.GetInt("web_port")
	handler := web.NewHandler(db.GetDB(), mainLogger)

	mainLogger.Info("Starting web server", "port", port)
	err = http.ListenAndServe(fmt.Sprintf(":%d", port), handler)
	if err != nil {
		mainLogger.Fatal("failed to start web server", "error", err.Error())
	}
}

func createLoggers() (mainLogger, discordLogger, deepgramLogger, sqlLogger *log.Logger) {
	logLevel := log.DebugLevel

	logger.SetLevel(logLevel)
	logger.SetReportCaller(true)
	logger.SetCallerFormatter(
		func(file string, line int, funcName string) string {
			return fmt.Sprintf("%s:%d", file, line)
		},
	)

	mainLogger = logger.WithPrefix("app")
	mainLogger.SetLevel(logLevel)
	discordLogger = logger.WithPrefix("yap")
	discordLogger.SetLevel(logLevel)
	deepgramLogger = logger.WithPrefix("ear")
	deepgramLogger.SetLevel(logLevel)
	sqlLogger = logger.WithPrefix("sql")
	sqlLogger.SetLevel(logLevel)

	return
}
