package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"jamie/db"
	"jamie/discord"
	"jamie/speech"
)

var (
	logger *log.Logger
	bot    *discord.DiscordBot
)

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.AddCommand(discordCmd)

	// Add persistent flags
	rootCmd.PersistentFlags().String("web-port", "8080", "Port for the HTTP server")
	rootCmd.PersistentFlags().String("discord-token", "", "Discord bot token")
	rootCmd.PersistentFlags().String("deepgram-api-key", "", "Deepgram API key")

	// Bind flags to viper
	viper.BindPFlag("web_port", rootCmd.PersistentFlags().Lookup("web-port"))
	viper.BindPFlag("discord_token", rootCmd.PersistentFlags().Lookup("discord-token"))
	viper.BindPFlag("deepgram_api_key", rootCmd.PersistentFlags().Lookup("deepgram-api-key"))
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
	mainLogger, discordLogger, deepgramLogger, httpLogger := createLoggers()

	discordToken := viper.GetString("discord_token")
	deepgramAPIKey := viper.GetString("deepgram_api_key")

	if discordToken == "" {
		mainLogger.Fatal("No Discord token provided. Please set the DISCORD_TOKEN flag or environment variable.")
	}

	if deepgramAPIKey == "" {
		mainLogger.Fatal("No Deepgram API key provided. Please set the DEEPGRAM_API_KEY flag or environment variable.")
	}

	db.InitDB()
	defer db.Close()

	go startHTTPServer(httpLogger)

	transcriptionService, err := speech.NewDeepgramClient(
		deepgramAPIKey,
		deepgramLogger,
	)
	if err != nil {
		mainLogger.Fatal("create deepgram client", "error", err.Error())
	}

	bot, err = discord.NewDiscordBot(
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

func createLoggers() (mainLogger, discordLogger, deepgramLogger, httpLogger *log.Logger) {
	mainLogger = logger.WithPrefix("app")
	discordLogger = logger.WithPrefix("yap")
	deepgramLogger = logger.WithPrefix("ear")
	httpLogger = logger.WithPrefix("web")
	return
}

func startHTTPServer(httpLogger *log.Logger) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", handleRoot)
	mux.HandleFunc(
		"GET /guild/{guildID}/channel/{channelID}/{format}",
		handleGuildRequest,
	)

	webPort := viper.GetString("web_port")
	httpLogger.Info("boot", "port", webPort)
	err := http.ListenAndServe(":"+webPort, mux)
	if err != nil {
		httpLogger.Error("error", "error", err.Error())
	}
}

func handleGuildRequest(w http.ResponseWriter, r *http.Request) {
	guildID := r.PathValue("guildID")
	channelID := r.PathValue("channelID")
	format := r.PathValue("format")

	switch format {
	case "transcript.txt":
		handleTranscript(w, r, guildID, channelID)
	case "transcript.html":
		handleTranscriptHTML(w, r, guildID, channelID)
	default:
		http.Error(w, "Invalid URL", http.StatusBadRequest)
	}
}

func handleTranscriptHTML(
	w http.ResponseWriter,
	r *http.Request,
	guildID, channelID string,
) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
	<title>Voice Channel Transcript</title>
	<style>
		body { font-family: helvetica, sans-serif; }
		.transcript { margin-bottom: 10px; }
	</style>
</head>
<body>
	<h1>Voice Channel Transcript</h1>
	<div id="transcripts">
`)
	handleTranscriptStream(w, r, guildID, channelID, func(transcript string) {
		fmt.Fprintf(
			w,
			"<p class=\"transcript\">%s</p>\n",
			template.HTMLEscapeString(transcript),
		)
	})
	fmt.Fprintf(w, "</div></body></html>")
}

func handleTranscript(
	w http.ResponseWriter,
	r *http.Request,
	guildID, channelID string,
) {
	w.Header().Set("Content-Type", "text/plain")
	handleTranscriptStream(w, r, guildID, channelID, func(transcript string) {
		fmt.Fprintln(w, transcript)
	})
}

func handleTranscriptStream(
	w http.ResponseWriter,
	r *http.Request,
	guildID, channelID string,
	writeTranscript func(string),
) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Get all previous transcripts
	allTranscripts, err := db.GetAllTranscripts(guildID, channelID)
	if err != nil {
		logger.Error("get all transcripts", "error", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Send all previous transcripts
	for _, transcript := range allTranscripts {
		writeTranscript(transcript)
	}
	flusher.Flush()

	// Get the transcript channel for this guild and channel
	transcriptChan := bot.GetTranscriptChannel(
		discord.Venue{GuildID: guildID, ChannelID: channelID},
	)

	// Start streaming new transcripts
	for {
		select {
		case singleTranscriptChan := <-transcriptChan:
			// For now, we'll only send the last string in each chan
			var lastTranscript string
			for transcript := range singleTranscriptChan {
				lastTranscript = transcript
			}
			if lastTranscript != "" {
				writeTranscript(lastTranscript)
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.New("root").Parse(`
		<!DOCTYPE html>
		<html>
		<head>
			<title>Voice Channel Transcripts</title>
		</head>
		<body>
			<h1>Voice Channel Transcripts</h1>
			<p>Access transcripts at: /guild/{guild_id}/channel/{channel_id}/transcript.txt</p>
		</body>
		</html>
	`))

	err := tmpl.Execute(w, nil)
	if err != nil {
		logger.Error("template execution", "error", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
