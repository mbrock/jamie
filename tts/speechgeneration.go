package tts

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/haguro/elevenlabs-go"
)

type SpeechGenerator interface {
	TextToSpeechStreaming(ctx context.Context, text string, writer io.Writer) error
}

type ElevenLabsSpeechGenerator struct {
	client *elevenlabs.Client
}

func NewElevenLabsSpeechGenerator(apiKey string) *ElevenLabsSpeechGenerator {
	client := elevenlabs.NewClient(context.Background(), apiKey, 30*time.Second)
	return &ElevenLabsSpeechGenerator{client: client}
}

func (e *ElevenLabsSpeechGenerator) TextToSpeechStreaming(
	ctx context.Context,
	text string,
	writer io.Writer,
) error {
	ttsReq := elevenlabs.TextToSpeechRequest{
		Text:    text,
		ModelID: "eleven_turbo_v2_5",
	}

	errChan := make(chan error, 1)
	go func() {
		err := e.client.TextToSpeechStream(
			ctx,
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
