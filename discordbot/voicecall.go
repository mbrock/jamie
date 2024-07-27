package discordbot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"jamie/db"
	"jamie/etc"
	"jamie/ogg"
	"sync"

	"github.com/bwmarrin/discordgo"
)

type VoiceCall struct {
	*sync.RWMutex
	Conn                *discordgo.VoiceConnection
	TalkMode            bool
	InboundAudioPackets chan *discordgo.Packet
	streamIdCache       map[string]string // cacheKey -> streamID
	GuildID             string
	ChannelID           string
}

func (bot *Bot) joinVoiceCall(guildID, channelID string) error {
	bot.mu.Lock()
	defer bot.mu.Unlock()

	if bot.voiceCall != nil {
		if err := bot.voiceCall.Conn.Disconnect(); err != nil {
			return fmt.Errorf(
				"failed to disconnect from previous voice channel: %w",
				err,
			)
		}
	}

	vc, err := bot.conn.ChannelVoiceJoin(guildID, channelID, false, false)
	if err != nil {
		return fmt.Errorf("failed to join voice channel: %w", err)
	}

	bot.log.Info("joined voice channel", "channel", channelID)

	bot.voiceCall = &VoiceCall{
		Conn:      vc,
		GuildID:   guildID,
		ChannelID: channelID,

		TalkMode: false,

		InboundAudioPackets: make(
			chan *discordgo.Packet,
			3*1000/20,
		), // 3 second audio buffer

		streamIdCache: make(map[string]string),
	}

	bot.voiceCall.Conn.AddHandler(bot.handleVoiceSpeakingUpdate)

	go bot.acceptInboundAudioPackets()
	go bot.processInboundAudioPackets()

	return nil
}

func (bot *Bot) joinAllVoiceChannels(guildID string) error {
	channels, err := bot.conn.GuildChannels(guildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildVoice {
			err := bot.joinVoiceCall(guildID, channel.ID)
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

func (bot *Bot) acceptInboundAudioPackets() {
	for packet := range bot.voiceCall.Conn.OpusRecv {
		select {
		case bot.voiceCall.InboundAudioPackets <- packet:
			// good
		default:
			bot.log.Warn(
				"voice packet channel full, dropping packet",
				"channelID",
				bot.voiceCall.ChannelID,
			)
		}
	}
}

func (bot *Bot) processInboundAudioPackets() {
	for packet := range bot.voiceCall.InboundAudioPackets {
		err := bot.processInboundAudioPacket(packet)
		if err != nil {
			bot.log.Error(
				"failed to process voice packet",
				"error", err.Error(),
				"guildID", bot.voiceCall.GuildID,
				"channelID", bot.voiceCall.ChannelID,
			)
		}
	}
}

func (bot *Bot) processInboundAudioPacket(
	packet *discordgo.Packet,
) error {
	streamID, err := bot.ensureVoiceStream(packet)

	if err != nil {
		bot.log.Error("Failed to get or create voice stream",
			"error", err,
			"guildID", bot.voiceCall.GuildID,
			"channelID", bot.voiceCall.ChannelID,
			"SSRC", packet.SSRC,
		)
		return fmt.Errorf(
			"failed to get or create voice stream: %w",
			err,
		)
	}

	recognizer, err := bot.getRecognizerForStream(streamID)
	if err != nil {
		return fmt.Errorf("failed to get recognizer for stream: %w", err)
	}

	err = recognizer.SendAudio(packet.Opus)
	if err != nil {
		return fmt.Errorf(
			"failed to send audio to speech recognition service: %w",
			err,
		)
	}

	return nil
}

func (bot *Bot) handleVoiceSpeakingUpdate(
	_ *discordgo.VoiceConnection,
	v *discordgo.VoiceSpeakingUpdate,
) {
	bot.log.Info(
		"speaking update",
		"ssrc", v.SSRC,
		"userID", v.UserID,
		"speaking", v.Speaking,
	)

	err := bot.db.UpsertVoiceState(
		context.Background(),
		db.UpsertVoiceStateParams{
			ID:         etc.Gensym(),
			Ssrc:       int64(v.SSRC),
			UserID:     v.UserID,
			IsSpeaking: v.Speaking,
		},
	)

	if err != nil {
		bot.log.Error(
			"failed to upsert voice state",
			"error", err.Error(),
			"ssrc", v.SSRC,
			"userID", v.UserID,
		)
	}
}

func (bot *Bot) ensureVoiceStream(packet *discordgo.Packet) (string, error) {
	cacheKey := fmt.Sprintf(
		"%d:%s:%s",
		packet.SSRC,
		bot.voiceCall.GuildID,
		bot.voiceCall.ChannelID,
	)

	if streamID, ok := bot.getCachedVoiceStream(cacheKey); ok {
		return streamID, nil
	}

	streamID, err := bot.findOrSaveVoiceStream(packet)
	if err != nil {
		return "", err
	}

	bot.voiceCall.Lock()
	bot.voiceCall.streamIdCache[cacheKey] = streamID
	bot.voiceCall.Unlock()

	return streamID, nil
}

func (bot *Bot) getCachedVoiceStream(cacheKey string) (string, bool) {
	bot.voiceCall.RLock()
	streamID, ok := bot.voiceCall.streamIdCache[cacheKey]
	bot.voiceCall.RUnlock()
	return streamID, ok
}

func (bot *Bot) findOrSaveVoiceStream(
	packet *discordgo.Packet,
) (string, error) {
	discordID, username, streamID, err := bot.resolveStreamForPacket(packet)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			streamID, err = bot.createStreamForPacket(
				packet,
				discordID,
				username,
			)
			if err != nil {
				return "", fmt.Errorf(
					"failed to create new voice stream: %w",
					err,
				)
			}
		} else {
			return "", fmt.Errorf("failed to find voice stream: %w", err)
		}
	}

	return streamID, nil
}

func (bot *Bot) resolveStreamForPacket(
	packet *discordgo.Packet,
) (string, string, string, error) {
	voiceState, err := bot.db.GetVoiceState(
		context.Background(),
		db.GetVoiceStateParams{
			Ssrc:   int64(packet.SSRC),
			UserID: "",
		},
	)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get voice state: %w", err)
	}

	discordID := voiceState.UserID
	username := bot.getUsernameFromID(discordID)

	streamID, err := bot.db.GetStreamForDiscordChannelAndSpeaker(
		context.Background(),
		db.GetStreamForDiscordChannelAndSpeakerParams{
			DiscordGuild:   bot.voiceCall.GuildID,
			DiscordChannel: bot.voiceCall.ChannelID,
			DiscordID:      discordID,
		},
	)
	if err != nil {
		return discordID, username, "", err
	}

	return discordID, username, streamID, nil
}

