package audio

import (
	"bytes"
	"context"
	"fmt"
	"github.com/charmbracelet/log"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
	"jamie/db"
)

func GenerateOggOpusBlob(
	log *log.Logger,
	q *db.Queries,
	streamID string,
	startSample, endSample int64,
) ([]byte, error) {
	log.Debug("Starting GenerateOggOpusBlob", "streamID", streamID, "startSample", startSample, "endSample", endSample)

	packets, err := q.GetPacketsForStreamInSampleRange(
		context.Background(),
		db.GetPacketsForStreamInSampleRangeParams{
			Stream:      streamID,
			SampleIdx:   startSample,
			SampleIdx_2: endSample,
		},
	)
	if err != nil {
		log.Error("Failed to fetch packets", "error", err)
		return nil, fmt.Errorf("fetch packets: %w", err)
	}
	log.Debug("Fetched packets", "count", len(packets))

	var oggBuffer bytes.Buffer

	oggWriter, err := oggwriter.NewWith(&oggBuffer, 48000, 2)
	if err != nil {
		log.Error("Failed to create OGG writer", "error", err)
		return nil, fmt.Errorf("create OGG writer: %w", err)
	}
	log.Debug("Created OGG writer")

	var lastSampleIdx int64
	for i, packet := range packets {
		if lastSampleIdx != 0 {
			gap := packet.SampleIdx - lastSampleIdx
			if gap > 960 { // 960 samples = 20ms at 48kHz
				silentPacketsCount := gap / 960
				log.Debug("Inserting silent packets", "count", silentPacketsCount, "gap", gap)
				for j := int64(0); j < silentPacketsCount; j++ {
					silentPacket := []byte{0xf8, 0xff, 0xfe}
					if err := oggWriter.WriteRTP(&rtp.Packet{
						Header: rtp.Header{
							Timestamp: uint32(lastSampleIdx + (j * 960)),
						},
						Payload: silentPacket,
					}); err != nil {
						log.Error("Failed to write silent Opus packet", "error", err)
						return nil, fmt.Errorf(
							"write silent Opus packet: %w",
							err,
						)
					}
				}
			}
		}

		if err := oggWriter.WriteRTP(&rtp.Packet{
			Header: rtp.Header{
				Timestamp: uint32(packet.SampleIdx),
			},
			Payload: packet.Payload,
		}); err != nil {
			log.Error("Failed to write Opus packet", "error", err, "packetIndex", i)
			return nil, fmt.Errorf("write Opus packet: %w", err)
		}

		lastSampleIdx = packet.SampleIdx
		if i%100 == 0 {
			log.Debug("Writing packets progress", "packetIndex", i, "totalPackets", len(packets))
		}
	}

	if err := oggWriter.Close(); err != nil {
		log.Error("Failed to close OGG writer", "error", err)
		return nil, fmt.Errorf("close OGG writer: %w", err)
	}
	log.Debug("Closed OGG writer")

	log.Debug("GenerateOggOpusBlob completed", "outputSize", oggBuffer.Len())
	return oggBuffer.Bytes(), nil
}
