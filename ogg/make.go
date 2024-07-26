package ogg

import (
	"context"
	"fmt"
	"os"
	"os/exec"

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
	// Create a temporary file for the input MP3
	inputFile, err := os.CreateTemp("", "input*.mp3")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary input file: %w", err)
	}
	defer os.Remove(inputFile.Name())
	defer inputFile.Close()

	// Write MP3 data to the temporary file
	if _, err := inputFile.Write(mp3Data); err != nil {
		return nil, fmt.Errorf("failed to write MP3 data to temporary file: %w", err)
	}

	// Create a temporary file for the output PCM
	outputFile, err := os.CreateTemp("", "output*.pcm")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary output file: %w", err)
	}
	defer os.Remove(outputFile.Name())
	defer outputFile.Close()

	// Use ffmpeg to convert MP3 to 48kHz stereo PCM
	cmd := exec.Command("ffmpeg", "-i", inputFile.Name(), "-ar", "48000", "-ac", "2", "-f", "s16le", outputFile.Name())
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to execute ffmpeg: %w", err)
	}

	// Read the converted PCM data
	pcmData, err := os.ReadFile(outputFile.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to read converted PCM data: %w", err)
	}

	// Create an Opus encoder
	encoder, err := gopus.NewEncoder(48000, 2, gopus.Audio)
	if err != nil {
		return nil, fmt.Errorf("failed to create Opus encoder: %w", err)
	}

	var opusPackets [][]byte
	frameSize := 960 * 2 * 2 // 960 samples * 2 channels * 2 bytes per sample
	for i := 0; i < len(pcmData); i += frameSize {
		end := i + frameSize
		if end > len(pcmData) {
			end = len(pcmData)
		}
		frame := pcmData[i:end]

		// Convert byte PCM to int16 PCM
		pcmInt16 := make([]int16, len(frame)/2)
		for j := 0; j < len(frame); j += 2 {
			pcmInt16[j/2] = int16(frame[j]) | int16(frame[j+1])<<8
		}

		// Encode the frame to Opus
		opusData, err := encoder.Encode(pcmInt16, 960, 32000)
		if err != nil {
			return nil, fmt.Errorf("failed to encode Opus: %w", err)
		}
		opusPackets = append(opusPackets, opusData)
	}

	return opusPackets, nil
}
