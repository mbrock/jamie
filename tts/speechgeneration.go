package tts

import (
	"context"
	"fmt"
	"io"

	"github.com/haguro/elevenlabs-go"
)

type SpeechGenerator interface {
	TextToSpeechStreaming(ctx context.Context, text string, writer io.Writer) error
}

type ElevenLabsSpeechGenerator struct {
	apiKey string
}

func NewElevenLabsSpeechGenerator(apiKey string) *ElevenLabsSpeechGenerator {
	return &ElevenLabsSpeechGenerator{apiKey: apiKey}
}

func (e *ElevenLabsSpeechGenerator) TextToSpeechStreaming(
	ctx context.Context,
	text string,
	writer io.Writer,
) error {
	elevenlabs.SetAPIKey(e.apiKey)

	ttsReq := elevenlabs.TextToSpeechRequest{
		Text:    text,
		ModelID: "eleven_turbo_v2_5",
	}

	errChan := make(chan error, 1)
	go func() {
		err := elevenlabs.TextToSpeechStream(
			writer,
			"pKLLpypGseGMUjkb5fEZ",
			ttsReq,
		)
		errChan <- err
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("failed to generate speech: %w", err)
		}

		return nil
	}
}
