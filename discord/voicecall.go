package discord

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"jamie/ai"
	"jamie/audio"
	"jamie/db"
	"jamie/etc"
	"sync"

	"github.com/bwmarrin/discordgo"
	"layeh.com/gopus"
)

type VoiceChat struct {
	sync.RWMutex
	Conn                *discordgo.VoiceConnection
	TalkMode            bool
	InboundAudioPackets chan *discordgo.Packet
	streamIdCache       map[string]string
	GuildID             string
	ChannelID           string
	Transcribers        map[string][]ai.SpeechRecognitionSession
}

func (bot *Bot) joinVoiceCall(guildID, channelID string) error {
	bot.mu.Lock()
	defer bot.mu.Unlock()

	if bot.voiceChat != nil {
		if err := bot.voiceChat.Conn.Disconnect(); err != nil {
			return fmt.Errorf(
				"failed to disconnect from previous voice channel: %w",
				err,
			)
		}
	}

	vc, err := bot.discord.ChannelVoiceJoin(guildID, channelID, false, false)
	if err != nil {
		return fmt.Errorf("failed to join voice channel: %w", err)
	}

	sessionID := vc.SessionID
	bot.log.Info("joined", "channel", channelID, "session", sessionID)

	bot.voiceChat = &VoiceChat{
		Conn:      vc,
		GuildID:   guildID,
		ChannelID: channelID,

		TalkMode: bot.defaultTalkMode,

		InboundAudioPackets: make(
			chan *discordgo.Packet,
			3*1000/20,
		), // 3 second audio buffer

		streamIdCache: make(map[string]string),
		Transcribers:  make(map[string][]ai.SpeechRecognitionSession),
	}

	bot.voiceChat.Conn.AddHandler(bot.handleVoiceSpeakingUpdate)

	go bot.acceptInboundAudioPackets()
	go bot.processInboundAudioPackets()

	return nil
}

func (bot *Bot) joinAllVoiceChannels(guildID string) error {
	channels, err := bot.discord.GuildChannels(guildID)
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
	for packet := range bot.voiceChat.Conn.OpusRecv {
		select {
		case bot.voiceChat.InboundAudioPackets <- packet:
			// good
		default:
			bot.log.Warn(
				"voice packet channel full, dropping packet",
				"channelID",
				bot.voiceChat.ChannelID,
			)
		}
	}
}

func (bot *Bot) processInboundAudioPackets() {
	for packet := range bot.voiceChat.InboundAudioPackets {
		err := bot.processInboundAudioPacket(packet)
		if err != nil {
			bot.log.Error(
				"failed to process voice packet",
				"error", err.Error(),
				"guildID", bot.voiceChat.GuildID,
				"channelID", bot.voiceChat.ChannelID,
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
			"guildID", bot.voiceChat.GuildID,
			"channelID", bot.voiceChat.ChannelID,
			"SSRC", packet.SSRC,
		)
		return fmt.Errorf(
			"failed to get or create voice stream: %w",
			err,
		)
	}

	// Save the audio packet
	err = bot.db.InsertVoicePacket(context.Background(), db.InsertVoicePacketParams{
		ID:            etc.NewFreshID(),
		VoiceStreamID: streamID,
		Sequence:      int64(packet.Sequence),
		SampleIdx:     int64(packet.Timestamp),
		Payload:       packet.Opus,
	})
	if err != nil {
		bot.log.Error("Failed to save audio packet",
			"error", err,
			"streamID", streamID,
		)
		// Continue processing even if saving fails
	}

	recognizers, err := bot.getRecognizersForStream(streamID)
	if err != nil {
		return fmt.Errorf("failed to get recognizers for stream: %w", err)
	}

	for _, recognizer := range recognizers {
		err = recognizer.SendAudio(packet.Opus, int64(packet.Timestamp))
		if err != nil {
			bot.log.Error(
				"Failed to send audio to speech recognition service",
				"error", err,
				"streamID", streamID,
			)
			// Continue with other recognizers even if one fails
			continue
		}
	}

	return nil
}

func (bot *Bot) handleVoiceSpeakingUpdate(
	_ *discordgo.VoiceConnection,
	v *discordgo.VoiceSpeakingUpdate,
) {
	bot.log.Info(
		"state",
		"speaking", v.Speaking,
		"userID", v.UserID,
		"ssrc", v.SSRC,
	)
}

func (bot *Bot) ensureVoiceStream(packet *discordgo.Packet) (string, error) {
	cacheKey := fmt.Sprintf(
		"%d:%s:%s",
		packet.SSRC,
		bot.voiceChat.GuildID,
		bot.voiceChat.ChannelID,
	)

	if streamID, ok := bot.getCachedVoiceStream(cacheKey); ok {
		return streamID, nil
	}

	streamID, err := bot.findOrSaveVoiceStream(packet)
	if err != nil {
		return "", err
	}

	bot.voiceChat.Lock()
	bot.voiceChat.streamIdCache[cacheKey] = streamID
	bot.voiceChat.Unlock()

	return streamID, nil
}

func (bot *Bot) getCachedVoiceStream(cacheKey string) (string, bool) {
	bot.voiceChat.RLock()
	streamID, ok := bot.voiceChat.streamIdCache[cacheKey]
	bot.voiceChat.RUnlock()
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
	stream, err := bot.db.GetVoiceStreamForSsrc(
		context.Background(),
		db.GetVoiceStreamForSsrcParams{
			Ssrc:           int64(packet.SSRC),
			VoiceSessionID: bot.voiceChat.Conn.SessionID,
		},
	)

	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", nil
	}

	if err != nil {
		return "", "", "", fmt.Errorf("failed to get voice stream: %w", err)
	}

	return stream.DiscordUserID, bot.getUsernameFromID(stream.DiscordUserID), stream.ID, nil
}

