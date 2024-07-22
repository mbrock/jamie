package discord

import (
	"fmt"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"

	"jamie/db"
	"jamie/deepgram"
)

var (
	logger             *log.Logger
	transcriptChannels sync.Map
)

func SetLogger(l *log.Logger) {
	logger = l
	deepgram.SetLogger(l)
}

func StartBot(discordToken string, deepgramToken string) (*discordgo.Session, error) {
	dg, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	dg.AddHandler(func(s *discordgo.Session, event *discordgo.GuildCreate) {
		guildCreate(s, event, deepgramToken, discordToken)
	})

	err = dg.Open()
	if err != nil {
		return nil, fmt.Errorf("error opening connection: %w", err)
	}

	logger.Info("Bot is now running.")
	return dg, nil
}

func guildCreate(s *discordgo.Session, event *discordgo.GuildCreate, deepgramToken, discordToken string) {
	logger.Info("Joined new guild", "guild", event.Guild.Name)
	err := joinAllVoiceChannels(s, event.Guild.ID, deepgramToken, discordToken)
	if err != nil {
		logger.Error("Error joining voice channels", "error", err.Error())
	}
}

func joinAllVoiceChannels(s *discordgo.Session, guildID, deepgramToken, discordToken string) error {
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
				go startDeepgramStream(s, vc, guildID, channel.ID, deepgramToken)
			}

			vc.AddHandler(voiceStateUpdate)
		}
	}

	return nil
}

func voiceStateUpdate(s *discordgo.VoiceConnection, v *discordgo.VoiceSpeakingUpdate) {
	logger.Info("Voice state update", "userID", v.UserID, "speaking", v.Speaking)
}

func startDeepgramStream(s *discordgo.Session, v *discordgo.VoiceConnection, guildID, channelID, deepgramToken string) {
	logger.Info("Starting Deepgram stream", "guild", guildID, "channel", channelID)

	dgClient, err := deepgram.NewDeepgramClient(deepgramToken, guildID, channelID, handleTranscript)
	if err != nil {
		logger.Error("Error creating Deepgram client", "error", err.Error())
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

func handleTranscript(discordToken, guildID, channelID, transcript string) {
	// Send the transcript to Discord
	s, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		logger.Error("Failed to create Discord session", "error", err.Error())
		return
	}
	defer s.Close()

	_, err = s.ChannelMessageSend(channelID, transcript)
	if err != nil {
		logger.Error("Failed to send message to Discord", "error", err.Error())
	}

	// Send the transcript to the channel
	key := fmt.Sprintf("%s:%s", guildID, channelID)
	if ch, ok := transcriptChannels.Load(key); ok {
		ch.(chan string) <- transcript
	}
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
