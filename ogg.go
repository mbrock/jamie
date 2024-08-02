package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v4"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

type OpusPacket struct {
	ID        int
	Sequence  uint16
	Timestamp uint32
	CreatedAt time.Time
	OpusData  []byte
}

type Ogg struct {
	ssrc       int64
	startTime  time.Time
	endTime    time.Time
	outputFile string
}

func NewOgg(ssrc int64, startTime, endTime time.Time, outputFile string) *Ogg {
	return &Ogg{
		ssrc:       ssrc,
		startTime:  startTime,
		endTime:    endTime,
		outputFile: outputFile,
	}
}

func (o *Ogg) ProcessPackets(rows pgx.Rows) error {
	oggWriter, err := oggwriter.New(o.outputFile, 48000, 2)
	if err != nil {
		return err
	}
	defer oggWriter.Close()

	var packetCount int
	var firstTimestamp, lastTimestamp time.Time
	var lastPacketTimestamp uint32
	var gapCount int

	for rows.Next() {
		var packet OpusPacket
		err := rows.Scan(&packet.ID, &packet.Sequence, &packet.Timestamp, &packet.CreatedAt, &packet.OpusData)
		if err != nil {
			log.Error("Error scanning row", "error", err)
			continue
		}

		if packetCount == 0 {
			firstTimestamp = packet.CreatedAt
			o.addInitialSilence(oggWriter, packet.CreatedAt, packet.Timestamp)
		} else {
			gapCount += o.handleGap(oggWriter, packet.Timestamp, lastPacketTimestamp, packet.ID, packet.CreatedAt)
		}

		rtpPacket := &rtp.Packet{
			Header:  rtp.Header{Timestamp: packet.Timestamp},
			Payload: packet.OpusData,
		}

		if err := oggWriter.WriteRTP(rtpPacket); err != nil {
			log.Error("Error writing RTP packet", "error", err)
		}

		lastTimestamp = packet.CreatedAt
		lastPacketTimestamp = packet.Timestamp
		packetCount++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating over rows: %w", err)
	}

	log.Info("Summary",
		"total_packets", packetCount,
		"time_range", lastTimestamp.Sub(firstTimestamp),
		"gap_count", gapCount,
		"output_file", o.outputFile,
	)

	return nil
}

func (o *Ogg) addInitialSilence(oggWriter *oggwriter.OggWriter, createdAt time.Time, timestamp uint32) {
	if createdAt.After(o.startTime) {
		silenceDuration := createdAt.Sub(o.startTime)
		silentFrames := int(silenceDuration.Milliseconds() / 20) // 20ms per frame
		for i := 0; i < silentFrames; i++ {
			silentPacket := &rtp.Packet{
				Header:  rtp.Header{Timestamp: timestamp - uint32((silentFrames-i)*960)},
				Payload: []byte{0xf8, 0xff, 0xfe}, // Empty packet payload
			}
			if err := oggWriter.WriteRTP(silentPacket); err != nil {
				log.Error("Error writing initial silent frame", "error", err)
			}
		}
		log.Info("Added initial silence", "duration", silenceDuration)
	}
}

func (o *Ogg) handleGap(oggWriter *oggwriter.OggWriter, timestamp, lastPacketTimestamp uint32, id int, createdAt time.Time) int {
	timestampDiff := timestamp - lastPacketTimestamp
	if timestampDiff > 960 { // 960 represents 20ms in the Opus timestamp units
		gapDuration := time.Duration(timestampDiff) * time.Millisecond / 48 // Convert to real time (Opus uses 48kHz)
		log.Info("Audio gap detected",
			"gap_duration", gapDuration,
			"packet_id", id,
			"created_at", createdAt,
		)

		// Insert silent frames
		silentFrames := int(timestampDiff / 960)
		for i := 0; i < silentFrames; i++ {
			silentPacket := &rtp.Packet{
				Header:  rtp.Header{Timestamp: lastPacketTimestamp + uint32(i*960)},
				Payload: []byte{0xf8, 0xff, 0xfe}, // Empty packet payload
			}
			if err := oggWriter.WriteRTP(silentPacket); err != nil {
				log.Error("Error writing silent frame", "error", err)
			}
		}
		return 1
	}
	return 0
}