func (bot *Bot) createStreamForPacket(
	packet *discordgo.Packet,
	discordID, username string,
) (string, error) {
	streamID := etc.Gensym()
	speakerID := etc.Gensym()

	err := bot.db.CreateStream(
		context.Background(),
		db.CreateStreamParams{
			ID:              streamID,
			PacketSeqOffset: int64(packet.Sequence),
			SampleIdxOffset: int64(packet.Timestamp),
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to create new stream: %w", err)
	}

	err = bot.db.CreateDiscordChannelStream(
		context.Background(),
		db.CreateDiscordChannelStreamParams{
			ID:             etc.Gensym(),
			DiscordGuild:   bot.voiceCall.GuildID,
			DiscordChannel: bot.voiceCall.ChannelID,
			Stream:         streamID,
		},
	)
	if err != nil {
		return "", fmt.Errorf(
			"failed to create discord channel stream: %w",
			err,
		)
	}

	err = bot.db.CreateSpeaker(
		context.Background(),
		db.CreateSpeakerParams{
			ID:     speakerID,
			Stream: streamID,
			Emoji:  "", // We're not using emoji anymore
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to create speaker: %w", err)
	}

	err = bot.db.CreateDiscordSpeaker(
		context.Background(),
		db.CreateDiscordSpeakerParams{
			ID:        etc.Gensym(),
			Speaker:   speakerID,
			DiscordID: discordID,
			Ssrc:      int64(packet.SSRC),
			Username:  username,
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to create discord speaker: %w", err)
	}

	bot.log.Info(
		"created new voice stream",
		"streamID", streamID,
		"speakerID", speakerID,
		"discordID", discordID,
		"username", username,
	)

	return streamID, nil
}

func (bot *Bot) handleVoiceStateUpdate(
	_ *discordgo.Session,
	v *discordgo.VoiceStateUpdate,
) {
	if v.UserID == bot.conn.State.User.ID {
		return
	}
}

func (bot *Bot) speakInChannel(
	channelID string,
	text string,
) error {
	// Set the speaking flag
	bot.speakingMu.Lock()
	bot.isSpeaking = true
	bot.speakingMu.Unlock()
	defer func() {
		bot.speakingMu.Lock()
		bot.isSpeaking = false
		bot.speakingMu.Unlock()
	}()

	// Find the voice channel associated with the text channel
	channel, err := bot.conn.Channel(channelID)
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}

	guild, err := bot.conn.State.Guild(channel.GuildID)
	if err != nil {
		return fmt.Errorf("failed to get guild: %w", err)
	}

	var voiceChannelID string
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID != "" {
			voiceChannelID = vs.ChannelID
			break
		}
	}

	if voiceChannelID == "" {
		return fmt.Errorf("no active voice channel found")
	}

	bot.mu.Lock()
	// Join the voice channel if not already connected
	if bot.voiceCall == nil ||
		bot.voiceCall.Conn.ChannelID != voiceChannelID {
		err := bot.joinVoiceCall(guild.ID, voiceChannelID)
		if err != nil {
			bot.mu.Unlock()
			return fmt.Errorf("failed to join voice channel: %w", err)
		}
	}
	voiceChannel := bot.voiceCall
	bot.mu.Unlock()

	// Generate speech
	speechData, err := bot.TextToSpeech(text)
	if err != nil {
		return fmt.Errorf("failed to generate speech: %w", err)
	}

	// Convert to Opus packets
	opusPackets, err := ogg.ConvertToOpus(speechData)
	if err != nil {
		return fmt.Errorf("failed to convert to Opus: %w", err)
	}

	// Send Opus packets
	bot.log.Debug("Starting to send Opus packets")
	err = voiceChannel.Conn.Speaking(true)
	if err != nil {
		return err
	}

	bot.log.Debug("Speaking true")

	for _, packet := range opusPackets {
		voiceChannel.Conn.OpusSend <- packet
	}

	bot.log.Debug("Finished sending all Opus packets")

	err = voiceChannel.Conn.Speaking(false)
	if err != nil {
		return err
	}

	return nil
}
