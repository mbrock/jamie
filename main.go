package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/log"

	"jamie/db"
	"jamie/discord"
	"jamie/speech"
)

var (
	DiscordToken  string
	DeepgramToken string
	HttpPort      string
	logger        *log.Logger
	bot           *discord.DiscordBot
)

func init() {
	DiscordToken = os.Getenv("DISCORD_TOKEN")
	if DiscordToken == "" {
		fmt.Println("No Discord token provided. Please set the DISCORD_TOKEN environment variable.")
		os.Exit(1)
	}

	DeepgramToken = os.Getenv("DEEPGRAM_API_KEY")
	if DeepgramToken == "" {
		fmt.Println("No Deepgram token provided. Please set the DEEPGRAM_API_KEY environment variable.")
		os.Exit(1)
	}

	HttpPort = os.Getenv("PORT")
	if HttpPort == "" {
		HttpPort = "8080" // Default port if not specified
	}

	logger = log.New(os.Stdout)
}

func main() {
	db.InitDB()
	defer db.Close()

	go startHTTPServer()

	transcriptionService, err := speech.NewDeepgramClient(DeepgramToken)
	if err != nil {
		logger.Fatal("Error creating Deepgram client", "error", err.Error())
	}

	bot, err = discord.NewDiscordBot(DiscordToken, transcriptionService)
	if err != nil {
		logger.Fatal("Error starting Discord bot", "error", err.Error())
	}
	defer bot.Close()

	logger.Info("Bot is now running. Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
}

func startHTTPServer() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", handleRoot)
	mux.HandleFunc("GET /guild/{guildID}/channel/{channelID}/{format}", handleGuildRequest)

	logger.Info("Starting HTTP server", "port", HttpPort)
	err := http.ListenAndServe(":"+HttpPort, mux)
	if err != nil {
		logger.Error("HTTP server error", "error", err.Error())
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

func handleTranscriptHTML(w http.ResponseWriter, r *http.Request, guildID, channelID string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
	<title>Voice Channel Transcript</title>
	<style>
		body { font-family: Arial, sans-serif; }
		.transcript { margin-bottom: 10px; }
	</style>
</head>
<body>
	<h1>Voice Channel Transcript</h1>
	<div id="transcripts">
`)
	handleTranscriptStream(w, r, guildID, channelID, func(transcript string) {
		fmt.Fprintf(w, "<p class=\"transcript\">%s</p>\n", template.HTMLEscapeString(transcript))
	})
	fmt.Fprintf(w, "</div></body></html>")
}

func handleTranscript(w http.ResponseWriter, r *http.Request, guildID, channelID string) {
	w.Header().Set("Content-Type", "text/plain")
	handleTranscriptStream(w, r, guildID, channelID, func(transcript string) {
		fmt.Fprintln(w, transcript)
	})
}

func handleTranscriptStream(w http.ResponseWriter, r *http.Request, guildID, channelID string, writeTranscript func(string)) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Get all previous transcripts
	allTranscripts, err := db.GetAllTranscripts(guildID, channelID)
	if err != nil {
		logger.Error("Failed to get all transcripts", "error", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Send all previous transcripts
	for _, transcript := range allTranscripts {
		writeTranscript(transcript)
	}
	flusher.Flush()

	// Start streaming new transcripts
	for {
		select {
		case <-time.After(100 * time.Millisecond):
			if currentTranscript, ok := bot.GetCurrentTranscript(guildID, channelID); ok {
				writeTranscript(currentTranscript)
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
		logger.Error("Template execution error", "error", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
