package discord

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"

	"jamie/db"
	"jamie/speech"
)

type VoiceStreamProcessor struct {
	guildID              string
	channelID            string
	logger               *log.Logger
	ssrcToUser           *sync.Map
	ssrcToStream         *sync.Map
	transcriptionService speech.LiveTranscriptionService
	session              *discordgo.Session
	userCache            *sync.Map
	lastMessageID        *sync.Map
	currentSpeaker       string
}

func NewVoiceStreamProcessor(guildID, channelID string, logger *log.Logger, transcriptionService speech.LiveTranscriptionService, session *discordgo.Session) *VoiceStreamProcessor {
	return &VoiceStreamProcessor{
		guildID:              guildID,
		channelID:            channelID,
		logger:               logger,
		ssrcToUser:           &sync.Map{},
		ssrcToStream:         &sync.Map{},
		transcriptionService: transcriptionService,
		session:              session,
		userCache:            &sync.Map{},
		lastMessageID:        &sync.Map{},
		currentSpeaker:       "",
	}
}

func (vsp *VoiceStreamProcessor) ProcessVoicePacket(opus *discordgo.Packet) error {
	// Get or create the stream for this SSRC
	stream, err := vsp.getOrCreateStream(opus)
	if err != nil {
		return fmt.Errorf("failed to get or create stream: %w", err)
	}

	// Calculate relative timestamps and sequence
	relativeOpusTimestamp := opus.Timestamp - stream.FirstOpusTimestamp
	relativeSequence := opus.Sequence - stream.FirstSequence
	receiveTime := time.Now().UnixNano()

	// Save the Discord voice packet to the database
	if err := db.SaveDiscordVoicePacket(stream.StreamID, opus.Opus, relativeSequence, relativeOpusTimestamp, receiveTime); err != nil {
		return fmt.Errorf("failed to save Discord voice packet to database: %w", err)
	}

	// Send audio to the Deepgram stream
	if err := stream.DeepgramSession.SendAudio(opus.Opus); err != nil {
		return fmt.Errorf("failed to send audio to Deepgram: %w", err)
	}

	// Log packet info
	vsp.logPacketInfo(opus, stream, relativeOpusTimestamp)

	return nil
}

func (vsp *VoiceStreamProcessor) getOrCreateStream(opus *discordgo.Packet) (*VoiceStream, error) {
	streamInterface, exists := vsp.ssrcToStream.Load(opus.SSRC)
	if exists {
		return streamInterface.(*VoiceStream), nil
	}

	streamID := uuid.New().String()
	userID, ok := vsp.ssrcToUser.Load(opus.SSRC)
	if !ok {
		vsp.logger.Debug("user id not found", "ssrc", int(opus.SSRC))
		userID = ""
	}

	deepgramSession, err := vsp.transcriptionService.Start(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to start Deepgram session: %w", err)
	}

	stream := &VoiceStream{
		UserID:             userID.(string),
		StreamID:           streamID,
		FirstOpusTimestamp: opus.Timestamp,
		FirstReceiveTime:   time.Now().UnixNano(),
		FirstSequence:      opus.Sequence,
		DeepgramSession:    deepgramSession,
	}

	vsp.ssrcToStream.Store(opus.SSRC, stream)

	if err := db.CreateVoiceStream(vsp.guildID, vsp.channelID, streamID, userID.(string), opus.SSRC, opus.Timestamp, stream.FirstReceiveTime, stream.FirstSequence); err != nil {
		return nil, fmt.Errorf("failed to create voice stream: %w", err)
	}

	vsp.logger.Info("talk",
		"src", int(opus.SSRC),
		"guy", userID.(string),
		"ear", streamID,
	)

	go vsp.handleTranscriptions(stream)

	return stream, nil
}

