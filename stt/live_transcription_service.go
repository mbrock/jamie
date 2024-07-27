package stt

import (
	"context"
)

type Result struct {
	Text       string
	Start      float64
	Duration   float64
	Confidence float64
}

type SpeechRecognizer interface {
	Stop() error
	SendAudio(data []byte) error
	Receive() <-chan chan Result
}

type SpeechRecognition interface {
	Start(ctx context.Context, language string) (SpeechRecognizer, error)
}
