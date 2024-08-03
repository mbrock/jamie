package snd

import (
	"bytes"
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
	log.Info("rtp",
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
	writer              io.Writer
	oggWriter           *oggwriter.OggWriter
	packetCount         int
	firstTimestamp      time.Time
	lastTimestamp       time.Time
	lastPacketTimestamp uint32
	gapCount            int
	sequenceNumber      uint16
	segmentNumber       uint32
	buffer              bytes.Buffer
	flushSize           int
}

func NewOgg(
	ssrc int64,
	startTime, endTime time.Time,
	writer io.Writer,
	flushSize int,
) (*Ogg, error) {
	oggWriter, err := oggwriter.NewWith(writer, 48000, 2)
	if err != nil {
		return nil, fmt.Errorf("failed to create OggWriter: %w", err)
	}

	return &Ogg{
		ssrc:       ssrc,
		startTime:  startTime.UTC(),
		endTime:    endTime.UTC(),
		writer:     writer,
		oggWriter:  oggWriter,
		flushSize:  flushSize,
	}, nil
}

func (o *Ogg) Close() error {
	if err := o.Flush(); err != nil {
		return fmt.Errorf("failed to flush buffer: %w", err)
	}

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

func (o *Ogg) Flush() error {
	_, err := o.writer.Write(o.buffer.Bytes())
	if err != nil {
		return fmt.Errorf("error writing to output: %w", err)
	}
	o.buffer.Reset()
	return nil
}

func (o *Ogg) WritePacket(packet OpusPacket) error {
	if o.packetCount == 0 {
		o.firstTimestamp = packet.CreatedAt
		o.addInitialSilence(packet.CreatedAt)
	} else {
		o.gapCount += o.handleGap(packet.Timestamp, o.lastPacketTimestamp, packet.ID, packet.CreatedAt)
	}

	o.sequenceNumber++
	o.segmentNumber++

	rtpPacket := createRTPPacket(
		o.sequenceNumber,
		o.segmentNumber*960, // Use segment number for timestamp
		uint32(o.ssrc),
		packet.OpusData,
	)

	if err := o.oggWriter.WriteRTP(rtpPacket); err != nil {
		return fmt.Errorf("error writing RTP packet: %w", err)
	}

	o.lastTimestamp = packet.CreatedAt
	o.lastPacketTimestamp = packet.Timestamp
	o.packetCount++

	if o.buffer.Len() >= o.flushSize {
		if err := o.Flush(); err != nil {
			return fmt.Errorf("error flushing buffer: %w", err)
		}
	}

	return nil
}

func (o *Ogg) addInitialSilence(createdAt time.Time) {
	createdAtUTC := createdAt
	startTimeUTC := o.startTime
	if createdAtUTC.After(startTimeUTC) {
		silenceDuration := createdAtUTC.Sub(startTimeUTC)
		silentFrames := int(
			silenceDuration.Milliseconds() / 20,
		) // 20ms per frame
		o.writeSilentFrames(silentFrames)
		log.Info("Added initial silence", "duration", silenceDuration,
			"created_at", createdAtUTC,
			"start_time", startTimeUTC,
		)
	}
}

func (o *Ogg) handleGap(
	timestamp, lastPacketTimestamp uint32,
	id int,
	createdAt time.Time,
) int {
	timestampDiff := timestamp - lastPacketTimestamp
	if timestampDiff > 960 { // 960 represents 20ms in the Opus timestamp units
		gapDuration := time.Duration(
			timestampDiff,
		) * time.Millisecond / 48 // Convert to real time (Opus uses 48kHz)
		log.Info("Audio gap detected",
			"gap_duration", gapDuration,
			"packet_id", id,
			"created_at", createdAt,
		)

		silentFrames := int(timestampDiff / 960)
		o.writeSilentFrames(silentFrames)
		return 1
	}
	return 0
}

func (o *Ogg) writeSilentFrames(frames int) {
	for i := 0; i < frames; i++ {
		o.sequenceNumber++
		o.segmentNumber++
		silentPacket := createRTPPacket(
			o.sequenceNumber,
			o.segmentNumber*960,
			uint32(o.ssrc),
			[]byte{0xf8, 0xff, 0xfe},
		)
		if err := o.oggWriter.WriteRTP(silentPacket); err != nil {
			log.Error("Error writing silent frame", "error", err)
		}
	}
}
