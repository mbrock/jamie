package main

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"

	api "github.com/deepgram/deepgram-go-sdk/pkg/api/listen/v1/websocket/interfaces"
	interfaces "github.com/deepgram/deepgram-go-sdk/pkg/client/interfaces"
	client "github.com/deepgram/deepgram-go-sdk/pkg/client/listen"
)

var (
	Token         string
	logger        *log.Logger
	DeepgramToken string
	Port          string
)

func init() {
	Token = os.Getenv("DISCORD_TOKEN")
	if Token == "" {
		fmt.Println("No Discord token provided. Please set the DISCORD_TOKEN environment variable.")
		os.Exit(1)
	}

	DeepgramToken = os.Getenv("DEEPGRAM_API_KEY")
	if DeepgramToken == "" {
		fmt.Println("No Deepgram token provided. Please set the DEEPGRAM_API_KEY environment variable.")
		os.Exit(1)
	}

	Port = os.Getenv("PORT")
	if Port == "" {
		Port = "8080" // Default port if not specified
	}

	logger = log.New(os.Stdout)
}

func main() {
	initDB()
	defer db.Close()

	dg, err := discordgo.New("Bot " + Token)
	if err != nil {
		logger.Fatal("Error creating Discord session", "error", err.Error())
	}

	dg.AddHandler(guildCreate)

	err = dg.Open()
	if err != nil {
		logger.Fatal("Error opening connection", "error", err.Error())
	}

	// Start HTTP server
	go startHTTPServer()

	logger.Info("Bot is now running. Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	dg.Close()
}

func startHTTPServer() {
	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/guild/", handleTranscript)
	logger.Info("Starting HTTP server", "port", Port)
	err := http.ListenAndServe(":"+Port, nil)
	if err != nil {
		logger.Error("HTTP server error", "error", err.Error())
	}
}

func handleTranscript(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 6 || parts[1] != "guild" || parts[3] != "channel" || parts[5] != "transcript.txt" {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	guildID := parts[2]
	channelID := parts[4]

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	lastTimestamp := time.Now()
	for {
		transcripts, err := getTranscripts(guildID, channelID)
		if err != nil {
			logger.Error("Failed to get transcripts", "error", err.Error())
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		for _, transcript := range transcripts {
			fmt.Fprintln(w, transcript)
		}
		flusher.Flush()

		select {
		case <-r.Context().Done():
			return
		default:
			time.Sleep(1 * time.Second)
			lastTimestamp = time.Now()
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

func startDeepgramStream(s *discordgo.Session, v *discordgo.VoiceConnection, guildID, channelID string) {
	logger.Info("Starting Deepgram stream", "guild", guildID, "channel", channelID)

	// Initialize Deepgram client
	ctx := context.Background()
	cOptions := &interfaces.ClientOptions{
		EnableKeepAlive: true,
	}
	tOptions := &interfaces.LiveTranscriptionOptions{
		Model:          "nova-2",
		Language:       "en-US",
		Punctuate:      true,
		Encoding:       "opus",
		Channels:       2,
		SampleRate:     48000,
		SmartFormat:    true,
		InterimResults: true,
		UtteranceEndMs: "1000",
		VadEvents:      true,
		Diarize:        true,
	}

	callback := MyCallback{
		sb:        &strings.Builder{},
		s:         s,
		channelID: channelID,
		guildID:   guildID,
	}

	dgClient, err := client.NewWebSocket(ctx, DeepgramToken, cOptions, tOptions, callback)
	if err != nil {
		logger.Error("Error creating LiveTranscription connection", "error", err.Error())
		return
	}

	bConnected := dgClient.Connect()
	if !bConnected {
		logger.Error("Failed to connect to Deepgram")
		return
	}

	// Start receiving audio
	v.Speaking(true)
	defer v.Speaking(false)

	for {
		opus, ok := <-v.OpusRecv
		if !ok {
			logger.Info("Voice channel closed")
			break
		}
		err := dgClient.WriteBinary(opus.Opus)
		if err != nil {
			logger.Error("Failed to send audio to Deepgram", "error", err.Error())
		}
	}

	dgClient.Stop()
}

func voiceStateUpdate(s *discordgo.VoiceConnection, v *discordgo.VoiceSpeakingUpdate) {
	logger.Info("Voice state update", "userID", v.UserID, "speaking", v.Speaking)
}

func guildCreate(s *discordgo.Session, event *discordgo.GuildCreate) {
	logger.Info("Joined new guild", "guild", event.Guild.Name)
	err := joinAllVoiceChannels(s, event.Guild.ID)
	if err != nil {
		logger.Error("Error joining voice channels", "error", err.Error())
	}
}

func joinAllVoiceChannels(s *discordgo.Session, guildID string) error {
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildVoice {
			vc, err := s.ChannelVoiceJoin(guildID, channel.ID, false, false)
			if err != nil {
				logger.Error("Failed to join voice channel", "channel", channel.Name, "error", err.Error())
			} else {
				transcriptURL := fmt.Sprintf("http://localhost:%s/guild/%s/channel/%s/transcript.txt", Port, guildID, channel.ID)
				logger.Info("Joined voice channel", "channel", channel.Name, "transcriptURL", transcriptURL)
				go startDeepgramStream(s, vc, guildID, channel.ID)
			}

			vc.AddHandler(voiceStateUpdate)
		}
	}

	return nil
}

type MyCallback struct {
	sb        *strings.Builder
	s         *discordgo.Session
	channelID string
	guildID   string
}

func (c MyCallback) Message(mr *api.MessageResponse) error {
	sentence := strings.TrimSpace(mr.Channel.Alternatives[0].Transcript)

	if len(mr.Channel.Alternatives) == 0 || len(sentence) == 0 {
		return nil
	}

	if mr.IsFinal {
		c.sb.WriteString(sentence)
		c.sb.WriteString(" ")

		if mr.SpeechFinal {
			transcript := strings.TrimSpace(c.sb.String())
			logger.Info("Transcript", "text", transcript)
			_, err := c.s.ChannelMessageSend(c.channelID, transcript)
			if err != nil {
				logger.Error("Failed to send message to Discord", "error", err.Error())
			}

			// Store the transcript in the database
			err = saveTranscript(c.guildID, c.channelID, transcript)
			if err != nil {
				logger.Error("Failed to save transcript to database", "error", err.Error())
			}

			c.sb.Reset()
		}
	}

	return nil
}

func (c MyCallback) Open(ocr *api.OpenResponse) error {
	logger.Info("Deepgram connection opened")
	return nil
}

func (c MyCallback) Metadata(md *api.MetadataResponse) error {
	logger.Info("Received metadata", "metadata", md)
	return nil
}

func (c MyCallback) SpeechStarted(ssr *api.SpeechStartedResponse) error {
	logger.Info("Speech started", "timestamp", ssr.Timestamp)
	return nil
}

func (c MyCallback) UtteranceEnd(ur *api.UtteranceEndResponse) error {
	logger.Info("Utterance ended", "timestamp", ur.LastWordEnd)
	return nil
}

func (c MyCallback) Close(ocr *api.CloseResponse) error {
	logger.Info("Deepgram connection closed", "reason", ocr.Type)
	return nil
}

func (c MyCallback) Error(er *api.ErrorResponse) error {
	logger.Error("Deepgram error", "type", er.Type, "description", er.Description)
	return nil
}

func (c MyCallback) UnhandledEvent(byData []byte) error {
	logger.Warn("Unhandled Deepgram event", "data", string(byData))
	return nil
}
