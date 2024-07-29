package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

func StreamMp3ToPCM(
	ctx context.Context,
	mp3Input <-chan []byte,
	bufferLength int,
) (<-chan []byte, error) {
	pcmOutput := make(chan []byte)

	ffmpegIn, ffmpegInWriter := io.Pipe()
	ffmpegOutReader, ffmpegOut := io.Pipe()

	ffmpegCmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", "pipe:0",
		"-f", "s16le",
		"-acodec", "pcm_s16le",
		"-ar", "48000",
		"-ac", "2",
		"-fflags", "nobuffer+flush_packets",
		"-flags", "low_delay",
		"-strict", "experimental",
		"-probesize", "32",
		"-analyzeduration", "0",
		"-")
	ffmpegCmd.Stdin = ffmpegIn
	ffmpegCmd.Stdout = ffmpegOut

	err := ffmpegCmd.Start()
	if err != nil {
		return nil, err
	}

	go func() {
		defer ffmpegInWriter.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case mp3Data, ok := <-mp3Input:
				if !ok {
					return
				}
				ffmpegInWriter.Write(mp3Data)
			}
		}
	}()

	go func() {
		defer close(pcmOutput)
		defer ffmpegOutReader.Close()
		buffer := make([]byte, bufferLength)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, err := io.ReadFull(ffmpegOutReader, buffer)
				if err == io.EOF && n == 0 {
					return
				}
				if err != nil && err != io.EOF && !errors.Is(err, io.ErrUnexpectedEOF) {
					return
				}
				select {
				case <-ctx.Done():
					return
				case pcmOutput <- buffer[:n]:
				}
			}
		}
	}()

	go func() {
		ffmpegCmd.Wait()
	}()

	return pcmOutput, nil
}

func StreamPCMToInt16(
	ctx context.Context,
	pcmInput <-chan []byte,
) <-chan []int16 {
	int16Output := make(chan []int16)

	go func() {
		defer close(int16Output)

		buffer := make([]int16, 0, 960*2)

		for {
			select {
			case <-ctx.Done():
				return
			case pcmData, ok := <-pcmInput:
				if !ok {
					return
				}
				// Convert byte buffer to int16 slice
				for i := 0; i < len(pcmData); i += 2 {
					sample := int16(pcmData[i]) | int16(pcmData[i+1])<<8
					buffer = append(buffer, sample)

					if len(buffer) == 960*2 {
						select {
						case <-ctx.Done():
							return
						case int16Output <- buffer:
							buffer = make([]int16, 0, 960*2)
						}
					}
				}
			}
		}
	}()

	return int16Output
}

func DecodeMpeg20msPCM(
	ctx context.Context,
	mp3Chan <-chan []byte,
) (<-chan []int16, error) {
	bufferLength := 960 * 2 * 2 // 960 samples * 2 bytes per sample * 2 channels
	pcmChan, err := StreamMp3ToPCM(ctx, mp3Chan, bufferLength)
	if err != nil {
		return nil, fmt.Errorf("failed to start audio conversion: %w", err)
	}

	return StreamPCMToInt16(ctx, pcmChan), nil
}