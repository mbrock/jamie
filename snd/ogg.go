package snd

import (
	"fmt"
	"io"
	"time"

	"github.com/charmbracelet/log"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

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

type RealTimeProvider struct{}

func (r *RealTimeProvider) Now() time.Time {
	return time.Now()
}

type OggWriterWrapper struct {
	writer *oggwriter.OggWriter
}

func NewOggWriter(w io.Writer) (*OggWriterWrapper, error) {
	writer, err := oggwriter.NewWith(w, 48000, 2)
	if err != nil {
		return nil, fmt.Errorf("failed to create OggWriter: %w", err)
	}
	return &OggWriterWrapper{
		writer: writer,
	}, nil
}

func (o *OggWriterWrapper) WriteRTP(packet *rtp.Packet) error {
	return o.writer.WriteRTP(packet)
}

func (o *OggWriterWrapper) Close() error {
	return o.writer.Close()
}

func createRTPPacket(
	sequenceNumber uint16,
	timestamp uint32,
	ssrc uint32,
	payload []byte,
) *rtp.Packet {
	log.Debug("rtp",
		"seq", sequenceNumber,
		"ts", timestamp,
	)
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

type OpusPacket struct {
	ID        int
	Sequence  uint16
	Timestamp uint32
	CreatedAt time.Time
	OpusData  []byte
}

type Ogg struct {
	ssrc              int64
	startTime         time.Time
	endTime           time.Time
	oggWriter         OggWriter
	timeProvider      TimeProvider
	logger            Logger
	packetCount       int
	firstTimestamp    time.Time
	lastTimestamp     time.Time
	lastSegmentNumber uint64
	gapCount          int
	segmentNumber     uint64
}

func NewOgg(
	ssrc int64,
	startTime, endTime time.Time,
	oggWriter OggWriter,
	timeProvider TimeProvider,
	logger Logger,
) (*Ogg, error) {
	return &Ogg{
		ssrc:         ssrc,
		startTime:    startTime.UTC(),
		endTime:      endTime.UTC(),
		oggWriter:    oggWriter,
		timeProvider: timeProvider,
		logger:       logger,
	}, nil
}

func (o *Ogg) Close() error {
	if o.oggWriter != nil {
		if err := o.oggWriter.Close(); err != nil {
			return fmt.Errorf("failed to close OggWriter: %w", err)
		}
	}

	log.Info("Summary",
		"total_packets", o.packetCount,
		"time_range", o.lastTimestamp.Sub(o.firstTimestamp),
		"gap_count", o.gapCount,
	)

	return nil
}

func (o *Ogg) WritePacket(packet OpusPacket) error {
	log.Info("WritePacket",
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

	err := o.writeRTPPacket(packet.OpusData)
	if err != nil {
		return err
	}

	o.lastTimestamp = packet.CreatedAt
	//	o.segmentNumber = uint64(packet.Sequence)
	o.packetCount++

	return nil
}

func (o *Ogg) WriteSilence(duration time.Duration) error {
	silentFrames := int(duration / (20 * time.Millisecond))
	err := o.writeSilentFrames(silentFrames)
	if err != nil {
		return err
	}
	o.lastTimestamp = o.lastTimestamp.Add(duration)
	return nil
}

func (o *Ogg) addInitialSilence(createdAt time.Time) {
	if createdAt.After(o.startTime) {
		silenceDuration := createdAt.Sub(o.startTime)
		silentFrames := int(
			silenceDuration.Milliseconds() / 20,
		) // 20ms per frame
		err := o.writeSilentFrames(silentFrames)
		if err != nil {
			log.Error("Error adding initial silence", "error", err)
			return
		}
		log.Info("Added initial silence", "duration", silenceDuration,
			"created_at", createdAt,
			"start_time", o.startTime,
			"frames", silentFrames,
		)
	}
}

func (o *Ogg) handleGap(packet OpusPacket) time.Duration {
	if o.lastTimestamp.IsZero() {
		return 0 // No gap for the first packet
	}

	expectedTimestamp := o.lastTimestamp.Add(20 * time.Millisecond)
	actualGap := packet.CreatedAt.Sub(expectedTimestamp)

	log.Info(
		"Actual gap",
		"gap",
		actualGap,
		"expected",
		expectedTimestamp,
		"created_at",
		packet.CreatedAt,
		"last_timestamp",
		o.lastTimestamp,
	)

	if actualGap > 20*time.Millisecond {
		// Calculate frames without rounding
		silentFrames := int(actualGap / (20 * time.Millisecond))
		gapDuration := time.Duration(silentFrames) * 20 * time.Millisecond

		log.Info("Audio gap detected",
			"gap_duration", gapDuration,
			"silent_frames", silentFrames,
			"created_at", packet.CreatedAt,
		)

		err := o.writeSilentFrames(silentFrames)
		if err != nil {
			log.Error("Error handling gap", "error", err)
			return 0
		}
		o.gapCount++
		return gapDuration
	}
	return 0
}

func (o *Ogg) writeSilentFrames(frames int) error {
	for i := 0; i < frames; i++ {
		err := o.writeRTPPacket(
			[]byte{0xf8, 0xff, 0xfe},
		) // Silent Opus packet
		if err != nil {
			return fmt.Errorf("error writing silent frame: %w", err)
		}
	}
	o.packetCount += frames
	return nil
}

func (o *Ogg) writeRTPPacket(payload []byte) error {
	o.segmentNumber++
	rtpPacket := createRTPPacket(
		uint16(
			o.segmentNumber,
		),
		uint32(o.segmentNumber*960),
		uint32(o.ssrc),
		payload,
	)

	if err := o.oggWriter.WriteRTP(rtpPacket); err != nil {
		return fmt.Errorf("error writing RTP packet: %w", err)
	}
	return nil
}
