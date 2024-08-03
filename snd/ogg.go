package snd

import (
	"fmt"
	"io"
	"time"

	"github.com/charmbracelet/log"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

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
	ssrc                int64
	startTime           time.Time
	endTime             time.Time
	oggWriter           *oggwriter.OggWriter
	packetCount         int
	firstTimestamp      time.Time
	lastTimestamp       time.Time
	lastPacketTimestamp uint32
	gapCount            int
	sequenceNumber      uint16
	segmentNumber       uint32
}

func NewOgg(
	ssrc int64,
	startTime, endTime time.Time,
	writer io.Writer,
) (*Ogg, error) {
	oggWriter, err := oggwriter.NewWith(writer, 48000, 2)
	if err != nil {
		return nil, fmt.Errorf("failed to create OggWriter: %w", err)
	}

	return &Ogg{
		ssrc:      ssrc,
		startTime: startTime.UTC(),
		endTime:   endTime.UTC(),
		oggWriter: oggWriter,
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
	if o.packetCount == 0 {
		o.firstTimestamp = packet.CreatedAt
		o.addInitialSilence(packet.CreatedAt)
	} else {
		o.handleGap(packet.Timestamp, packet.CreatedAt)
	}

	err := o.writeRTPPacket(packet.OpusData)
	if err != nil {
		return err
	}

	o.lastTimestamp = packet.CreatedAt
	o.lastPacketTimestamp = packet.Timestamp
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
		silentFrames := int(silenceDuration.Milliseconds() / 20) // 20ms per frame
		err := o.writeSilentFrames(silentFrames)
		if err != nil {
			log.Error("Error adding initial silence", "error", err)
			return
		}
		log.Info("Added initial silence", "duration", silenceDuration,
			"created_at", createdAt,
			"start_time", o.startTime,
		)
	}
}

func (o *Ogg) handleGap(timestamp uint32, createdAt time.Time) {
	timestampDiff := timestamp - o.lastPacketTimestamp
	if timestampDiff > 960 { // 960 represents 20ms in the Opus timestamp units
		gapDuration := time.Duration(timestampDiff) * time.Millisecond / 48 // Convert to real time (Opus uses 48kHz)
		log.Info("Audio gap detected",
			"gap_duration", gapDuration,
			"created_at", createdAt,
		)

		silentFrames := int(timestampDiff / 960)
		err := o.writeSilentFrames(silentFrames)
		if err != nil {
			log.Error("Error handling gap", "error", err)
			return
		}
		o.gapCount++
	}
}

func (o *Ogg) writeSilentFrames(frames int) error {
	for i := 0; i < frames; i++ {
		err := o.writeRTPPacket([]byte{0xf8, 0xff, 0xfe}) // Silent Opus packet
		if err != nil {
			return fmt.Errorf("error writing silent frame: %w", err)
		}
	}
	o.packetCount += frames
	return nil
}

func (o *Ogg) writeRTPPacket(payload []byte) error {
	o.sequenceNumber++
	o.segmentNumber++
	rtpPacket := createRTPPacket(
		o.sequenceNumber,
		o.segmentNumber*960, // Use segment number for timestamp
		uint32(o.ssrc),
		payload,
	)

	if err := o.oggWriter.WriteRTP(rtpPacket); err != nil {
		return fmt.Errorf("error writing RTP packet: %w", err)
	}
	return nil
}
