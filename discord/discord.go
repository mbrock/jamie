package discord

import (
	"fmt"
	"os"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"

	"jamie/deepgram"
)

type Venue struct {
	GuildID   string
	ChannelID string
}

import (
	"jamie/deepgram"
	// ... other imports
)

type DiscordBot struct {
	logger             *log.Logger
	transcriptChannels sync.Map
	discordToken       string
	session            *discordgo.Session
	transcriptionService deepgram.LiveTranscriptionService
}

func (bot *DiscordBot) SetLogger(l *log.Logger) {
	bot.logger = l
}

func NewDiscordBot(token string, transcriptionService deepgram.LiveTranscriptionService) (*DiscordBot, error) {
	bot := &DiscordBot{
		discordToken:  token,
		transcriptionService: transcriptionService,
		logger:        log.New(os.Stderr),
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	dg.AddHandler(func(s *discordgo.Session, event *discordgo.GuildCreate) {
		bot.guildCreate(s, event)
	})

	err = dg.Open()
	if err != nil {
		return nil, fmt.Errorf("error opening connection: %w", err)
	}

	bot.session = dg
	bot.logger.Info("Bot is now running.")
	return bot, nil
}

func (bot *DiscordBot) Close() error {
	return bot.session.Close()
}

func (bot *DiscordBot) guildCreate(s *discordgo.Session, event *discordgo.GuildCreate) {
	bot.logger.Info("Joined new guild", "guild", event.Guild.Name)
	err := bot.joinAllVoiceChannels(s, Venue{GuildID: event.Guild.ID, ChannelID: ""})
	if err != nil {
		bot.logger.Error("Error joining voice channels", "error", err.Error())
	}
}

func (bot *DiscordBot) joinAllVoiceChannels(s *discordgo.Session, channelID Venue) error {
	channels, err := s.GuildChannels(channelID.GuildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildVoice {
			vc, err := s.ChannelVoiceJoin(channelID.GuildID, channel.ID, false, false)
			if err != nil {
				bot.logger.Error("Failed to join voice channel", "channel", channel.Name, "error", err.Error())
			} else {
				bot.logger.Info("Joined voice channel", "channel", channel.Name)
				channelID := Venue{GuildID: channelID.GuildID, ChannelID: channel.ID}
				go func() {
					bot.startDeepgramStream(vc, channelID)
				}()
			}
		}
	}

	return nil
}

func (bot *DiscordBot) startDeepgramStream(v *discordgo.VoiceConnection, channelID Venue) {
	bot.logger.Info("Starting transcription stream", "guild", channelID.GuildID, "channel", channelID.ChannelID)

	vsp := NewVoiceStreamProcessor(channelID.GuildID, channelID.ChannelID, bot.logger)

	v.AddHandler(func(vc *discordgo.VoiceConnection, vs *discordgo.VoiceSpeakingUpdate) {
		vsp.HandleVoiceStateUpdate(vs)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := bot.transcriptionService.Start(ctx)
	if err != nil {
		bot.logger.Error("Failed to start transcription service", "error", err.Error())
		return
	}

	go func() {
		for transcript := range bot.transcriptionService.Transcriptions() {
			bot.handleTranscript(channelID, transcript)
		}
	}()

	for {
		opus, ok := <-v.OpusRecv
		if !ok {
			bot.logger.Info("Voice channel closed")
			break
		}
		err := bot.transcriptionService.SendAudio(opus.Opus)
		if err != nil {
			bot.logger.Error("Failed to send audio to transcription service", "error", err.Error())
		}

		err = vsp.ProcessVoicePacket(opus)
		if err != nil {
			bot.logger.Error("Failed to process voice packet", "error", err.Error())
		}
	}

	bot.transcriptionService.Stop()
}

func (bot *DiscordBot) handleTranscript(channelID Venue, transcript string) {
	// Send the transcript to Discord
	_, err := bot.session.ChannelMessageSend(channelID.ChannelID, transcript)
	if err != nil {
		bot.logger.Error("Failed to send message to Discord", "error", err.Error())
	}

	// Send the transcript to the channel
	key := fmt.Sprintf("%s:%s", channelID.GuildID, channelID.ChannelID)
	if ch, ok := bot.transcriptChannels.Load(key); ok {
		ch.(chan string) <- transcript
	}
}

func (bot *DiscordBot) GetTranscriptChannel(channelID Venue) chan string {
	key := fmt.Sprintf("%s:%s", channelID.GuildID, channelID.ChannelID)
	ch, _ := bot.transcriptChannels.LoadOrStore(key, make(chan string))
	return ch.(chan string)
}
