package discordbot

import (
	"io"
	"os/exec"
)

func streamMp3ToPCM(mp3Input <-chan []byte) (<-chan []byte, error) {
	pcmOutput := make(chan []byte)

	ffmpegIn, ffmpegInWriter := io.Pipe()
	ffmpegOutReader, ffmpegOut := io.Pipe()

	ffmpegCmd := exec.Command("ffmpeg",
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
		for mp3Data := range mp3Input {
			ffmpegInWriter.Write(mp3Data)
		}
	}()

	go func() {
		defer close(pcmOutput)
		defer ffmpegOutReader.Close()
		buffer := make([]byte, 960*2*2)
		for {
			n, err := io.ReadFull(ffmpegOutReader, buffer)
			if err == io.EOF && n == 0 {
				break
			}
			if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
				return
			}
			pcmOutput <- buffer[:n]
		}
	}()

	go func() {
		ffmpegCmd.Wait()
	}()

	return pcmOutput, nil
}

func streamPCMToOpus(pcmInput <-chan []byte, encoder *gopus.Encoder) <-chan []byte {
	opusOutput := make(chan []byte)

	go func() {
		defer close(opusOutput)

		for pcmData := range pcmInput {
			// Convert byte buffer to int16 slice
			pcmBuffer := make([]int16, len(pcmData)/2)
			for i := 0; i < len(pcmData); i += 2 {
				pcmBuffer[i/2] = int16(pcmData[i]) | int16(pcmData[i+1])<<8
			}

			// If we got a partial read, pad with silence
			if len(pcmBuffer) < 960*2 {
				pcmBuffer = append(pcmBuffer, make([]int16, 960*2-len(pcmBuffer))...)
			}

			// Encode the frame to Opus
			opusData, err := encoder.Encode(pcmBuffer, 960, 128000)
			if err != nil {
				// Handle error (you might want to log this)
				continue
			}

			opusOutput <- opusData
		}
	}()

	return opusOutput
}
