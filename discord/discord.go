package discord

import (
	"context"
	"fmt"
	"jamie/db"
	"jamie/speech"
	"strings"
	"sync"
	"time"

	dis "github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"
)

type ChannelInfo struct {
	GuildID   string
	ChannelID string
}

type UserSpeechStream struct {
	UserID               string
	StreamID             string
	InitialTimestamp     uint32
	InitialReceiveTime   int64
	InitialSequence      uint16
	TranscriptionSession speech.LiveTranscriptionSession
	Avatar               string
	ChannelInfo          ChannelInfo
	bot                  *Bot
}

type Bot struct {
	logger         *log.Logger
	session        *dis.Session
	transcriber    speech.ASR
	voiceStreams   map[uint32]*UserSpeechStream
	userIDTable    map[uint32]string
	mutex          sync.RWMutex
}

func NewBot(
	token string,
	asr speech.ASR,
	logger *log.Logger,
) (*Bot, error) {
	bot := &Bot{
		asr: asr,
		log: logger,
		vox: make(map[uint32]*UserSpeechStream),
		tab: make(map[uint32]string),
	}

	dg, err := dis.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	dg.AddHandler(bot.handleGuildCreate)

	err = dg.Open()
	if err != nil {
		return nil, fmt.Errorf("error opening connection: %w", err)
	}

	bot.con = dg
	bot.log.Info("bot started", "username", bot.con.State.User.Username)
	return bot, nil
}

func (bot *Bot) Close() error {
	return bot.con.Close()
}

func (bot *Bot) handleGuildCreate(
	_ *dis.Session,
	event *dis.GuildCreate,
) {
	bot.log.Info("joined guild", "guild", event.Guild.Name)
	err := bot.joinAllVoiceChannels(event.Guild.ID)
	if err != nil {
		bot.log.Error("failed to join voice channels", "error", err.Error())
	}
}

func (bot *Bot) joinVoiceChannel(guildID, channelID string) error {
	vc, err := bot.con.ChannelVoiceJoin(
		guildID,
		channelID,
		false,
		false,
	)
	if err != nil {
		return fmt.Errorf("failed to join voice channel: %w", err)
	}

	bot.log.Info("joined voice channel", "channel", channelID)
	go bot.handleVoiceConnection(
		vc,
		ChannelInfo{GuildID: guildID, ChannelID: channelID},
	)
	return nil
}

func (bot *Bot) joinAllVoiceChannels(guildID string) error {
	channels, err := bot.con.GuildChannels(guildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == dis.ChannelTypeGuildVoice {
			err := bot.joinVoiceChannel(guildID, channel.ID)
			if err != nil {
				bot.log.Error(
					"failed to join voice channel",
					"channel",
					channel.Name,
					"error",
					err.Error(),
				)
			}
		}
	}

	return nil
}

func (bot *Bot) handleVoiceConnection(
	vc *dis.VoiceConnection,
	channelInfo ChannelInfo,
) {

	vc.AddHandler(bot.handleVoiceSpeakingUpdate)

	for {
		packet, ok := <-vc.OpusRecv
		if !ok {
			bot.log.Info("voice channel closed")
			break
		}

		err := bot.processVoicePacket(packet, channelInfo)
		if err != nil {
			bot.log.Error(
				"failed to process voice packet",
				"error",
				err.Error(),
			)
		}
	}
}

func (bot *Bot) handleVoiceSpeakingUpdate(
	_ *dis.VoiceConnection,
	v *dis.VoiceSpeakingUpdate,
) {
	bot.log.Info(
		"tab",
		"ssrc",
		v.SSRC,
		"userID",
		v.UserID,
		"speaking",
		v.Speaking,
	)
	bot.mut.Lock()
	bot.tab[uint32(v.SSRC)] = v.UserID
	bot.mut.Unlock()
}

