package stt

import (
	"context"
)

type Result struct {
	Text      string
	Start     float64
	Duration  float64
}

type LiveTranscriptionSession interface {
	Stop() error
	SendAudio(data []byte) error
	Receive() <-chan chan Result
}

type SpeechRecognitionService interface {
	Start(ctx context.Context) (LiveTranscriptionSession, error)
}
