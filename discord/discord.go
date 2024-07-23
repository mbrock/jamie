package discord

import (
	"context"
	"fmt"
	"jamie/speech"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
)

type ChannelMessage struct {
	MessageID string
	Content   string
}

type Venue struct {
	GuildID   string
	ChannelID string
}

type DiscordBot struct {
	logger               *log.Logger
	transcriptChannels   sync.Map // map[string]chan chan string
	discordToken         string
	session              *discordgo.Session
	transcriptionService speech.LiveTranscriptionService
}

func NewDiscordBot(token string, transcriptionService speech.LiveTranscriptionService, logger *log.Logger) (*DiscordBot, error) {
	bot := &DiscordBot{
		discordToken:         token,
		transcriptionService: transcriptionService,
		logger:               logger,
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
	bot.logger.Info("boot", "username", bot.session.State.User.Username)
	return bot, nil
}

func (bot *DiscordBot) Close() error {
	return bot.session.Close()
}

func (bot *DiscordBot) guildCreate(s *discordgo.Session, event *discordgo.GuildCreate) {
	bot.logger.Info("join", "guild", event.Guild.Name)
	err := bot.joinAllVoiceChannels(s, Venue{GuildID: event.Guild.ID, ChannelID: ""})
	if err != nil {
		bot.logger.Error("join voice channels", "error", err.Error())
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
				bot.logger.Error("join", "channel", channel.Name, "error", err.Error())
			} else {
				bot.logger.Info("join", "channel", channel.Name)
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
	bot.logger.Info("hark", "guild", channelID.GuildID, "channel", channelID.ChannelID)

	vsp := NewVoiceStreamProcessor(channelID.GuildID, channelID.ChannelID, bot.logger)

	v.AddHandler(func(vc *discordgo.VoiceConnection, vs *discordgo.VoiceSpeakingUpdate) {
		vsp.HandleVoiceStateUpdate(vs)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session, err := bot.transcriptionService.Start(ctx)
	if err != nil {
		bot.logger.Error("hark", "error", err.Error())
		return
	}
	defer session.Stop()
	go func() {
		for transcriptChan := range session.Transcriptions() {
			go func(tc <-chan string) {
				bot.handleTranscript(channelID, tc)
			}(transcriptChan)
		}
	}()

	for {
		opus, ok := <-v.OpusRecv
		if !ok {
			bot.logger.Info("voice channel closed")
			break
		}

		err := session.SendAudio(opus.Opus)
		if err != nil {
			bot.logger.Error("send audio", "error", err.Error())
		}

		err = vsp.ProcessVoicePacket(opus)
		if err != nil {
			bot.logger.Error("process voice packet", "error", err.Error())
		}
	}
}

func (bot *DiscordBot) handleTranscript(channelID Venue, transcriptChan <-chan string) {
	var lastMessage *ChannelMessage

	for transcript := range transcriptChan {
		if lastMessage == nil {
			// Send a new message if there's no existing message
			msg, err := bot.session.ChannelMessageSend(channelID.ChannelID, transcript)
			if err != nil {
				bot.logger.Error("send message", "error", err.Error())
				continue
			}
			lastMessage = &ChannelMessage{MessageID: msg.ID, Content: transcript}
		} else {
			// Edit the existing message
			newContent := lastMessage.Content + "\n" + transcript
			_, err := bot.session.ChannelMessageEdit(channelID.ChannelID, lastMessage.MessageID, newContent)
			if err != nil {
				bot.logger.Error("edit message", "error", err.Error())
				continue
			}
			lastMessage.Content = newContent
		}
	}
}

func (bot *DiscordBot) GetTranscriptChannel(channelID Venue) chan chan string {
	key := fmt.Sprintf("%s:%s", channelID.GuildID, channelID.ChannelID)
	ch, _ := bot.transcriptChannels.LoadOrStore(key, make(chan chan string))
	return ch.(chan chan string)
}
