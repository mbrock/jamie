package discordbot

import (
	"context"
	"fmt"
	"jamie/db"
	"jamie/stt"
	"jamie/txt"
	"strings"
	"sync"
	"time"

	discordsdk "github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"
)

type PacketTiming struct {
	PacketIndex uint16
	SampleIndex uint32
	ReceivedAt  int64
}

type UserStream struct {
	ID        UserStreamID
	UserID    UserID
	ChannelID ChannelID
	GuildID   GuildID
	Emoji     string

	FirstPacketTiming PacketTiming

	SpeechRecognitionSession stt.LiveTranscriptionSession
	bot                      *Bot
}

type (
	UserID       string
	ChannelID    string
	GuildID      string
	UserStreamID string
)

type Bot struct {
	logger                   *log.Logger
	session                  *discordsdk.Session
	speechRecognitionService stt.SpeechRecognitionService
	userStreams              map[uint32]*UserStream
	peerMap                  map[uint32]UserID
	mutex                    sync.RWMutex
}

func NewBot(
	discordToken string,
	speechRecognitionService stt.SpeechRecognitionService,
	logger *log.Logger,
) (*Bot, error) {
	bot := &Bot{
		speechRecognitionService: speechRecognitionService,
		logger:                   logger,
		userStreams:              make(map[uint32]*UserStream),
		peerMap:                  make(map[uint32]UserID),
	}

	dg, err := discordsdk.New("Bot " + discordToken)
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
	_ *discordsdk.Session,
	event *discordsdk.GuildCreate,
) {
	bot.logger.Info("joined guild", "guild", event.Guild.Name)
	err := bot.joinAllVoiceChannels(GuildID(event.Guild.ID))
	if err != nil {
		bot.logger.Error(
			"failed to join voice channels",
			"error",
			err.Error(),
		)
	}
}

func (bot *Bot) joinVoiceChannel(guildID GuildID, channelID ChannelID) error {
	vc, err := bot.session.ChannelVoiceJoin(
		string(guildID),
		string(channelID),
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

func (bot *Bot) joinAllVoiceChannels(guildID GuildID) error {
	channels, err := bot.session.GuildChannels(string(guildID))
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == discordsdk.ChannelTypeGuildVoice {
			err := bot.joinVoiceChannel(guildID, ChannelID(channel.ID))
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
	vc *discordsdk.VoiceConnection,
	guildID GuildID,
	channelID ChannelID,
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
	_ *discordsdk.VoiceConnection,
	v *discordsdk.VoiceSpeakingUpdate,
) {
	bot.logger.Info(
		"peerMap",
		"ssrc",
		v.SSRC,
		"userID",
		v.UserID,
		"speaking",
		v.Speaking,
	)
	bot.mutex.Lock()
	bot.peerMap[uint32(v.SSRC)] = UserID(v.UserID)
	bot.mutex.Unlock()
}

func (bot *Bot) processVoicePacket(
	packet *discordsdk.Packet,
	guildID GuildID,
	channelID ChannelID,
) error {
	stream, err := bot.getOrCreateVoiceStream(packet, guildID, channelID)
	if err != nil {
		return fmt.Errorf("failed to get or create voice stream: %w", err)
	}

	relativeOpusTimestamp := packet.Timestamp - stream.FirstPacketTiming.SampleIndex
	relativeSequence := packet.Sequence - stream.FirstPacketTiming.PacketIndex
	receiveTime := time.Now().UnixNano()

	err = db.SaveDiscordVoicePacket(
		string(stream.ID),
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

	err = stream.SpeechRecognitionSession.SendAudio(packet.Opus)
	if err != nil {
		return fmt.Errorf("failed to send audio to SpeechRecognitionService service: %w", err)
	}

	return nil
}

func (bot *Bot) getOrCreateVoiceStream(
	packet *discordsdk.Packet,
	guildID GuildID,
	channelID ChannelID,
) (*UserStream, error) {
	bot.mutex.RLock()
	stream, exists := bot.userStreams[packet.SSRC]
	bot.mutex.RUnlock()

	if exists {
		return stream, nil
	}

	streamID := UserStreamID(uuid.New().String())

	bot.mutex.RLock()
	userIDStr := bot.peerMap[packet.SSRC]
	bot.mutex.RUnlock()

	transcriptionSession, err := bot.speechRecognitionService.Start(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to start SpeechRecognitionService session: %w", err)
	}

	stream = &UserStream{
		UserID: userIDStr,
		ID:     streamID,
		FirstPacketTiming: PacketTiming{
			SampleIndex: packet.Timestamp,
			ReceivedAt:  time.Now().UnixNano(),
			PacketIndex: packet.Sequence,
		},
		SpeechRecognitionSession: transcriptionSession,
		Emoji:                    txt.RandomAvatar(),
		GuildID:                  guildID,
		ChannelID:                channelID,
		bot:                      bot,
	}

	bot.mutex.Lock()
	bot.userStreams[packet.SSRC] = stream
	bot.mutex.Unlock()

	err = db.CreateVoiceStream(
		string(guildID),
		string(channelID),
		string(streamID),
		string(userIDStr),
		packet.SSRC,
		stream.FirstPacketTiming.SampleIndex,
		stream.FirstPacketTiming.ReceivedAt,
		stream.FirstPacketTiming.PacketIndex,
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

func (s *UserStream) listen() {
	for phrase := range s.SpeechRecognitionSession.Read() {
		s.handlePhrase(phrase)
	}
}

func (s *UserStream) handlePhrase(phrase <-chan string) {
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
			string(s.ChannelID),
			fmt.Sprintf("%s %s", s.Emoji, final),
		)

		if err != nil {
			s.bot.logger.Error(
				"failed to send transcribed message",
				"error",
				err.Error(),
			)
		}

		err = db.SaveTranscript(
			string(s.GuildID),
			string(s.ChannelID),
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

func (s *UserStream) handleAvatarChangeRequest() {
	s.Emoji = txt.RandomAvatar()
	_, err := s.bot.session.ChannelMessageSend(
		string(s.ChannelID),
		fmt.Sprintf("You are now %s.", s.Emoji),
	)
	if err != nil {
		s.bot.logger.Error(
			"failed to send identity change message",
			"error",
			err.Error(),
		)
	}
}
