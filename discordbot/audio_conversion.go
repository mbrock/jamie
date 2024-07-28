package discordbot

import (
	"context"
	"io"
	"os/exec"

	"layeh.com/gopus"
)

func streamMp3ToPCM(
	ctx context.Context,
	mp3Input <-chan []byte,
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
		buffer := make([]byte, 960*2*2)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, err := io.ReadFull(ffmpegOutReader, buffer)
				if err == io.EOF && n == 0 {
					return
				}
				if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
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

func streamPCMToOpus(
	ctx context.Context,
	pcmInput <-chan []byte,
	encoder *gopus.Encoder,
) <-chan []byte {
	opusOutput := make(chan []byte)

	go func() {
		defer close(opusOutput)

		for {
			select {
			case <-ctx.Done():
				return
			case pcmData, ok := <-pcmInput:
				if !ok {
					return
				}
				// Convert byte buffer to int16 slice
				pcmBuffer := make([]int16, len(pcmData)/2)
				for i := 0; i < len(pcmData); i += 2 {
					pcmBuffer[i/2] = int16(
						pcmData[i],
					) | int16(
						pcmData[i+1],
					)<<8
				}

				// If we got a partial read, pad with silence
				if len(pcmBuffer) < 960*2 {
					pcmBuffer = append(
						pcmBuffer,
						make([]int16, 960*2-len(pcmBuffer))...)
				}

				// Encode the frame to Opus
				opusData, err := encoder.Encode(pcmBuffer, 960, 128000)
				if err != nil {
					// Handle error (you might want to log this)
					continue
				}

				select {
				case <-ctx.Done():
					return
				case opusOutput <- opusData:
				}
			}
		}
	}()

	return opusOutput
}
