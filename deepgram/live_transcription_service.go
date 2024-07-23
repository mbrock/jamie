package deepgram

import (
	"context"
)

type LiveTranscriptionService interface {
	Start(ctx context.Context) error
	Stop() error
	SendAudio(data []byte) error
	Transcriptions() <-chan string
}
