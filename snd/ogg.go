package snd

import (
	"fmt"
	"io"
	"time"

	"github.com/charmbracelet/log"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

// Constants
const (
	OpusFrameDuration = 20 * time.Millisecond
	SampleRate        = 48000
	Channels          = 2
)

// Interfaces
type TimeProvider interface {
	Now() time.Time
}

type OggWriter interface {
	WriteRTP(*rtp.Packet) error
	Close() error
}

type Logger interface {
	Info(interface{}, ...interface{})
	Error(interface{}, ...interface{})
	Debug(interface{}, ...interface{})
}

// RealTimeProvider implements TimeProvider
type RealTimeProvider struct{}

func (r *RealTimeProvider) Now() time.Time {
	return time.Now()
}

// OggWriterWrapper wraps oggwriter.OggWriter to implement OggWriter interface
type OggWriterWrapper struct {
	writer *oggwriter.OggWriter
}

func NewOggWriter(w io.Writer) (*OggWriterWrapper, error) {
	writer, err := oggwriter.NewWith(w, SampleRate, Channels)
	if err != nil {
		return nil, fmt.Errorf("failed to create OggWriter: %w", err)
	}
	return &OggWriterWrapper{writer: writer}, nil
}

func (o *OggWriterWrapper) WriteRTP(packet *rtp.Packet) error {
	return o.writer.WriteRTP(packet)
}

func (o *OggWriterWrapper) Close() error {
	return o.writer.Close()
}

// createRTPPacket creates an RTP packet with the given parameters
func createRTPPacket(sequenceNumber uint16, timestamp uint32, ssrc uint32, payload []byte) *rtp.Packet {
	log.Debug("Creating RTP packet", "seq", sequenceNumber, "ts", timestamp)
	return &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    0x78,
			SequenceNumber: sequenceNumber,
			Timestamp:      timestamp,
			SSRC:           ssrc,
		},
		Payload: payload,
	}
}

// OpusPacket represents an Opus audio packet
type OpusPacket struct {
	ID        int
	Sequence  uint16
	Timestamp uint32
	CreatedAt time.Time
	OpusData  []byte
}

// Ogg represents an Ogg container for Opus audio
type Ogg struct {
	ssrc           int64
	startTime      time.Time
	endTime        time.Time
	oggWriter      OggWriter
	timeProvider   TimeProvider
	logger         Logger
	packetCount    int
	firstTimestamp time.Time
	lastTimestamp  time.Time
	gapCount       int
	segmentNumber  uint64
}

// NewOgg creates a new Ogg instance
func NewOgg(ssrc int64, startTime, endTime time.Time, oggWriter OggWriter, timeProvider TimeProvider, logger Logger) (*Ogg, error) {
	return &Ogg{
		ssrc:         ssrc,
		startTime:    startTime.UTC(),
		endTime:      endTime.UTC(),
		oggWriter:    oggWriter,
		timeProvider: timeProvider,
		logger:       logger,
	}, nil
}

// Close finalizes the Ogg container and logs summary information
func (o *Ogg) Close() error {
	if o.oggWriter != nil {
		if err := o.oggWriter.Close(); err != nil {
			return fmt.Errorf("failed to close OggWriter: %w", err)
		}
	}

	o.logger.Info("Ogg processing summary",
		"total_packets", o.packetCount,
		"time_range", o.lastTimestamp.Sub(o.firstTimestamp),
		"gap_count", o.gapCount,
	)

	return nil
}

// WritePacket writes an OpusPacket to the Ogg container
func (o *Ogg) WritePacket(packet OpusPacket) error {
	o.logger.Info("Writing packet",
		"packet_count", o.packetCount,
		"packet_sequence", packet.Sequence,
		"packet_timestamp", packet.Timestamp,
		"packet_created_at", packet.CreatedAt,
	)

	if o.packetCount == 0 {
		o.firstTimestamp = packet.CreatedAt
		o.addInitialSilence(packet.CreatedAt)
	} else {
		gapDuration := o.handleGap(packet)
		if gapDuration > 0 {
			packet.CreatedAt = packet.CreatedAt.Add(gapDuration)
		}
	}

	if err := o.writeRTPPacket(packet.OpusData); err != nil {
		return err
	}

	o.lastTimestamp = packet.CreatedAt
	o.packetCount++

	return nil
}

// WriteSilence writes a duration of silence to the Ogg container
func (o *Ogg) WriteSilence(duration time.Duration) error {
	silentFrames := int(duration / OpusFrameDuration)
	if err := o.writeSilentFrames(silentFrames); err != nil {
		return err
	}
	o.lastTimestamp = o.lastTimestamp.Add(duration)
	return nil
}

// addInitialSilence adds silence at the beginning if needed
func (o *Ogg) addInitialSilence(createdAt time.Time) {
	if createdAt.After(o.startTime) {
		silenceDuration := createdAt.Sub(o.startTime)
		silentFrames := int(silenceDuration / OpusFrameDuration)
		if err := o.writeSilentFrames(silentFrames); err != nil {
			o.logger.Error("Error adding initial silence", "error", err)
			return
		}
		o.logger.Info("Added initial silence",
			"duration", silenceDuration,
			"created_at", createdAt,
			"start_time", o.startTime,
			"frames", silentFrames,
		)
	}
}

// handleGap detects and handles gaps between packets
func (o *Ogg) handleGap(packet OpusPacket) time.Duration {
	if o.lastTimestamp.IsZero() {
		return 0 // No gap for the first packet
	}

	expectedTimestamp := o.lastTimestamp.Add(OpusFrameDuration)
	actualGap := packet.CreatedAt.Sub(expectedTimestamp)

	o.logger.Info("Packet gap analysis",
		"gap", actualGap,
		"expected", expectedTimestamp,
		"created_at", packet.CreatedAt,
		"last_timestamp", o.lastTimestamp,
	)

	if actualGap > OpusFrameDuration {
		silentFrames := int(actualGap / OpusFrameDuration)
		gapDuration := time.Duration(silentFrames) * OpusFrameDuration

		o.logger.Info("Audio gap detected",
			"gap_duration", gapDuration,
			"silent_frames", silentFrames,
			"created_at", packet.CreatedAt,
		)

		if err := o.writeSilentFrames(silentFrames); err != nil {
			o.logger.Error("Error handling gap", "error", err)
			return 0
		}
		o.gapCount++
		return gapDuration
	}
	return 0
}

// writeSilentFrames writes a number of silent frames to the Ogg container
func (o *Ogg) writeSilentFrames(frames int) error {
	silentOpusPacket := []byte{0xf8, 0xff, 0xfe} // Silent Opus packet
	for i := 0; i < frames; i++ {
		if err := o.writeRTPPacket(silentOpusPacket); err != nil {
			return fmt.Errorf("error writing silent frame: %w", err)
		}
	}
	o.packetCount += frames
	return nil
}

// writeRTPPacket writes an RTP packet to the Ogg container
func (o *Ogg) writeRTPPacket(payload []byte) error {
	o.segmentNumber++
	rtpPacket := createRTPPacket(
		uint16(o.segmentNumber),
		uint32(o.segmentNumber*960),
		uint32(o.ssrc),
		payload,
	)

	if err := o.oggWriter.WriteRTP(rtpPacket); err != nil {
		return fmt.Errorf("error writing RTP packet: %w", err)
	}
	return nil
}
