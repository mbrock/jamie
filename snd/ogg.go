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
func createRTPPacket(
	sequenceNumber uint16,
	timestamp uint32,
	ssrc uint32,
	payload []byte,
) *rtp.Packet {
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
	// Core components
	logger       Logger
	oggWriter    OggWriter
	timeProvider TimeProvider

	// Session information
	ssrc      int64
	startTime time.Time
	endTime   time.Time

	// Packet tracking
	packetCount       uint64
	firstTimestamp    time.Time
	lastTimestamp     time.Time
	expectedTimestamp time.Time

	// Statistics
	gapCount        int
	silenceDuration time.Duration
}

// Helper functions for Ogg struct
func (o *Ogg) totalDuration() time.Duration {
	return o.lastTimestamp.Sub(o.firstTimestamp)
}

func (o *Ogg) averagePeriod() time.Duration {
	if o.packetCount > 1 {
		return o.totalDuration() / time.Duration(o.packetCount-1)
	}
	return 0
}

func (o *Ogg) isFirstPacket() bool {
	return o.packetCount == 0
}

func (o *Ogg) updateTimestamps(packetTime time.Time) {
	if o.isFirstPacket() {
		o.firstTimestamp = packetTime
		o.expectedTimestamp = o.startTime
	}
	o.lastTimestamp = packetTime
	o.expectedTimestamp = o.lastTimestamp.Add(OpusFrameDuration)
}

// AudioStreamMetrics holds metrics about the audio stream
type AudioStreamMetrics struct {
	TotalDuration    time.Duration
	AveragePeriod    time.Duration
	TotalGapDuration time.Duration
}

// GetStreamMetrics calculates and returns metrics about the audio stream
func (o *Ogg) GetStreamMetrics() AudioStreamMetrics {
	return AudioStreamMetrics{
		TotalDuration:    o.totalDuration(),
		AveragePeriod:    o.averagePeriod(),
		TotalGapDuration: o.silenceDuration,
	}
}

// NewOgg creates a new Ogg instance
func NewOgg(
	ssrc int64,
	startTime, endTime time.Time,
	oggWriter OggWriter,
	timeProvider TimeProvider,
	logger Logger,
) (*Ogg, error) {
	return &Ogg{
		ssrc:         ssrc,
		startTime:    startTime,
		endTime:      endTime,
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

	metrics := o.GetStreamMetrics()
	o.logger.Info("Ogg processing summary",
		"total_packets", o.packetCount,
		"total_duration", metrics.TotalDuration,
		"average_period", metrics.AveragePeriod,
		"gap_count", o.gapCount,
		"total_gap_duration", metrics.TotalGapDuration,
	)

	return nil
}

// WritePacket writes an OpusPacket to the Ogg container
func (o *Ogg) WritePacket(packet OpusPacket) error {
	if o.isFirstPacket() {
		silenceDuration := packet.CreatedAt.Sub(o.startTime)
		if silenceDuration > 0 {
			if err := o.WriteSilence(silenceDuration); err != nil {
				return err
			}
		}
		o.firstTimestamp = packet.CreatedAt
		o.expectedTimestamp = packet.CreatedAt
	} else {
		silenceDuration := o.insertSilenceIfNeeded(packet.CreatedAt)
		if silenceDuration > 0 {
			packet.CreatedAt = o.expectedTimestamp.Add(silenceDuration)
		}
	}

	if err := o.writeRTPPacket(packet.OpusData); err != nil {
		return err
	}

	o.updateTimestamps(packet.CreatedAt)
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

// insertSilenceIfNeeded adds silence if there's a gap between the expected and actual timestamp
func (o *Ogg) insertSilenceIfNeeded(packetTimestamp time.Time) time.Duration {
	if packetTimestamp.Before(o.expectedTimestamp) {
		return 0
	}

	silenceDuration := packetTimestamp.Sub(o.expectedTimestamp)
	if silenceDuration < OpusFrameDuration {
		return 0
	}

	silenceDuration = silenceDuration.Truncate(OpusFrameDuration)
	silentFrames := int(silenceDuration / OpusFrameDuration)

	if err := o.writeSilentFrames(silentFrames); err != nil {
		o.logger.Error("Error inserting silence", "error", err)
		return 0
	}

	o.silenceDuration += silenceDuration
	o.gapCount++

	return silenceDuration
}

// writeSilentFrames writes a number of silent frames to the Ogg container
func (o *Ogg) writeSilentFrames(frames int) error {
	silentOpusPacket := []byte{0xf8, 0xff, 0xfe} // Silent Opus packet
	for i := 0; i < frames; i++ {
		if err := o.writeRTPPacket(silentOpusPacket); err != nil {
			return fmt.Errorf("error writing silent frame: %w", err)
		}
	}
	o.packetCount += uint64(frames)
	return nil
}

// writeRTPPacket writes an RTP packet to the Ogg container
func (o *Ogg) writeRTPPacket(payload []byte) error {
	o.packetCount++
	rtpPacket := createRTPPacket(
		uint16(o.packetCount),
		uint32(o.packetCount*960),
		uint32(o.ssrc),
		payload,
	)

	if err := o.oggWriter.WriteRTP(rtpPacket); err != nil {
		return fmt.Errorf("error writing RTP packet: %w", err)
	}
	return nil
}
