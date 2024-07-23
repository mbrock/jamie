package discord

import (
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"

	"jamie/db"
)

type VoiceStreamProcessor struct {
	guildID      string
	channelID    string
	logger       *log.Logger
	ssrcToUser   sync.Map
	ssrcToStream sync.Map
}

func NewVoiceStreamProcessor(guildID, channelID string, logger *log.Logger) *VoiceStreamProcessor {
	return &VoiceStreamProcessor{
		guildID:   guildID,
		channelID: channelID,
		logger:    logger,
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

	// Log packet info
	vsp.logPacketInfo(opus, stream, relativeOpusTimestamp)

	return nil
}

func (vsp *VoiceStreamProcessor) getOrCreateStream(opus *discordgo.Packet) (VoiceStream, error) {
	streamInterface, exists := vsp.ssrcToStream.Load(opus.SSRC)
	if exists {
		return streamInterface.(VoiceStream), nil
	}

	// Create new stream
	streamID := uuid.New().String()
	userID, ok := vsp.ssrcToUser.Load(opus.SSRC)
	if !ok {
		vsp.logger.Warn("User ID not found for SSRC", "SSRC", int(opus.SSRC))
		userID = "unknown"
	}

	stream := VoiceStream{
		UserID:             userID.(string),
		StreamID:           streamID,
		FirstOpusTimestamp: opus.Timestamp,
		FirstReceiveTime:   time.Now().UnixNano(),
		FirstSequence:      opus.Sequence,
	}

	vsp.ssrcToStream.Store(opus.SSRC, stream)

	if err := db.CreateVoiceStream(vsp.guildID, vsp.channelID, streamID, userID.(string), opus.SSRC, opus.Timestamp, stream.FirstReceiveTime, stream.FirstSequence); err != nil {
		return VoiceStream{}, fmt.Errorf("failed to create voice stream: %w", err)
	}

	vsp.logger.Info("Created new voice stream", 
		"streamID", streamID, 
		"userID", userID.(string), 
		"SSRC", int(opus.SSRC))

	return stream, nil
}

func (vsp *VoiceStreamProcessor) logPacketInfo(opus *discordgo.Packet, stream VoiceStream, relativeOpusTimestamp uint32) {
	timestampSeconds := float64(relativeOpusTimestamp) / 48000.0
	vsp.logger.Info("Processed voice packet", 
		"seq", int(opus.Sequence), 
		"timestamp", timestampSeconds, 
		"userID", stream.UserID)
}

func (vsp *VoiceStreamProcessor) HandleVoiceStateUpdate(v *discordgo.VoiceSpeakingUpdate) {
	vsp.logger.Info("Voice state update", 
		"userID", v.UserID, 
		"speaking", v.Speaking, 
		"SSRC", int(v.SSRC))
	vsp.ssrcToUser.Store(v.SSRC, v.UserID)
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
	return stream.(VoiceStream).StreamID, true
}
