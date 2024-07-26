package ogg

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/tosone/minimp3"
	"layeh.com/gopus"

	"jamie/db"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

func GenerateOggOpusBlob(
	db *db.Queries,
	streamID string,
	startSample, endSample int64,
) ([]byte, error) {
	packets, err := db.GetPacketsForStreamInSampleRange(
		context.Background(),
		streamID, // XXX: weird
	)
	if err != nil {
		return nil, fmt.Errorf("fetch packets: %w", err)
	}

	var oggBuffer bytes.Buffer

	oggWriter, err := oggwriter.NewWith(&oggBuffer, 48000, 2)
	if err != nil {
		return nil, fmt.Errorf("create OGG writer: %w", err)
	}

	var lastSampleIdx int64
	for _, packet := range packets {
		if lastSampleIdx != 0 {
			gap := packet.SampleIdx - lastSampleIdx
			if gap > 960 { // 960 samples = 20ms at 48kHz
				silentPacketsCount := gap / 960
				for j := int64(0); j < silentPacketsCount; j++ {
					silentPacket := []byte{0xf8, 0xff, 0xfe}
					if err := oggWriter.WriteRTP(&rtp.Packet{
						Header: rtp.Header{
							Timestamp: uint32(lastSampleIdx + (j * 960)),
						},
						Payload: silentPacket,
					}); err != nil {
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
			return nil, fmt.Errorf("write Opus packet: %w", err)
		}

		lastSampleIdx = packet.SampleIdx
	}

	if err := oggWriter.Close(); err != nil {
		return nil, fmt.Errorf("close OGG writer: %w", err)
	}

	return oggBuffer.Bytes(), nil
}

func ConvertToOpus(mp3Data []byte) ([][]byte, error) {
	// Create an Opus encoder with 48kHz sample rate, 2 channels (stereo), and optimized for audio
	encoder, err := gopus.NewEncoder(48000, 2, gopus.Audio)
	if err != nil {
		return nil, fmt.Errorf("failed to create Opus encoder: %w", err)
	}

	// Create an MP3 decoder from the input data
	decoder, err := minimp3.NewDecoder(bytes.NewReader(mp3Data))
	if err != nil {
		return nil, fmt.Errorf("failed to create MP3 decoder: %w", err)
	}

	var opusPackets [][]byte
	// Buffer to hold exactly 960 mono samples (1920 bytes for 16-bit PCM)
	pcmBuffer := make([]byte, 960*2)
	for {
		// Read 960 stereo samples (1920 bytes) from the MP3 decoder
		_, err := io.ReadFull(decoder, pcmBuffer)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break // End of MP3 data
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read from MP3 decoder: %w", err)
		}

		// Convert byte PCM to int16 PCM and duplicate for stereo
		pcmInt16 := make([]int16, 960)
		for i := 0; i < 960; i += 1 {
			sample := int16(pcmBuffer[i*2]) | int16(pcmBuffer[i*2+1])<<8
			pcmInt16[i] = sample
		}

		// Encode the 960 stereo samples to Opus
		opusData, err := encoder.Encode(pcmInt16, 960, 32000)
		if err != nil {
			return nil, fmt.Errorf("failed to encode Opus: %w", err)
		}
		opusPackets = append(opusPackets, opusData)
	}

	return opusPackets, nil
}
