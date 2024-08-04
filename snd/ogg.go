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
	ssrc              int64
	startTime         time.Time
	endTime           time.Time
	oggWriter         OggWriter
	timeProvider      TimeProvider
	logger            Logger
	packetCount       int           // Total number of packets processed
	firstTimestamp    time.Time     // Timestamp of the first packet received
	lastTimestamp     time.Time     // Timestamp of the last packet received
	gapCount          int           // Number of gaps detected in the audio stream
	segmentNumber     uint64        // Current segment number for RTP packets
	expectedTimestamp time.Time     // Expected timestamp for the next packet
	silenceDuration   time.Duration // Total duration of inserted silence
}

// AudioStreamMetrics holds metrics about the audio stream
type AudioStreamMetrics struct {
	TotalDuration    time.Duration
	AveragePeriod    time.Duration
	TotalGapDuration time.Duration
}

// GetStreamMetrics calculates and returns metrics about the audio stream
func (o *Ogg) GetStreamMetrics() AudioStreamMetrics {
	totalDuration := o.lastTimestamp.Sub(o.firstTimestamp)
	averagePeriod := time.Duration(0)
	if o.packetCount > 1 {
		averagePeriod = totalDuration / time.Duration(o.packetCount-1)
	}

	return AudioStreamMetrics{
		TotalDuration:    totalDuration,
		AveragePeriod:    averagePeriod,
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
	if o.packetCount == 0 {
		o.firstTimestamp = packet.CreatedAt
		o.expectedTimestamp = o.startTime
	}

	silenceDuration := o.insertSilenceIfNeeded(packet.CreatedAt)
	if silenceDuration > 0 {
		packet.CreatedAt = o.expectedTimestamp.Add(silenceDuration)
	}

	if err := o.writeRTPPacket(packet.OpusData); err != nil {
		return err
	}

	o.lastTimestamp = packet.CreatedAt
	o.packetCount++
	o.expectedTimestamp = o.lastTimestamp.Add(OpusFrameDuration)

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

	silentFrames := int(silenceDuration / OpusFrameDuration)
	actualSilenceDuration := time.Duration(silentFrames) * OpusFrameDuration

	if err := o.writeSilentFrames(silentFrames); err != nil {
		o.logger.Error("Error inserting silence", "error", err)
		return 0
	}

	o.silenceDuration += actualSilenceDuration
	o.gapCount++

	return actualSilenceDuration
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
