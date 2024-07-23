package speech

import (
	"context"
)

type LiveTranscriptionSession interface {
	Stop() error
	SendAudio(data []byte) error
	Transcriptions() <-chan string
}

type LiveTranscriptionService interface {
	Start(ctx context.Context) (LiveTranscriptionSession, error)
}
