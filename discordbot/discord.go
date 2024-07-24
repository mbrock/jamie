package discordbot

import (
	"context"
	"fmt"
	"jamie/db"
	"jamie/etc"
	"jamie/stt"
	"jamie/txt"
	"strings"
	"sync"
	"time"

	discordsdk "github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
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

	Beginning PacketTiming
	Emoji     string

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
	mu sync.RWMutex

	log  *log.Logger
	conn *discordsdk.Session

	speechRecognitionService stt.SpeechRecognitionService

	userStreams map[uint32]*UserStream
}

func NewBot(
	discordToken string,
	speechRecognitionService stt.SpeechRecognitionService,
	logger *log.Logger,
) (*Bot, error) {
	bot := &Bot{
		speechRecognitionService: speechRecognitionService,
		log:                      logger,
		userStreams:              make(map[uint32]*UserStream),
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

	bot.conn = dg
	bot.log.Info(
		"bot started",
		"username",
		bot.conn.State.User.Username,
	)
	return bot, nil
}

func (bot *Bot) Close() error {
	return bot.conn.Close()
}

func (bot *Bot) handleGuildCreate(
	_ *discordsdk.Session,
	event *discordsdk.GuildCreate,
) {
	bot.log.Info("joined guild", "guild", event.Guild.Name)
	err := bot.joinAllVoiceChannels(GuildID(event.Guild.ID))
	if err != nil {
		bot.log.Error(
			"failed to join voice channels",
			"error",
			err.Error(),
		)
	}
}

func (bot *Bot) joinVoiceChannel(guildID GuildID, channelID ChannelID) error {
	vc, err := bot.conn.ChannelVoiceJoin(
		string(guildID),
		string(channelID),
		false,
		false,
	)
	if err != nil {
		return fmt.Errorf("failed to join voice channel: %w", err)
	}

	bot.log.Info("joined voice channel", "channel", channelID)
	go bot.handleVoiceConnection(vc, guildID, channelID)
	return nil
}

func (bot *Bot) joinAllVoiceChannels(guildID GuildID) error {
	channels, err := bot.conn.GuildChannels(string(guildID))
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == discordsdk.ChannelTypeGuildVoice {
			err := bot.joinVoiceChannel(guildID, ChannelID(channel.ID))
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
	vc *discordsdk.VoiceConnection,
	guildID GuildID,
	channelID ChannelID,
) {

	vc.AddHandler(bot.handleVoiceSpeakingUpdate)

	for {
		packet, ok := <-vc.OpusRecv
		if !ok {
			bot.log.Info("voice channel closed")
			break
		}

		err := bot.processVoicePacket(packet, guildID, channelID)
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
	_ *discordsdk.VoiceConnection,
	v *discordsdk.VoiceSpeakingUpdate,
) {
	bot.log.Info(
		"userStreams",
		"ssrc",
		v.SSRC,
		"userID",
		v.UserID,
		"speaking",
		v.Speaking,
	)
	bot.mu.Lock()
	if _, exists := bot.userStreams[uint32(v.SSRC)]; !exists {
		bot.userStreams[uint32(v.SSRC)] = &UserStream{
			UserID: UserID(v.UserID),
		}
	}
	bot.mu.Unlock()
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

	packetID := etc.Gensym()
	err = db.SavePacket(
		packetID,
		string(stream.ID),
		int(packet.Sequence),
		int(packet.Timestamp),
		packet.Opus,
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
	bot.mu.Lock()
	defer bot.mu.Unlock()

	stream, exists := bot.userStreams[packet.SSRC]
	if !exists {
		streamID := UserStreamID(etc.Gensym())

		speechRecognitionSession, err := bot.speechRecognitionService.Start(context.Background())
		if err != nil {
			return nil, fmt.Errorf("failed to start SpeechRecognitionService session: %w", err)
		}

		stream = &UserStream{
			ID:        streamID,
			GuildID:   guildID,
			ChannelID: channelID,
			Emoji:     txt.RandomAvatar(),
			Beginning: PacketTiming{
				PacketIndex: packet.Sequence,
				SampleIndex: packet.Timestamp,
				ReceivedAt:  time.Now().UnixNano(),
			},
			SpeechRecognitionSession: speechRecognitionSession,
			bot:                      bot,
		}

		bot.userStreams[packet.SSRC] = stream
	}

	if stream.UserID == "" {
		bot.log.Warn("UserID not set for stream", "ssrc", packet.SSRC)
	}

	err := db.CreateStream(
		string(stream.ID),
		int(stream.Beginning.PacketIndex),
		int(stream.Beginning.SampleIndex),
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
		stream.UserID,
		"streamID",
		stream.ID,
	)

	// Create speaker
	speakerID := etc.Gensym()
	err = db.CreateSpeaker(speakerID, string(stream.ID), stream.Emoji)
	if err != nil {
		bot.log.Error("failed to create speaker", "error", err.Error())
	}

	// Create discord speaker
	discordSpeakerID := etc.Gensym()
	err = db.CreateDiscordSpeaker(discordSpeakerID, speakerID, string(stream.UserID))
	if err != nil {
		bot.log.Error("failed to create discord speaker", "error", err.Error())
	}

	// Create discord channel stream
	channelStreamID := etc.Gensym()
	err = db.CreateDiscordChannelStream(channelStreamID, string(stream.ID), string(stream.GuildID), string(stream.ChannelID))
	if err != nil {
		bot.log.Error("failed to create discord channel stream", "error", err.Error())
	}

	// Create attribution
	attributionID := etc.Gensym()
	err = db.CreateAttribution(attributionID, string(stream.ID), speakerID)
	if err != nil {
		bot.log.Error("failed to create attribution", "error", err.Error())
	}

	go stream.SpeechRecognitionLoop()

	return stream, nil
}

func (s *UserStream) SpeechRecognitionLoop() {
	for segment := range s.SpeechRecognitionSession.Receive() {
		s.ProcessSegment(segment)
	}
}

func (s *UserStream) ProcessSegment(segmentDrafts <-chan string) {
	var final string

	for draft := range segmentDrafts {
		final = draft
	}

	if final != "" {
		if strings.EqualFold(final, "Change my identity.") {
			s.handleAvatarChangeRequest()
			return
		}

		_, err := s.bot.conn.ChannelMessageSend(
			string(s.ChannelID),
			fmt.Sprintf("%s %s", s.Emoji, final),
		)

		if err != nil {
			s.bot.log.Error(
				"failed to send transcribed message",
				"error",
				err.Error(),
			)
		}

		// Save recognition
		recognitionID := etc.Gensym()
		err = db.SaveRecognition(recognitionID, string(s.ID), 0, 0, final, 1.0) // Assuming sample_idx and sample_len are 0, and confidence is 1.0
		if err != nil {
			s.bot.log.Error(
				"failed to save recognition to database",
				"error",
				err.Error(),
			)
		}
	}
}

func (s *UserStream) handleAvatarChangeRequest() {
	s.Emoji = txt.RandomAvatar()
	_, err := s.bot.conn.ChannelMessageSend(
		string(s.ChannelID),
		fmt.Sprintf("You are now %s.", s.Emoji),
	)
	if err != nil {
		s.bot.log.Error(
			"failed to send identity change message",
			"error",
			err.Error(),
		)
	}

	// Update speaker emoji in the database
	stmt, err := db.GetDB().Prepare("UPDATE speakers SET emoji = ? WHERE stream = ?")
	if err != nil {
		s.bot.log.Error("failed to prepare update statement", "error", err.Error())
		return
	}
	defer stmt.Close()

	_, err = stmt.Exec(s.Emoji, string(s.ID))
	if err != nil {
		s.bot.log.Error("failed to update speaker emoji", "error", err.Error())
	}
}
