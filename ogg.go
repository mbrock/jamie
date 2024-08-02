package main

import (
	"context"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

type Ogg struct {
	dbpool     *pgxpool.Pool
	ssrc       int64
	startTime  time.Time
	endTime    time.Time
	outputFile string
}

func NewOgg(dbpool *pgxpool.Pool, ssrc int64, startTime, endTime time.Time, outputFile string) *Ogg {
	return &Ogg{
		dbpool:     dbpool,
		ssrc:       ssrc,
		startTime:  startTime,
		endTime:    endTime,
		outputFile: outputFile,
	}
}

func (o *Ogg) ProcessPackets() error {
	rows, err := o.dbpool.Query(context.Background(), `
		SELECT id, sequence, timestamp, created_at, opus_data
		FROM opus_packets
		WHERE ssrc = $1 AND created_at BETWEEN $2 AND $3
		ORDER BY created_at
	`, o.ssrc, o.startTime, o.endTime)
	if err != nil {
		return err
	}
	defer rows.Close()

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
		var id int
		var sequence uint16
		var timestamp uint32
		var createdAt time.Time
		var opusData []byte
		err := rows.Scan(&id, &sequence, &timestamp, &createdAt, &opusData)
		if err != nil {
			log.Error("Error scanning row", "error", err)
			continue
		}

		if packetCount == 0 {
			firstTimestamp = createdAt
			o.addInitialSilence(oggWriter, createdAt, timestamp)
		} else {
			gapCount += o.handleGap(oggWriter, timestamp, lastPacketTimestamp, id, createdAt)
		}

		rtpPacket := &rtp.Packet{
			Header:  rtp.Header{Timestamp: timestamp},
			Payload: opusData,
		}

		if err := oggWriter.WriteRTP(rtpPacket); err != nil {
			log.Error("Error writing RTP packet", "error", err)
		}

		lastTimestamp = createdAt
		lastPacketTimestamp = timestamp
		packetCount++
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
