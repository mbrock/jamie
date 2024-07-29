package audio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"github.com/charmbracelet/log"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
	"jamie/db"
)

type OggOpusWriter struct {
	writer        *oggwriter.OggWriter
	lastSampleIdx int64
	log           *log.Logger
}

func NewOggOpusWriter(w io.Writer, log *log.Logger) (*OggOpusWriter, error) {
	oggWriter, err := oggwriter.NewWith(w, 48000, 2)
	if err != nil {
		return nil, fmt.Errorf("create OGG writer: %w", err)
	}
	return &OggOpusWriter{
		writer: oggWriter,
		log:    log,
	}, nil
}

func (w *OggOpusWriter) WritePacket(payload []byte, sampleIdx int64) error {
	if w.lastSampleIdx != 0 {
		gap := sampleIdx - w.lastSampleIdx
		if gap > 960 {
			if err := w.insertSilence(gap); err != nil {
				return err
			}
		}
	}

	if err := w.writer.WriteRTP(&rtp.Packet{
		Header: rtp.Header{
			Timestamp: uint32(sampleIdx),
		},
		Payload: payload,
	}); err != nil {
		return fmt.Errorf("write Opus packet: %w", err)
	}

	w.lastSampleIdx = sampleIdx
	return nil
}

func (w *OggOpusWriter) insertSilence(gap int64) error {
	silentPacketsCount := gap / 960
	w.log.Debug("Inserting silent packets", "count", silentPacketsCount, "gap", gap)
	for j := int64(0); j < silentPacketsCount; j++ {
		silentPacket := []byte{0xf8, 0xff, 0xfe}
		if err := w.writer.WriteRTP(&rtp.Packet{
			Header: rtp.Header{
				Timestamp: uint32(w.lastSampleIdx + (j * 960)),
			},
			Payload: silentPacket,
		}); err != nil {
			return fmt.Errorf("write silent Opus packet: %w", err)
		}
	}
	return nil
}

func (w *OggOpusWriter) Close() error {
	return w.writer.Close()
}

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
	writer, err := NewOggOpusWriter(&oggBuffer, log)
	if err != nil {
		log.Error("Failed to create OGG writer", "error", err)
		return nil, err
	}

	for i, packet := range packets {
		if err := writer.WritePacket(packet.Payload, packet.SampleIdx); err != nil {
			log.Error("Failed to write Opus packet", "error", err, "packetIndex", i)
			return nil, err
		}

		if i%100 == 0 {
			log.Debug("Writing packets progress", "packetIndex", i, "totalPackets", len(packets))
		}
	}

	if err := writer.Close(); err != nil {
		log.Error("Failed to close OGG writer", "error", err)
		return nil, fmt.Errorf("close OGG writer: %w", err)
	}
	log.Debug("Closed OGG writer")

	log.Debug("GenerateOggOpusBlob completed", "outputSize", oggBuffer.Len())
	return oggBuffer.Bytes(), nil
}