func (bot *Bot) processVoicePacket(
	packet *dis.Packet,
	channelInfo ChannelInfo,
) error {
	stream, err := bot.getOrCreateVoiceStream(packet, channelInfo)
	if err != nil {
		return fmt.Errorf("failed to get or create voice stream: %w", err)
	}

	relativeOpusTimestamp := packet.Timestamp - stream.InitialTimestamp
	relativeSequence := packet.Sequence - stream.InitialSequence
	receiveTime := time.Now().UnixNano()

	err = db.SaveDiscordVoicePacket(
		stream.StreamID,
		packet.Opus,
		relativeSequence,
		relativeOpusTimestamp,
		receiveTime,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to save Discord voice packet to database: %w",
			err,
		)
	}

	err = stream.TranscriptionSession.SendAudio(packet.Opus)
	if err != nil {
		return fmt.Errorf("failed to send audio to ASR service: %w", err)
	}

	return nil
}

func (bot *Bot) getOrCreateVoiceStream(
	packet *dis.Packet,
	channelInfo ChannelInfo,
) (*UserSpeechStream, error) {
	bot.mut.RLock()
	stream, exists := bot.vox[packet.SSRC]
	bot.mut.RUnlock()

	if exists {
		return stream, nil
	}

	streamID := uuid.New().String()

	bot.mut.RLock()
	userIDStr := bot.tab[packet.SSRC]
	bot.mut.RUnlock()

	transcriptionSession, err := bot.asr.Start(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to start ASR session: %w", err)
	}

	stream = &UserSpeechStream{
		UserID:               userIDStr,
		StreamID:             streamID,
		InitialTimestamp:     packet.Timestamp,
		InitialReceiveTime:   time.Now().UnixNano(),
		InitialSequence:      packet.Sequence,
		TranscriptionSession: transcriptionSession,
		Avatar:               getRandomAvatar(),
		ChannelInfo:          channelInfo,
		bot:                  bot,
	}

	bot.mut.Lock()
	bot.vox[packet.SSRC] = stream
	bot.mut.Unlock()

	err = db.CreateVoiceStream(
		channelInfo.GuildID,
		channelInfo.ChannelID,
		streamID,
		userIDStr,
		packet.SSRC,
		packet.Timestamp,
		stream.InitialReceiveTime,
		stream.InitialSequence,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to create voice stream in database: %w",
			err,
		)
	}

	bot.log.Info(
		"created new voice stream",
		"ssrc",
		int(packet.SSRC),
		"userID",
		userIDStr,
		"streamID",
		streamID,
	)

	go stream.listen()

	return stream, nil
}

func (s *UserSpeechStream) listen() {
	for phrase := range s.TranscriptionSession.Read() {
		var final string

		for draft := range phrase {
			final = draft
		}

		if final != "" {
			if strings.EqualFold(final, "Change my identity.") {
				s.handleAvatarChangeRequest()
				continue
			}

			_, err := s.bot.con.ChannelMessageSend(
				s.ChannelInfo.ChannelID,
				fmt.Sprintf("%s %s", s.Avatar, final),
			)

			if err != nil {
				s.bot.log.Error(
					"failed to send transcribed message",
					"error",
					err.Error(),
				)
			}

			err = db.SaveTranscript(
				s.ChannelInfo.GuildID,
				s.ChannelInfo.ChannelID,
				final,
			)
			if err != nil {
				s.bot.log.Error(
					"failed to save transcript to database",
					"error",
					err.Error(),
				)
			}
		}
	}
}

func (s *UserSpeechStream) handleAvatarChangeRequest() {
	s.Avatar = getRandomAvatar()
	_, err := s.bot.con.ChannelMessageSend(
		s.ChannelInfo.ChannelID,
		fmt.Sprintf("You are now %s.", s.Avatar),
	)
	if err != nil {
		s.bot.log.Error(
			"failed to send identity change message",
			"error",
			err.Error(),
		)
	}
}

var avatars = []string{
	"😀", "😎", "🤖", "👽", "🐱", "🐶", "🦄", "🐸", "🦉", "🦋",
	"🌈", "🌟", "🍎", "🍕", "🎸", "🚀", "🧙", "🧛", "🧜", "🧚",
}

func getRandomAvatar() string {
	return avatars[time.Now().UnixNano()%int64(len(avatars))]
}
