package stt

import (
	"context"
)

type LiveTranscriptionSession interface {
	Stop() error
	SendAudio(data []byte) error
	Receive() <-chan chan string
}

type SpeechRecognitionService interface {
	Start(ctx context.Context) (LiveTranscriptionSession, error)
}