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

type Voice struct {
	UserID               string
	StreamID             string
	InitialTimestamp     uint32
	InitialReceiveTime   int64
	InitialSequence      uint16
	TranscriptionSession speech.LiveTranscriptionSession
	Avatar               string
}

type Bot struct {
	log *log.Logger
	con *dis.Session
	asr speech.ASR
	vox map[uint32]*Voice
	tab map[uint32]string
	mut sync.RWMutex
}

func NewBot(
	token string,
	asr speech.ASR,
	logger *log.Logger,
) (*Bot, error) {
	bot := &Bot{
		asr: asr,
		log: logger,
		vox: make(map[uint32]*Voice),
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

func (bot *Bot) joinAllVoiceChannels(guildID string) error {
	channels, err := bot.con.GuildChannels(guildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == dis.ChannelTypeGuildVoice {
			vc, err := bot.con.ChannelVoiceJoin(
				guildID,
				channel.ID,
				false,
				false,
			)
			if err != nil {
				bot.log.Error(
					"failed to join voice channel",
					"channel",
					channel.Name,
					"error",
					err.Error(),
				)
			} else {
				bot.log.Info("joined voice channel", "channel", channel.Name)
				go bot.handleVoiceConnection(vc, ChannelInfo{GuildID: guildID, ChannelID: channel.ID})
			}
		}
	}

	return nil
}

func (bot *Bot) handleVoiceConnection(
	vc *dis.VoiceConnection,
	channelInfo ChannelInfo,
) {
	bot.log.Info(
		"handling voice connection",
		"guild",
		channelInfo.GuildID,
		"channel",
		channelInfo.ChannelID,
	)

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
		"voice speaking update",
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
) (*Voice, error) {
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

	stream = &Voice{
		UserID:               userIDStr,
		StreamID:             streamID,
		InitialTimestamp:     packet.Timestamp,
		InitialReceiveTime:   time.Now().UnixNano(),
		InitialSequence:      packet.Sequence,
		TranscriptionSession: transcriptionSession,
		Avatar:               getRandomAvatar(),
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

	go bot.handleTranscription(stream, channelInfo)

	return stream, nil
}

func (bot *Bot) handleTranscription(
	stream *Voice,
	channelInfo ChannelInfo,
) {
	for transcriptChan := range stream.TranscriptionSession.Read() {
		var transcript string

		for partialTranscript := range transcriptChan {
			transcript = partialTranscript
		}

		if transcript != "" {
			transcript = strings.TrimSpace(transcript)

			if strings.EqualFold(transcript, "Change my identity.") {
				stream.Avatar = getRandomAvatar()
				_, err := bot.con.ChannelMessageSend(
					channelInfo.ChannelID,
					fmt.Sprintf("You are now %s.", stream.Avatar),
				)
				if err != nil {
					bot.log.Error(
						"failed to send identity change message",
						"error",
						err.Error(),
					)
				}
				continue
			}

			_, err := bot.con.ChannelMessageSend(
				channelInfo.ChannelID,
				fmt.Sprintf("%s %s", stream.Avatar, transcript),
			)

			if err != nil {
				bot.log.Error(
					"failed to send transcribed message",
					"error",
					err.Error(),
				)
			}

			err = db.SaveTranscript(
				channelInfo.GuildID,
				channelInfo.ChannelID,
				transcript,
			)
			if err != nil {
				bot.log.Error(
					"failed to save transcript to database",
					"error",
					err.Error(),
				)
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
