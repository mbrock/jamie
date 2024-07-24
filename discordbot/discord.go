package discordbot

import (
	"context"
	"database/sql"
	"fmt"
	"jamie/db"
	"jamie/etc"
	"jamie/stt"
	"jamie/txt"
	"strings"

	discordsdk "github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
)

type Bot struct {
	log                      *log.Logger
	conn                     *discordsdk.Session
	speechRecognitionService stt.SpeechRecognitionService
	db                       *sql.DB
}

func NewBot(
	discordToken string,
	speechRecognitionService stt.SpeechRecognitionService,
	logger *log.Logger,
) (*Bot, error) {
	bot := &Bot{
		speechRecognitionService: speechRecognitionService,
		log:                      logger,
		db:                       db.GetDB(),
	}

	dg, err := discordsdk.New("Bot " + discordToken)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	dg.AddHandler(bot.handleGuildCreate)
	dg.AddHandler(bot.handleVoiceStateUpdate)

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
	err := bot.joinAllVoiceChannels(event.Guild.ID)
	if err != nil {
		bot.log.Error(
			"failed to join voice channels",
			"error",
			err.Error(),
		)
	}
}

func (bot *Bot) joinVoiceChannel(guildID, channelID string) error {
	vc, err := bot.conn.ChannelVoiceJoin(guildID, channelID, false, false)
	if err != nil {
		return fmt.Errorf("failed to join voice channel: %w", err)
	}

	bot.log.Info("joined voice channel", "channel", channelID)
	go bot.handleVoiceConnection(vc, guildID, channelID)
	return nil
}

func (bot *Bot) joinAllVoiceChannels(guildID string) error {
	channels, err := bot.conn.GuildChannels(guildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == discordsdk.ChannelTypeGuildVoice {
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
	vc *discordsdk.VoiceConnection,
	guildID, channelID string,
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
		"speaking update",
		"ssrc", v.SSRC,
		"userID", v.UserID,
		"speaking", v.Speaking,
	)
}

func (bot *Bot) processVoicePacket(
	packet *discordsdk.Packet,
	guildID, channelID string,
) error {
	streamID, err := bot.getOrCreateVoiceStream(packet, guildID, channelID)
	if err != nil {
		return fmt.Errorf("failed to get or create voice stream: %w", err)
	}

	packetID := etc.Gensym()
	err = db.SavePacket(
		packetID,
		streamID,
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

	session, err := bot.getSpeechRecognitionSession(streamID)
	if err != nil {
		return fmt.Errorf("failed to get speech recognition session: %w", err)
	}

	err = session.SendAudio(packet.Opus)
	if err != nil {
		return fmt.Errorf("failed to send audio to speech recognition service: %w", err)
	}

	return nil
}

func (bot *Bot) getOrCreateVoiceStream(
	packet *discordsdk.Packet,
	guildID, channelID string,
) (string, error) {
	var streamID string
	err := bot.db.QueryRow(
		"SELECT id FROM streams WHERE discord_guild = ? AND discord_channel = ? AND ssrc = ?",
		guildID, channelID, packet.SSRC,
	).Scan(&streamID)

	if err == sql.ErrNoRows {
		streamID = etc.Gensym()
		_, err = bot.db.Exec(
			"INSERT INTO streams (id, discord_guild, discord_channel, ssrc, packet_seq_offset, sample_idx_offset) VALUES (?, ?, ?, ?, ?, ?)",
			streamID, guildID, channelID, packet.SSRC, packet.Sequence, packet.Timestamp,
		)
		if err != nil {
			return "", fmt.Errorf("failed to create new stream: %w", err)
		}

		speakerID := etc.Gensym()
		emoji := txt.RandomAvatar()
		_, err = bot.db.Exec(
			"INSERT INTO speakers (id, stream, emoji) VALUES (?, ?, ?)",
			speakerID, streamID, emoji,
		)
		if err != nil {
			return "", fmt.Errorf("failed to create speaker: %w", err)
		}

		bot.log.Info(
			"created new voice stream",
			"ssrc", packet.SSRC,
			"streamID", streamID,
		)
	} else if err != nil {
		return "", fmt.Errorf("failed to query for stream: %w", err)
	}

	return streamID, nil
}

func (bot *Bot) getSpeechRecognitionSession(streamID string) (stt.LiveTranscriptionSession, error) {
	var session stt.LiveTranscriptionSession
	var exists bool

	err := bot.db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM speech_recognition_sessions WHERE stream = ?)",
		streamID,
	).Scan(&exists)

	if err != nil {
		return nil, fmt.Errorf("failed to check for existing speech recognition session: %w", err)
	}

	if !exists {
		session, err = bot.speechRecognitionService.Start(context.Background())
		if err != nil {
			return nil, fmt.Errorf("failed to start speech recognition session: %w", err)
		}

		_, err = bot.db.Exec(
			"INSERT INTO speech_recognition_sessions (stream, session_data) VALUES (?, ?)",
			streamID, "placeholder", // You might want to serialize the session data
		)
		if err != nil {
			return nil, fmt.Errorf("failed to save speech recognition session: %w", err)
		}

		go bot.speechRecognitionLoop(streamID, session)
	} else {
		// In a real implementation, you'd retrieve the session data and reconstruct the session
		session, err = bot.speechRecognitionService.Start(context.Background())
		if err != nil {
			return nil, fmt.Errorf("failed to recreate speech recognition session: %w", err)
		}
	}

	return session, nil
}

