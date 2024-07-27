package tts

import (
	"fmt"
	"io"

	"github.com/haguro/elevenlabs-go"
)

type SpeechGenerator interface {
	TextToSpeechStreaming(text string, writer io.Writer) error
}

type ElevenLabsSpeechGenerator struct {
	apiKey string
}

func NewElevenLabsSpeechGenerator(apiKey string) *ElevenLabsSpeechGenerator {
	return &ElevenLabsSpeechGenerator{apiKey: apiKey}
}

func (e *ElevenLabsSpeechGenerator) TextToSpeechStreaming(
	text string,
	writer io.Writer,
) error {
	elevenlabs.SetAPIKey(e.apiKey)

	ttsReq := elevenlabs.TextToSpeechRequest{
		Text:    text,
		ModelID: "eleven_turbo_v2_5",
	}

	err := elevenlabs.TextToSpeechStream(
		writer,
		"NFG5qt843uXKj4pFvR7C",
		ttsReq,
	)
	if err != nil {
		return fmt.Errorf("failed to generate speech: %w", err)
	}

	return nil
}