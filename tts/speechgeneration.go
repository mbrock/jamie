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
	client := elevenlabs.NewClient(ctx, e.apiKey, 30*time.Second)
	ttsReq := elevenlabs.TextToSpeechRequest{
		Text:    text,
		ModelID: "eleven_turbo_v2_5",
	}

	err := client.TextToSpeechStream(
		writer,
		"pKLLpypGseGMUjkb5fEZ",
		ttsReq,
	)
	if err != nil {
		return fmt.Errorf("failed to generate speech: %w", err)
	}
	return nil
}
