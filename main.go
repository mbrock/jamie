package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

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

	logger = log.New(os.Stdout)
}

func main() {
	dg, err := discordgo.New("Bot " + Token)
	if err != nil {
		logger.Fatal("Error creating Discord session", "error", err.Error())
	}

	dg.AddHandler(guildCreate)

	err = dg.Open()
	if err != nil {
		logger.Fatal("Error opening connection", "error", err.Error())
	}

	logger.Info("Bot is now running. Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	dg.Close()
}

func startDeepgramStream(v *discordgo.VoiceConnection, guildID, channelID string) {
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
	}

	callback := MyCallback{
		sb: &strings.Builder{},
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
				logger.Info("Joined voice channel", "channel", channel.Name)
				go startDeepgramStream(vc, guildID, channel.ID)
			}

			vc.AddHandler(voiceStateUpdate)
		}
	}

	return nil
}

type MyCallback struct {
	sb *strings.Builder
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
			transcript := c.sb.String()
			logger.Info("Transcript", "text", transcript)
			c.sb.Reset()
		}
	}

	return nil
}

func (c MyCallback) Open(ocr *api.OpenResponse) error {
	return nil
}

func (c MyCallback) Metadata(md *api.MetadataResponse) error {
	return nil
}

func (c MyCallback) SpeechStarted(ssr *api.SpeechStartedResponse) error {
	return nil
}

func (c MyCallback) UtteranceEnd(ur *api.UtteranceEndResponse) error {
	utterance := strings.TrimSpace(c.sb.String())
	if len(utterance) > 0 {
		logger.Info("Utterance End", "text", utterance)
		c.sb.Reset()
	}
	return nil
}

func (c MyCallback) Close(ocr *api.CloseResponse) error {
	return nil
}

func (c MyCallback) Error(er *api.ErrorResponse) error {
	logger.Error("Deepgram error", "description", er.Description)
	return nil
}

func (c MyCallback) UnhandledEvent(byData []byte) error {
	return nil
}
