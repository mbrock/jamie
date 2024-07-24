package discord

import (
	"context"
	"fmt"
	"jamie/db"
	"jamie/speech"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"
)

type ChannelInfo struct {
	GuildID   string
	ChannelID string
}

type VoiceStream struct {
	UserID               string
	StreamID             string
	InitialTimestamp     uint32
	InitialReceiveTime   int64
	InitialSequence      uint16
	TranscriptionSession speech.LiveTranscriptionSession
	Avatar               string
}

type DiscordBot struct {
	log          *log.Logger
	api          *discordgo.Session
	asr          speech.ASR
	voiceStreams *sync.Map
	userIDMap    *sync.Map
}

func NewDiscordBot(token string, asr speech.ASR, logger *log.Logger) (*DiscordBot, error) {
	bot := &DiscordBot{
		asr:          asr,
		log:          logger,
		voiceStreams: &sync.Map{},
		userIDMap:    &sync.Map{},
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	dg.AddHandler(bot.handleGuildCreate)

	err = dg.Open()
	if err != nil {
		return nil, fmt.Errorf("error opening connection: %w", err)
	}

	bot.api = dg
	bot.log.Info("bot started", "username", bot.api.State.User.Username)
	return bot, nil
}

func (bot *DiscordBot) Close() error {
	return bot.api.Close()
}

func (bot *DiscordBot) handleGuildCreate(_ *discordgo.Session, event *discordgo.GuildCreate) {
	bot.log.Info("joined guild", "guild", event.Guild.Name)
	err := bot.joinAllVoiceChannels(ChannelInfo{GuildID: event.Guild.ID, ChannelID: ""})
	if err != nil {
		bot.log.Error("failed to join voice channels", "error", err.Error())
	}
}

func (bot *DiscordBot) joinAllVoiceChannels(channelInfo ChannelInfo) error {
	channels, err := bot.api.GuildChannels(channelInfo.GuildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildVoice {
			vc, err := bot.api.ChannelVoiceJoin(channelInfo.GuildID, channel.ID, false, false)
			if err != nil {
				bot.log.Error("failed to join voice channel", "channel", channel.Name, "error", err.Error())
			} else {
				bot.log.Info("joined voice channel", "channel", channel.Name)
				go bot.handleVoiceConnection(vc, ChannelInfo{GuildID: channelInfo.GuildID, ChannelID: channel.ID})
			}
		}
	}

	return nil
}

func (bot *DiscordBot) handleVoiceConnection(vc *discordgo.VoiceConnection, channelInfo ChannelInfo) {
	bot.log.Info("handling voice connection", "guild", channelInfo.GuildID, "channel", channelInfo.ChannelID)

	vc.AddHandler(bot.handleVoiceSpeakingUpdate)

	for {
		packet, ok := <-vc.OpusRecv
		if !ok {
			bot.log.Info("voice channel closed")
			break
		}

		err := bot.processVoicePacket(packet, channelInfo)
		if err != nil {
			bot.log.Error("failed to process voice packet", "error", err.Error())
		}
	}
}

func (bot *DiscordBot) handleVoiceSpeakingUpdate(_ *discordgo.VoiceConnection, v *discordgo.VoiceSpeakingUpdate) {
	bot.log.Info("voice speaking update", "ssrc", v.SSRC, "userID", v.UserID, "speaking", v.Speaking)
	bot.userIDMap.Store(uint32(v.SSRC), v.UserID)
}

func (bot *DiscordBot) processVoicePacket(packet *discordgo.Packet, channelInfo ChannelInfo) error {
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
		return fmt.Errorf("failed to save Discord voice packet to database: %w", err)
	}

	err = stream.TranscriptionSession.SendAudio(packet.Opus)
	if err != nil {
		return fmt.Errorf("failed to send audio to ASR service: %w", err)
	}

	return nil
}

func (bot *DiscordBot) getOrCreateVoiceStream(packet *discordgo.Packet, channelInfo ChannelInfo) (*VoiceStream, error) {
	streamInterface, exists := bot.voiceStreams.Load(packet.SSRC)
	if exists {
		return streamInterface.(*VoiceStream), nil
	}

	streamID := uuid.New().String()
	userID, _ := bot.userIDMap.Load(packet.SSRC)
	userIDStr, _ := userID.(string)

	transcriptionSession, err := bot.asr.Start(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to start ASR session: %w", err)
	}

	stream := &VoiceStream{
		UserID:               userIDStr,
		StreamID:             streamID,
		InitialTimestamp:     packet.Timestamp,
		InitialReceiveTime:   time.Now().UnixNano(),
		InitialSequence:      packet.Sequence,
		TranscriptionSession: transcriptionSession,
		Avatar:               getRandomAvatar(),
	}

	bot.voiceStreams.Store(packet.SSRC, stream)

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
		return nil, fmt.Errorf("failed to create voice stream in database: %w", err)
	}

	bot.log.Info("created new voice stream", "ssrc", int(packet.SSRC), "userID", userIDStr, "streamID", streamID)

	go bot.handleTranscription(stream, channelInfo)

	return stream, nil
}

func (bot *DiscordBot) handleTranscription(stream *VoiceStream, channelInfo ChannelInfo) {
	for transcriptChan := range stream.TranscriptionSession.Read() {
		var transcript string

		for partialTranscript := range transcriptChan {
			transcript = partialTranscript
		}

		if transcript != "" {
			transcript = strings.TrimSpace(transcript)

			if strings.EqualFold(transcript, "Change my identity.") {
				stream.Avatar = getRandomAvatar()
				_, err := bot.api.ChannelMessageSend(
					channelInfo.ChannelID,
					fmt.Sprintf("You are now %s.", stream.Avatar),
				)
				if err != nil {
					bot.log.Error("failed to send identity change message", "error", err.Error())
				}
				continue
			}

			_, err := bot.api.ChannelMessageSend(
				channelInfo.ChannelID,
				fmt.Sprintf("%s %s", stream.Avatar, transcript),
			)

			if err != nil {
				bot.log.Error("failed to send transcribed message", "error", err.Error())
			}

			err = db.SaveTranscript(channelInfo.GuildID, channelInfo.ChannelID, transcript)
			if err != nil {
				bot.log.Error("failed to save transcript to database", "error", err.Error())
			}
		}
	}
}

var avatars = []string{
	"😀", "😎", "🤖", "👽", "🐱", "🐶", "🦄", "🐸", "🦉", "🦋",
	"🌈", "🌟", "🍎", "🍕", "🎸", "🚀", "🧙", "🧛", "🧜", "🧚",
}

func getRandomAvatar() string {
	return avatars[time.Now().UnixNano()%int64(len(avatars))]
}
