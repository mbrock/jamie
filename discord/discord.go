package discord

import (
	vox "jamie/speech"

	"fmt"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
)

type ChannelMessage struct {
	MessageID string
	Content   string
}

type Inn struct {
	GuildID   string
	ChannelID string
}

type DiscordBot struct {
	log                *log.Logger
	api                *discordgo.Session
	asr                vox.ASR
	transcriptChannels *sync.Map
}

func NewDiscordBot(
	token string,
	asr vox.ASR,
	logger *log.Logger,
) (*DiscordBot, error) {
	bot := &DiscordBot{
		asr:                asr,
		log:                logger,
		transcriptChannels: &sync.Map{},
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

	bot.api = dg
	bot.log.Info("boot", "username", bot.api.State.User.Username)
	return bot, nil
}

func (bot *DiscordBot) Close() error {
	return bot.api.Close()
}

func (bot *DiscordBot) guildCreate(
	_ *discordgo.Session,
	event *discordgo.GuildCreate,
) {
	bot.log.Info("join", "guild", event.Guild.Name)
	err := bot.joinAllVoiceChannels(
		Inn{GuildID: event.Guild.ID, ChannelID: ""},
	)
	if err != nil {
		bot.log.Error("join voice channels", "error", err.Error())
	}
}

func (bot *DiscordBot) joinAllVoiceChannels(
	channelID Inn,
) error {
	channels, err := bot.api.GuildChannels(channelID.GuildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildVoice {
			snd, err := bot.api.ChannelVoiceJoin(
				channelID.GuildID,
				channel.ID,
				false,
				false,
			)
			if err != nil {
				bot.log.Error(
					"join",
					"channel",
					channel.Name,
					"error",
					err.Error(),
				)
			} else {
				bot.log.Info("join", "channel", channel.Name)
				inn := Inn{GuildID: channelID.GuildID, ChannelID: channel.ID}
				go func() {
					bot.hear(snd, inn)
				}()
			}
		}
	}

	return nil
}

func (bot *DiscordBot) hear(
	snd *discordgo.VoiceConnection,
	inn Inn,
) {
	bot.log.Info(
		"hark",
		"guild",
		inn.GuildID,
		"channel",
		inn.ChannelID,
	)

	ear := Hear(
		inn.GuildID,
		inn.ChannelID,
		bot.log,
		bot.asr,
		bot.api,
	)

	snd.AddHandler(
		func(_ *discordgo.VoiceConnection, evt *discordgo.VoiceSpeakingUpdate) {
			ear.Know(evt)
		},
	)

	for {
		pkt, ok := <-snd.OpusRecv
		if !ok {
			bot.log.Info("voice channel closed")
			break
		}

		err := ear.Recv(pkt)
		if err != nil {
			bot.log.Error("process voice packet", "error", err.Error())
		}
	}
}

func (bot *DiscordBot) GetTranscriptChannel(
	channelID Inn,
) chan chan string {
	key := fmt.Sprintf("%s:%s", channelID.GuildID, channelID.ChannelID)
	ch, _ := bot.transcriptChannels.LoadOrStore(key, make(chan chan string))
	return ch.(chan chan string)
}
