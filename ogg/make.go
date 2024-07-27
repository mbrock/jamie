package ogg

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"

	"layeh.com/gopus"

	"jamie/db"

	"github.com/charmbracelet/log"
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
		streamID,
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
	inputFile, err := os.CreateTemp("", "input*.mp3")
	if err != nil {
		log.Error("Failed to create temporary input file", "error", err)
		return nil, fmt.Errorf(
			"failed to create temporary input file: %w",
			err,
		)
	}
	defer os.Remove(inputFile.Name())
	defer inputFile.Close()

	if _, err := inputFile.Write(mp3Data); err != nil {
		log.Error("Failed to write MP3 data to temporary file", "error", err)
		return nil, fmt.Errorf(
			"failed to write MP3 data to temporary file: %w",
			err,
		)
	}

	outputFile, err := os.CreateTemp("", "output*.pcm")
	if err != nil {
		log.Error("Failed to create temporary output file", "error", err)
		return nil, fmt.Errorf(
			"failed to create temporary output file: %w",
			err,
		)
	}
	defer os.Remove(outputFile.Name())
	defer outputFile.Close()

	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-i", inputFile.Name(),
		"-ar", "48000",
		"-ac", "2",
		"-f", "s16le",
		outputFile.Name(),
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		log.Error(
			"Failed to execute ffmpeg",
			"error", err,
			"stderr", stderr.String(),
		)
		return nil, fmt.Errorf(
			"failed to execute ffmpeg: %w\nStderr: %s",
			err,
			stderr.String(),
		)
	}

	log.Info("ffmpeg", "status", "ok")

	pcmData, err := os.ReadFile(outputFile.Name())
	if err != nil {
		log.Error("Failed to read converted PCM data", "error", err)
		return nil, fmt.Errorf("failed to read converted PCM data: %w", err)
	}

	encoder, err := gopus.NewEncoder(48000, 2, gopus.Audio)
	if err != nil {
		log.Error("Failed to create Opus encoder", "error", err)
		return nil, fmt.Errorf("failed to create Opus encoder: %w", err)
	}

	log.Info("Created Opus encoder")

	var opusPackets [][]byte
	frameSize := 960 * 2 * 2 // 960 samples * 2 channels * 2 bytes per sample
	for i := 0; i < len(pcmData); i += frameSize {
		end := i + frameSize
		if end > len(pcmData) {
			end = len(pcmData)
		}
		frame := pcmData[i:end]

		if len(frame) < frameSize {
			frame = append(frame, make([]byte, frameSize-len(frame))...)
		}

		pcmInt16 := make([]int16, 960*2)
		for j := 0; j < len(frame); j += 2 {
			pcmInt16[j/2] = int16(frame[j]) | int16(frame[j+1])<<8
		}

		opusData, err := encoder.Encode(pcmInt16, 960, 32000)
		if err != nil {
			log.Error("Failed to encode Opus", "error", err)
			return nil, fmt.Errorf("failed to encode Opus: %w", err)
		}
		opusPackets = append(opusPackets, opusData)
	}

	return opusPackets, nil
}
