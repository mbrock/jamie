package discord

import (
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"

	"jamie/db"
)

type VoiceStreamProcessor struct {
	guildID   string
	channelID string
	state     *VoiceState
	logger    *log.Logger
}

func NewVoiceStreamProcessor(guildID, channelID string, logger *log.Logger) *VoiceStreamProcessor {
	return &VoiceStreamProcessor{
		guildID:   guildID,
		channelID: channelID,
		state: &VoiceState{
			guildID:   guildID,
			channelID: channelID,
		},
		logger: logger,
	}
}

func (vsp *VoiceStreamProcessor) ProcessVoicePacket(opus *discordgo.Packet) error {
	// Get or create the stream for this SSRC
	streamInterface, exists := vsp.state.ssrcToStream.Load(opus.SSRC)
	var stream VoiceStream
	if !exists {
		streamID := uuid.New().String()
		userID, ok := vsp.state.ssrcToUser.Load(opus.SSRC)
		if !ok {
			vsp.logger.Warn("User ID not found for SSRC", log.Int("SSRC", int(opus.SSRC)))
			userID = "unknown"
		}
		stream = VoiceStream{
			UserID:             userID.(string),
			StreamID:           streamID,
			FirstOpusTimestamp: opus.Timestamp,
			FirstReceiveTime:   time.Now().UnixNano(),
			FirstSequence:      opus.Sequence,
		}
		vsp.state.ssrcToStream.Store(opus.SSRC, stream)
		err := db.CreateVoiceStream(vsp.guildID, vsp.channelID, streamID, userID.(string), opus.SSRC, opus.Timestamp, stream.FirstReceiveTime, stream.FirstSequence)
		if err != nil {
			vsp.logger.Error("Failed to create voice stream", log.Err(err))
			return err
		}
		vsp.logger.Info("Created new voice stream", 
			log.String("streamID", streamID), 
			log.String("userID", userID.(string)), 
			log.Int("SSRC", int(opus.SSRC)))
	} else {
		stream = streamInterface.(VoiceStream)
	}

	// Calculate relative timestamps and sequence
	relativeOpusTimestamp := opus.Timestamp - stream.FirstOpusTimestamp
	relativeSequence := opus.Sequence - stream.FirstSequence
	receiveTime := time.Now().UnixNano()

	// Save the Discord voice packet to the database
	err := db.SaveDiscordVoicePacket(stream.StreamID, opus.Opus, relativeSequence, relativeOpusTimestamp, receiveTime)
	if err != nil {
		vsp.logger.Error("Failed to save Discord voice packet to database", log.Err(err))
		return err
	}

	// Print timestamps in seconds and user ID
	timestampSeconds := float64(relativeOpusTimestamp) / 48000.0
	vsp.logger.Info("opus", 
		log.Int("seq", int(opus.Sequence)), 
		log.Float64("t", timestampSeconds), 
		log.String("userID", stream.UserID))

	return nil
}

func (vsp *VoiceStreamProcessor) HandleVoiceStateUpdate(v *discordgo.VoiceSpeakingUpdate) {
	vsp.logger.Info("Voice state update", 
		log.String("userID", v.UserID), 
		log.Bool("speaking", v.Speaking), 
		log.Int("SSRC", int(v.SSRC)))
	vsp.state.ssrcToUser.Store(v.SSRC, v.UserID)
}
