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

type UserSpeechStream struct {
	UserID               string
	StreamID             string
	InitialTimestamp     uint32
	InitialReceiveTime   int64
	InitialSequence      uint16
	TranscriptionSession speech.LiveTranscriptionSession
	Avatar               string
	GuildID              string
	ChannelID            string
	bot                  *Bot
}

type Bot struct {
	logger      *log.Logger
	session     *dis.Session
	transcriber speech.ASR
	streams     map[uint32]*UserSpeechStream
	userIDTable map[uint32]string
	mutex       sync.RWMutex
}

func NewBot(
	token string,
	asr speech.ASR,
	logger *log.Logger,
) (*Bot, error) {
	bot := &Bot{
		transcriber: asr,
		logger:      logger,
		streams:     make(map[uint32]*UserSpeechStream),
		userIDTable: make(map[uint32]string),
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

	bot.session = dg
	bot.logger.Info(
		"bot started",
		"username",
		bot.session.State.User.Username,
	)
	return bot, nil
}

func (bot *Bot) Close() error {
	return bot.session.Close()
}

func (bot *Bot) handleGuildCreate(
	_ *dis.Session,
	event *dis.GuildCreate,
) {
	bot.logger.Info("joined guild", "guild", event.Guild.Name)
	err := bot.joinAllVoiceChannels(event.Guild.ID)
	if err != nil {
		bot.logger.Error(
			"failed to join voice channels",
			"error",
			err.Error(),
		)
	}
}

func (bot *Bot) joinVoiceChannel(guildID, channelID string) error {
	vc, err := bot.session.ChannelVoiceJoin(
		guildID,
		channelID,
		false,
		false,
	)
	if err != nil {
		return fmt.Errorf("failed to join voice channel: %w", err)
	}

	bot.logger.Info("joined voice channel", "channel", channelID)
	go bot.handleVoiceConnection(vc, guildID, channelID)
	return nil
}

func (bot *Bot) joinAllVoiceChannels(guildID string) error {
	channels, err := bot.session.GuildChannels(guildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == dis.ChannelTypeGuildVoice {
			err := bot.joinVoiceChannel(guildID, channel.ID)
			if err != nil {
				bot.logger.Error(
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
	guildID, channelID string,
) {

	vc.AddHandler(bot.handleVoiceSpeakingUpdate)

	for {
		packet, ok := <-vc.OpusRecv
		if !ok {
			bot.logger.Info("voice channel closed")
			break
		}

		err := bot.processVoicePacket(packet, guildID, channelID)
		if err != nil {
			bot.logger.Error(
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
	bot.logger.Info(
		"userIDTable",
		"ssrc",
		v.SSRC,
		"userID",
		v.UserID,
		"speaking",
		v.Speaking,
	)
	bot.mutex.Lock()
	bot.userIDTable[uint32(v.SSRC)] = v.UserID
	bot.mutex.Unlock()
}

func (bot *Bot) processVoicePacket(
	packet *dis.Packet,
	guildID, channelID string,
) error {
	stream, err := bot.getOrCreateVoiceStream(packet, guildID, channelID)
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
	guildID, channelID string,
) (*UserSpeechStream, error) {
	bot.mutex.RLock()
	stream, exists := bot.streams[packet.SSRC]
	bot.mutex.RUnlock()

	if exists {
		return stream, nil
	}

	streamID := uuid.New().String()

	bot.mutex.RLock()
	userIDStr := bot.userIDTable[packet.SSRC]
	bot.mutex.RUnlock()

	transcriptionSession, err := bot.transcriber.Start(context.Background())
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
		GuildID:              guildID,
		ChannelID:            channelID,
		bot:                  bot,
	}

	bot.mutex.Lock()
	bot.streams[packet.SSRC] = stream
	bot.mutex.Unlock()

	err = db.CreateVoiceStream(
		guildID,
		channelID,
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

	bot.logger.Info(
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
		s.handlePhrase(phrase)
	}
}

func (s *UserSpeechStream) handlePhrase(phrase <-chan string) {
	var final string

	for draft := range phrase {
		final = draft
	}

	if final != "" {
		if strings.EqualFold(final, "Change my identity.") {
			s.handleAvatarChangeRequest()
			return
		}

		_, err := s.bot.session.ChannelMessageSend(
			s.ChannelID,
			fmt.Sprintf("%s %s", s.Avatar, final),
		)

		if err != nil {
			s.bot.logger.Error(
				"failed to send transcribed message",
				"error",
				err.Error(),
			)
		}

		err = db.SaveTranscript(
			s.GuildID,
			s.ChannelID,
			final,
		)
		if err != nil {
			s.bot.logger.Error(
				"failed to save transcript to database",
				"error",
				err.Error(),
			)
		}
	}
}

func (s *UserSpeechStream) handleAvatarChangeRequest() {
	s.Avatar = getRandomAvatar()
	_, err := s.bot.session.ChannelMessageSend(
		s.ChannelID,
		fmt.Sprintf("You are now %s.", s.Avatar),
	)
	if err != nil {
		s.bot.logger.Error(
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