func (bot *Bot) createStreamForPacket(
	packet *discordgo.Packet,
	discordID, username string,
) (string, error) {
	streamID := etc.NewFreshID()
	speakerID := etc.NewFreshID()

	err := bot.db.CreateVoiceStream(
		context.Background(),
		db.CreateVoiceStreamParams{
			ID:             streamID,
			VoiceSessionID: bot.voiceChat.Conn.SessionID,
			Ssrc:           int64(packet.SSRC),
			DiscordUserID:  discordID,
		},
	)

	if err != nil {
		return "", fmt.Errorf("failed to create new stream: %w", err)
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
	me, err := bot.discord.MyUserID()

	if err != nil {
		bot.log.Error("Failed to get bot's user ID", "error", err)
		return
	}

	if v.UserID == me {
		return
	}

}

func (bot *Bot) speakInChannel(
	ctx context.Context,
	channelID string,
	text string,
) error {
	bot.log.Info(
		"Starting speakInChannel",
		"channelID",
		channelID,
		"text",
		text,
	)
	defer bot.log.Info("Finished speakInChannel", "channelID", channelID)

	bot.setSpeakingFlag(true)
	defer bot.setSpeakingFlag(false)

	voiceChannel := bot.voiceChat
	if err := voiceChannel.Conn.Speaking(true); err != nil {
		return fmt.Errorf("failed to set speaking state: %w", err)
	}
	defer func(Conn *discordgo.VoiceConnection, b bool) {
		err := Conn.Speaking(b)
		if err != nil {
			bot.log.Warn("set speaking state", "error", err)
		}
	}(voiceChannel.Conn, false)

	mpeg, errChan := bot.textToSpeechMpegStream(ctx, text)
	int16Chan, err := audio.DecodeMpeg20msPCM(ctx, mpeg)
	if err != nil {
		return err
	}

	return bot.streamPcmToDiscordAsOpusPackets(
		ctx,
		int16Chan,
		errChan,
		voiceChannel,
	)
}

func (bot *Bot) setSpeakingFlag(speaking bool) {
	bot.speakingMu.Lock()
	defer bot.speakingMu.Unlock()
	bot.isSpeaking = speaking
}

func (bot *Bot) textToSpeechMpegStream(
	ctx context.Context,
	text string,
) (<-chan []byte, <-chan error) {
	mp3Chan := make(chan []byte)
	errChan := make(chan error, 1)

	go func() {
		defer close(mp3Chan)
		bot.log.Debug("Starting text-to-speech conversion")
		if err := bot.TextToSpeech(ctx, text, channelWriter{mp3Chan}); err != nil {
			bot.log.Error("Failed to generate speech", "error", err)
			errChan <- err
		}
		bot.log.Debug("Finished text-to-speech conversion")
	}()

	return mp3Chan, errChan
}

func (bot *Bot) streamPcmToDiscordAsOpusPackets(
	ctx context.Context,
	pcmChan <-chan []int16,
	errChan <-chan error,
	voiceChannel *VoiceChat,
) error {
	encoder, err := gopus.NewEncoder(48000, 2, gopus.Audio)
	if err != nil {
		return fmt.Errorf("failed to create Opus encoder: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errChan:
			return err
		case pcmData, ok := <-pcmChan:
			if !ok {
				bot.log.Info("Speech completed normally")
				return nil
			}
			if err := bot.encodeAndSendFrame(ctx, encoder, pcmData, voiceChannel); err != nil {
				return err
			}
		}
	}
}

func (bot *Bot) encodeAndSendFrame(
	ctx context.Context,
	encoder *gopus.Encoder,
	pcmData []int16,
	voiceChannel *VoiceChat,
) error {
	opusData, err := encoder.Encode(pcmData, 960, 128000)
	if err != nil {
		bot.log.Error("Failed to encode PCM to Opus", "error", err)
		return nil // Continue with the next frame
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case voiceChannel.Conn.OpusSend <- opusData:
		return nil
	}
}

type channelWriter struct {
	ch chan<- []byte
}

func (cw channelWriter) Write(p []byte) (n int, err error) {
	cw.ch <- p
	return len(p), nil
}