func (vsp *VoiceStreamProcessor) handleTranscriptions(stream *VoiceStream) {
	emoji := getEmojiFromStreamID(stream.StreamID)

	for transcriptChan := range stream.DeepgramSession.Transcriptions() {
		var finalTranscript string
		for transcript := range transcriptChan {
			finalTranscript = transcript
		}
		if finalTranscript != "" {
			if vsp.currentSpeaker != stream.UserID {
				// New speaker, create a new message
				vsp.currentSpeaker = stream.UserID
				finalFormattedTranscript := fmt.Sprintf("%s: %s", emoji, finalTranscript)
				msg, err := vsp.session.ChannelMessageSend(vsp.channelID, finalFormattedTranscript)
				if err != nil {
					vsp.logger.Error("send new message", "error", err.Error())
				} else {
					vsp.lastMessageID.Store(stream.UserID, msg.ID)
				}
			} else {
				// Same speaker, edit the existing message
				lastMsgID, ok := vsp.lastMessageID.Load(stream.UserID)
				if ok {
					finalFormattedTranscript := fmt.Sprintf("%s: %s", emoji, finalTranscript)
					_, err := vsp.session.ChannelMessageEdit(vsp.channelID, lastMsgID.(string), finalFormattedTranscript)
					if err != nil {
						vsp.logger.Error("edit message", "error", err.Error())
					}
				} else {
					vsp.logger.Error("last message ID not found", "userID", stream.UserID)
				}
			}
		}
	}
}

func (vsp *VoiceStreamProcessor) getUsernameFromID(userID string) string {
	if userID == "" {
		return "Unknown User"
	}

	if cachedName, ok := vsp.userCache.Load(userID); ok {
		return cachedName.(string)
	}

	user, err := vsp.session.User(userID)
	if err != nil {
		vsp.logger.Error("fetch user", "error", err.Error())
		return userID // Fallback to userID if we can't fetch the user
	}

	vsp.userCache.Store(userID, user.Username)
	return user.Username
}

// Helper function to generate a consistent emoji based on the stream ID
func getEmojiFromStreamID(streamID string) string {
	// List of emojis to choose from
	emojis := []string{"😀", "😎", "🤖", "👽", "🐱", "🐶", "🦄", "🐸", "🦉", "🦋", "🌈", "🌟", "🍎", "🍕", "🎸", "🚀"}

	// Use the first 4 characters of the stream ID to generate a consistent index
	index := 0
	for i := 0; i < 4 && i < len(streamID); i++ {
		index += int(streamID[i])
	}

	// Use modulo to ensure the index is within the range of the emojis slice
	return emojis[index%len(emojis)]
}

// func (vsp *VoiceStreamProcessor) getUsernameFromID(userID string) string {
// 	if userID == "" {
// 		return "unknown"
// 	}

// 	if cachedName, ok := vsp.userCache.Load(userID); ok {
// 		return cachedName.(string)
// 	}

// 	user, err := vsp.session.User(userID)
// 	if err != nil {
// 		vsp.logger.Error("fetch user", "error", err.Error())
// 		return userID // Fallback to userID if we can't fetch the user
// 	}

// 	vsp.userCache.Store(userID, user.Username)
// 	return user.Username
// }

func (vsp *VoiceStreamProcessor) logPacketInfo(opus *discordgo.Packet, stream *VoiceStream, relativeOpusTimestamp uint32) {
	timestampSeconds := float64(relativeOpusTimestamp) / 48000.0
	vsp.logger.Debug("voice packet",
		"seq", int(opus.Sequence),
		"sec", timestampSeconds,
		"guy", stream.UserID,
	)
}

func (vsp *VoiceStreamProcessor) HandleVoiceStateUpdate(v *discordgo.VoiceSpeakingUpdate) {
	vsp.logger.Info("talk",
		"src", v.SSRC,
		"guy", v.UserID,
		"yap", v.Speaking,
	)
	vsp.ssrcToUser.Store(uint32(v.SSRC), v.UserID)
}

func (vsp *VoiceStreamProcessor) GetUserIDFromSSRC(ssrc uint32) (string, bool) {
	userID, ok := vsp.ssrcToUser.Load(ssrc)
	if !ok {
		return "", false
	}
	return userID.(string), true
}

func (vsp *VoiceStreamProcessor) GetStreamIDFromSSRC(ssrc uint32) (string, bool) {
	stream, ok := vsp.ssrcToStream.Load(ssrc)
	if !ok {
		return "", false
	}
	return stream.(*VoiceStream).StreamID, true
}
