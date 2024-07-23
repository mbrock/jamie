package discord

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"

	"jamie/db"
	"jamie/deepgram"
)

type ChannelIdentifier struct {
	GuildID   string
	ChannelID string
}

type DiscordBot struct {
	logger             *log.Logger
	transcriptChannels sync.Map
	discordToken       string
	session            *discordgo.Session
	deepgramToken      string
}

type VoiceStream struct {
	UserID             string
	StreamID           string
	FirstOpusTimestamp uint32
	FirstReceiveTime   int64
	FirstSequence      uint16
}

type VoiceState struct {
	ssrcToUser   sync.Map
	ssrcToStream sync.Map
	guildID      string
	channelID    string
}

func (bot *DiscordBot) SetLogger(l *log.Logger) {
	bot.logger = l
	deepgram.SetLogger(l)
}

func NewDiscordBot(token string, deepgramToken string) (*DiscordBot, error) {
	bot := &DiscordBot{
		discordToken:  token,
		deepgramToken: deepgramToken,
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
	err := bot.joinAllVoiceChannels(s, ChannelIdentifier{GuildID: event.Guild.ID, ChannelID: ""})
	if err != nil {
		bot.logger.Error("Error joining voice channels", "error", err.Error())
	}
}

func (bot *DiscordBot) joinAllVoiceChannels(s *discordgo.Session, channelID ChannelIdentifier) error {
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
				channelID := ChannelIdentifier{GuildID: channelID.GuildID, ChannelID: channel.ID}
				go func() {
					bot.startDeepgramStream(vc, channelID)
				}()
			}
		}
	}

	return nil
}

func (bot *DiscordBot) startDeepgramStream(v *discordgo.VoiceConnection, channelID ChannelIdentifier) {
	bot.logger.Info("Starting Deepgram stream", "guild", channelID.GuildID, "channel", channelID.ChannelID)

	vsp := NewVoiceStreamProcessor(channelID.GuildID, channelID.ChannelID, bot.logger)

	v.AddHandler(func(vc *discordgo.VoiceConnection, vs *discordgo.VoiceSpeakingUpdate) {
		vsp.HandleVoiceStateUpdate(vs)
	})

	dgClient, err := deepgram.NewDeepgramClient(bot.deepgramToken, channelID.GuildID, channelID.ChannelID, func(guildID, channelID, transcript string) {
		bot.handleTranscript(ChannelIdentifier{GuildID: guildID, ChannelID: channelID}, transcript)
	})
	if err != nil {
		bot.logger.Error("Error creating Deepgram client", "error", err.Error())
		return
	}

	bConnected := dgClient.Connect()
	if !bConnected {
		bot.logger.Error("Failed to connect to Deepgram")
		return
	}

	for {
		opus, ok := <-v.OpusRecv
		if !ok {
			bot.logger.Info("Voice channel closed")
			break
		}
		err := dgClient.WriteBinary(opus.Opus)
		if err != nil {
			bot.logger.Error("Failed to send audio to Deepgram", "error", err.Error())
		}

		err = vsp.ProcessVoicePacket(opus)
		if err != nil {
			bot.logger.Error("Failed to process voice packet", "error", err.Error())
		}
	}

	dgClient.Stop()
}

func (bot *DiscordBot) handleTranscript(channelID ChannelIdentifier, transcript string) {
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

func (bot *DiscordBot) GetTranscriptChannel(channelID ChannelIdentifier) chan string {
	key := fmt.Sprintf("%s:%s", channelID.GuildID, channelID.ChannelID)
	ch, _ := bot.transcriptChannels.LoadOrStore(key, make(chan string))
	return ch.(chan string)
}

func (state *VoiceState) GetUserIDFromSSRC(ssrc uint32) (string, bool) {
	userID, ok := state.ssrcToUser.Load(ssrc)
	if !ok {
		return "", false
	}
	return userID.(string), true
}

func (state *VoiceState) GetStreamIDFromSSRC(ssrc uint32) (string, bool) {
	stream, ok := state.ssrcToStream.Load(ssrc)
	if !ok {
		return "", false
	}
	return stream.(VoiceStream).StreamID, true
}
