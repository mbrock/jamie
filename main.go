package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"jamie/db"
	"jamie/discordbot"
	"jamie/stt"
)

var (
	logger *log.Logger
	bot    *discordbot.Bot
)

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.AddCommand(discordCmd)

	// Add persistent flags
	rootCmd.PersistentFlags().String("discord-token", "", "Discord bot token")
	rootCmd.PersistentFlags().
		String("deepgram-api-key", "", "Deepgram API key")

	// Bind flags to viper
	viper.BindPFlag(
		"discord_token",
		rootCmd.PersistentFlags().Lookup("discord-token"),
	)
	viper.BindPFlag(
		"deepgram_api_key",
		rootCmd.PersistentFlags().Lookup("deepgram-api-key"),
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

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func runDiscord(cmd *cobra.Command, args []string) {
	mainLogger, discordLogger, deepgramLogger := createLoggers()

	discordToken := viper.GetString("discord_token")
	deepgramAPIKey := viper.GetString("deepgram_api_key")

	if discordToken == "" {
		mainLogger.Fatal("missing DISCORD_TOKEN or --discord-token=")
	}

	if deepgramAPIKey == "" {
		mainLogger.Fatal("missing DEEPGRAM_API_KEY or --deepgram-api-key=")
	}

	db.InitDB()
	defer db.Close()

	transcriptionService, err := stt.NewDeepgramClient(
		deepgramAPIKey,
		deepgramLogger,
	)
	if err != nil {
		mainLogger.Fatal("create deepgram client", "error", err.Error())
	}

	bot, err = discordbot.NewBot(
		discordToken,
		transcriptionService,
		discordLogger,
	)
	if err != nil {
		mainLogger.Fatal("start discord bot", "error", err.Error())
	}
	defer bot.Close()

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
}

func createLoggers() (mainLogger, discordLogger, deepgramLogger *log.Logger) {
	mainLogger = logger.WithPrefix("app")
	discordLogger = logger.WithPrefix("yap")
	deepgramLogger = logger.WithPrefix("ear")
	return
}
