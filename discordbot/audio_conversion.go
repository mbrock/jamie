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
