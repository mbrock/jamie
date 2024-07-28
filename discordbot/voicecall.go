package discordbot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"jamie/db"
	"jamie/etc"
	"jamie/stt"
	"os/exec"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"layeh.com/gopus"
)

type VoiceCall struct {
	sync.RWMutex
	Conn                *discordgo.VoiceConnection
	TalkMode            bool
	InboundAudioPackets chan *discordgo.Packet
	streamIdCache       map[string]string // cacheKey -> streamID
	GuildID             string
	ChannelID           string
	Recognizers         map[string][]stt.SpeechRecognizer // streamID -> []SpeechRecognizer
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

	vc, err := bot.discord.ChannelVoiceJoin(guildID, channelID, false, false)
	if err != nil {
		return fmt.Errorf("failed to join voice channel: %w", err)
	}

	bot.log.Info("joined", "channel", channelID)

	bot.voiceCall = &VoiceCall{
		Conn:      vc,
		GuildID:   guildID,
		ChannelID: channelID,

		TalkMode: bot.defaultTalkMode,

		InboundAudioPackets: make(
			chan *discordgo.Packet,
			3*1000/20,
		), // 3 second audio buffer

		streamIdCache: make(map[string]string),
		Recognizers:   make(map[string][]stt.SpeechRecognizer),
	}

	bot.voiceCall.Conn.AddHandler(bot.handleVoiceSpeakingUpdate)

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

	// Save the audio packet
	err = bot.db.SavePacket(context.Background(), db.SavePacketParams{
		ID:        etc.Gensym(),
		Stream:    streamID,
		PacketSeq: int64(packet.Sequence),
		SampleIdx: int64(packet.Timestamp),
		Payload:   packet.Opus,
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
	s *discordgo.Session,
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

	// Fetch the user information
	user, err := bot.discord.User(v.UserID)
	if err != nil {
		bot.log.Error("Failed to fetch user information", "error", err, "userID", v.UserID)
		return
	}

	// Update the username in the database
	err = bot.db.UpdateDiscordSpeakerUsername(context.Background(), db.UpdateDiscordSpeakerUsernameParams{
		DiscordID: v.UserID,
		Username:  user.Username,
	})
	if err != nil {
		bot.log.Error("Failed to update username", "error", err, "userID", v.UserID, "username", user.Username)
	} else {
		bot.log.Info("Updated username", "userID", v.UserID, "username", user.Username)
	}
}

func (bot *Bot) speakInChannel(
	channelID string,
	text string,
) error {
	bot.log.Info("Starting speakInChannel", "channelID", channelID, "text", text)

	// Set the speaking flag
	bot.speakingMu.Lock()
	bot.isSpeaking = true
	bot.speakingMu.Unlock()
	defer func() {
		bot.speakingMu.Lock()
		bot.isSpeaking = false
		bot.speakingMu.Unlock()
		bot.log.Info("Finished speakInChannel", "channelID", channelID)
	}()

	// Find the voice channel associated with the text channel
	channel, err := bot.discord.Channel(channelID)
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}

	voiceStates, err := bot.discord.GuildVoiceStates(channel.GuildID)
	if err != nil {
		return fmt.Errorf("failed to get guild voice states: %w", err)
	}

	var voiceChannelID string
	for _, vs := range voiceStates {
		if vs.ChannelID != "" {
			voiceChannelID = vs.ChannelID
			break
		}
	}

	if voiceChannelID == "" {
		return fmt.Errorf("no active voice channel found")
	}

	voiceChannel := bot.voiceCall

	// Start speaking
	if err := voiceChannel.Conn.Speaking(true); err != nil {
		return fmt.Errorf("failed to set speaking state: %w", err)
	}
	defer voiceChannel.Conn.Speaking(false)

	ffmpegIn, ffmpegInWriter := io.Pipe()
	ffmpegOutReader, ffmpegOut := io.Pipe()

	ffmpegCmd := exec.Command("ffmpeg",
		"-i", "pipe:0",
		"-f", "s16le",
		"-acodec", "pcm_s16le",
		"-ar", "48000",
		"-ac", "2",
		"-fflags", "nobuffer+flush_packets",
		"-flags", "low_delay",
		"-strict", "experimental",
		"-probesize", "32",
		"-analyzeduration", "0",
		"-")
	ffmpegCmd.Stdin = ffmpegIn
	ffmpegCmd.Stdout = ffmpegOut

	err = ffmpegCmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start FFmpeg: %w", err)
	}

	go func() {
		bot.log.Debug("Starting text-to-speech conversion")
		if err := bot.TextToSpeech(text, ffmpegInWriter); err != nil {
			bot.log.Error("Failed to generate speech", "error", err)
		}
		bot.log.Debug("Finished text-to-speech conversion")
		if err := ffmpegInWriter.Close(); err != nil {
			bot.log.Error("Failed to close mp3 writer", "error", err)
		}
	}()

	// Run audio encoding and sending in a goroutine
	go func() {
		encoder, err := gopus.NewEncoder(48000, 2, gopus.Audio)
		if err != nil {
			bot.log.Error("Failed to create Opus encoder", "error", err)
			return
		}

		// Read and encode audio data in chunks
		buffer := make([]byte, 960*2*2) // 20ms of audio at 48kHz, 2 channels, 2 bytes per sample
		for {
			n, err := io.ReadFull(ffmpegOutReader, buffer)
			if err == io.EOF && n == 0 {
				bot.log.Debug("Reached end of FFmpeg output")
				break
			}

			if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
				bot.log.Error("Failed to read audio data", "error", err)
				return
			}

			// Convert byte buffer to int16 slice
			pcmBuffer := make([]int16, n/2)
			for i := 0; i < n; i += 2 {
				pcmBuffer[i/2] = int16(buffer[i]) | int16(buffer[i+1])<<8
			}

			// If we got a partial read, pad with silence
			if len(pcmBuffer) < 960*2 {
				bot.log.Debug("Padding partial read with silence", "originalLength", len(pcmBuffer))
				pcmBuffer = append(pcmBuffer, make([]int16, 960*2-len(pcmBuffer))...)
			}

			// Encode the frame to Opus
			opusData, err := encoder.Encode(pcmBuffer, 960, 128000)
			if err != nil {
				bot.log.Error("Failed to encode Opus", "error", err)
				return
			}

			voiceChannel.Conn.OpusSend <- opusData
		}

	}()
	// Wait for FFmpeg to finish
	err = ffmpegCmd.Wait()
	if err != nil {
		bot.log.Error("FFmpeg error", "error", err)
		return fmt.Errorf("FFmpeg error: %w", err)
	}

	// // Wait for FFmpeg to finish or for cancellation
	time.Sleep(2 * time.Second)

	bot.log.Info("Speech completed normally")

	if err := ffmpegOut.Close(); err != nil {
		bot.log.Error("Failed to close ffmpeg stdout", "error", err)
	}

	return nil
}
