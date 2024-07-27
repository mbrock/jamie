package tts

import (
	"fmt"

	"github.com/haguro/elevenlabs-go"
)

type SpeechGenerator interface {
	TextToSpeech(text string) ([]byte, error)
}

type ElevenLabsSpeechGenerator struct {
	apiKey string
}

func NewElevenLabsSpeechGenerator(apiKey string) *ElevenLabsSpeechGenerator {
	return &ElevenLabsSpeechGenerator{apiKey: apiKey}
}

func (e *ElevenLabsSpeechGenerator) TextToSpeech(text string) ([]byte, error) {
	elevenlabs.SetAPIKey(e.apiKey)

	ttsReq := elevenlabs.TextToSpeechRequest{
		Text:    text,
		ModelID: "eleven_turbo_v2_5",
	}

	audio, err := elevenlabs.TextToSpeech("XB0fDUnXU5powFXDhCwa", ttsReq)
	if err != nil {
		return nil, fmt.Errorf("failed to generate speech: %w", err)
	}

	return audio, nil
}