func (bot *Bot) speechRecognitionLoop(streamID string, session stt.LiveTranscriptionSession) {
	for segment := range session.Receive() {
		bot.processSegment(streamID, segment)
	}
}

func (bot *Bot) processSegment(streamID string, segmentDrafts <-chan string) {
	var final string

	for draft := range segmentDrafts {
		final = draft
	}

	if final != "" {
		if strings.EqualFold(final, "Change my identity.") {
			bot.handleAvatarChangeRequest(streamID)
			return
		}

		var channelID string
		var emoji string
		err := bot.db.QueryRow(
			`SELECT discord_channel, emoji 
			 FROM streams 
			 JOIN speakers ON streams.id = speakers.stream 
			 WHERE streams.id = ?`,
			streamID,
		).Scan(&channelID, &emoji)
		if err != nil {
			bot.log.Error("failed to get channel and emoji", "error", err.Error())
			return
		}

		_, err = bot.conn.ChannelMessageSend(
			channelID,
			fmt.Sprintf("%s %s", emoji, final),
		)

		if err != nil {
			bot.log.Error(
				"failed to send transcribed message",
				"error",
				err.Error(),
			)
		}

		recognitionID := etc.Gensym()
		err = db.SaveRecognition(recognitionID, streamID, 0, 0, final, 1.0)
		if err != nil {
			bot.log.Error(
				"failed to save recognition to database",
				"error",
				err.Error(),
			)
		}
	}
}

func (bot *Bot) handleAvatarChangeRequest(streamID string) {
	newEmoji := txt.RandomAvatar()

	_, err := bot.db.Exec(
		"UPDATE speakers SET emoji = ? WHERE stream = ?",
		newEmoji, streamID,
	)
	if err != nil {
		bot.log.Error("failed to update speaker emoji", "error", err.Error())
		return
	}

	var channelID string
	err = bot.db.QueryRow(
		"SELECT discord_channel FROM streams WHERE id = ?",
		streamID,
	).Scan(&channelID)
	if err != nil {
		bot.log.Error("failed to get channel ID", "error", err.Error())
		return
	}

	_, err = bot.conn.ChannelMessageSend(
		channelID,
		fmt.Sprintf("You are now %s.", newEmoji),
	)
	if err != nil {
		bot.log.Error(
			"failed to send identity change message",
			"error",
			err.Error(),
		)
	}
}

func (bot *Bot) handleVoiceStateUpdate(
	_ *discordsdk.Session,
	v *discordsdk.VoiceStateUpdate,
) {
	if v.UserID == bot.conn.State.User.ID {
		return // Ignore bot's own voice state updates
	}

	if v.ChannelID == "" {
		// User left a voice channel
		_, err := bot.db.Exec(
			"UPDATE streams SET ended_at = CURRENT_TIMESTAMP WHERE discord_guild = ? AND discord_channel = ? AND ended_at IS NULL",
			v.GuildID, v.ChannelID,
		)
		if err != nil {
			bot.log.Error("failed to update stream end time", "error", err.Error())
		}
	} else {
		// User joined or moved to a voice channel
		streamID := etc.Gensym()
		_, err := bot.db.Exec(
			"INSERT INTO streams (id, discord_guild, discord_channel) VALUES (?, ?, ?)",
			streamID, v.GuildID, v.ChannelID,
		)
		if err != nil {
			bot.log.Error("failed to create new stream for user join", "error", err.Error())
		}
	}
}
