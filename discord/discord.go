package discord

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"

	api "github.com/deepgram/deepgram-go-sdk/pkg/api/listen/v1/websocket/interfaces"
	interfaces "github.com/deepgram/deepgram-go-sdk/pkg/client/interfaces"
	client "github.com/deepgram/deepgram-go-sdk/pkg/client/listen"

	"jamie/db"
)

var (
	logger             *log.Logger
	transcriptChannels sync.Map
)

func SetLogger(l *log.Logger) {
	logger = l
}

func StartBot(token string, deepgramToken string) (*discordgo.Session, error) {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	dg.AddHandler(guildCreate)

	err = dg.Open()
	if err != nil {
		return nil, fmt.Errorf("error opening connection: %w", err)
	}

	logger.Info("Bot is now running.")
	return dg, nil
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
				go startDeepgramStream(s, vc, guildID, channel.ID)
			}

			vc.AddHandler(voiceStateUpdate)
		}
	}

	return nil
}

func voiceStateUpdate(s *discordgo.VoiceConnection, v *discordgo.VoiceSpeakingUpdate) {
	logger.Info("Voice state update", "userID", v.UserID, "speaking", v.Speaking)
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

		// Calculate packet duration
		pcmDuration := calculatePCMDuration(opus.PCM)

		// Save the Opus packet to the database
		err = db.SaveOpusPacket(guildID, channelID, opus.Opus, opus.Sequence, pcmDuration)
		if err != nil {
			logger.Error("Failed to save Opus packet to database", "error", err.Error())
		}

		logger.Info("opus", "seq", opus.Sequence, "len", pcmDuration)
	}

	dgClient.Stop()
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
			err = db.SaveTranscript(c.guildID, c.channelID, transcript)
			if err != nil {
				logger.Error("Failed to save transcript to database", "error", err.Error())
			}

			// Send the transcript to the channel
			key := fmt.Sprintf("%s:%s", c.guildID, c.channelID)
			if ch, ok := transcriptChannels.Load(key); ok {
				ch.(chan string) <- transcript
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

func calculatePCMDuration(pcm []int16) float64 {
	sampleRate := 48000 // Discord uses 48kHz sample rate
	return float64(len(pcm)) / float64(sampleRate)
}

func GetTranscriptChannel(guildID, channelID string) chan string {
	key := fmt.Sprintf("%s:%s", guildID, channelID)
	ch, _ := transcriptChannels.LoadOrStore(key, make(chan string))
	return ch.(chan string)
}
