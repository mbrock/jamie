package speech

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	api "github.com/deepgram/deepgram-go-sdk/pkg/api/listen/v1/websocket/interfaces"
	interfaces "github.com/deepgram/deepgram-go-sdk/pkg/client/interfaces"
	"github.com/deepgram/deepgram-go-sdk/pkg/client/listen"
)

var logger *log.Logger

func SetLogger(l *log.Logger) {
	logger = l
}

type DeepgramClient struct {
	token string
}

func NewDeepgramClient(token string) (*DeepgramClient, error) {
	return &DeepgramClient{token: token}, nil
}

func (c *DeepgramClient) Start(ctx context.Context) (LiveTranscriptionSession, error) {
	cOptions := &interfaces.ClientOptions{
		EnableKeepAlive: true,
	}
	tOptions := &interfaces.LiveTranscriptionOptions{
		Model:          "nova-2",
		Language:       "en-US",
		Punctuate:      true,
		Encoding:       "opus",
		Channels:       2,
		SampleRate:     48000,
		SmartFormat:    true,
		InterimResults: true,
		UtteranceEndMs: "1000",
		VadEvents:      true,
		Diarize:        true,
	}

	session := &DeepgramSession{
		transcriptions: make(chan chan string),
		sb:             &strings.Builder{},
	}

	client, err := listen.NewWebSocket(ctx, c.token, cOptions, tOptions, session)
	if err != nil {
		return nil, fmt.Errorf("error creating LiveTranscription connection: %w", err)
	}

	session.client = client

	connected := session.client.Connect()
	if !connected {
		return nil, fmt.Errorf("failed to connect to Deepgram")
	}

	return session, nil
}

type DeepgramSession struct {
	client         *listen.WebSocketClient
	sb             *strings.Builder
	transcriptions chan chan string
	currentTrans   chan string
}

func (s *DeepgramSession) Stop() error {
	s.client.Stop()
	return nil
}

func (s *DeepgramSession) Close(ocr *api.CloseResponse) error {
	logger.Info("Deepgram connection closed", "reason", ocr.Type)
	return nil
}

func (s *DeepgramSession) SendAudio(data []byte) error {
	return s.client.WriteBinary(data)
}

func (s *DeepgramSession) Transcriptions() <-chan chan string {
	return s.transcriptions
}

func (s *DeepgramSession) Message(mr *api.MessageResponse) error {
	sentence := strings.TrimSpace(mr.Channel.Alternatives[0].Transcript)

	if len(mr.Channel.Alternatives) == 0 || len(sentence) == 0 {
		return nil
	}

	s.sb.WriteString(sentence)
	s.sb.WriteString(" ")

	if mr.IsFinal {
		if s.currentTrans == nil {
			s.currentTrans = make(chan string)
			s.transcriptions <- s.currentTrans
		}
		s.currentTrans <- s.sb.String()

		if mr.SpeechFinal {
			transcript := strings.TrimSpace(s.sb.String())
			logger.Info("Transcript", "text", transcript)

			// Send the final transcript and close the current channel
			if s.currentTrans != nil {
				s.currentTrans <- transcript
				close(s.currentTrans)
			}

			// Create a new channel for the next transcription
			s.currentTrans = make(chan string)
			s.transcriptions <- s.currentTrans

			s.sb.Reset()
		}
	}

	return nil
}

func (s *DeepgramSession) Open(ocr *api.OpenResponse) error {
	logger.Info("Deepgram connection opened")
	return nil
}

func (s *DeepgramSession) Metadata(md *api.MetadataResponse) error {
	logger.Info("Received metadata", "metadata", md)
	return nil
}

func (s *DeepgramSession) SpeechStarted(ssr *api.SpeechStartedResponse) error {
	logger.Info("Speech started", "timestamp", ssr.Timestamp)
	return nil
}

func (s *DeepgramSession) UtteranceEnd(ur *api.UtteranceEndResponse) error {
	logger.Info("Utterance ended", "timestamp", ur.LastWordEnd)
	return nil
}

func (s *DeepgramSession) Error(er *api.ErrorResponse) error {
	logger.Error("Deepgram error", "type", er.Type, "description", er.Description)
	return nil
}

func (s *DeepgramSession) UnhandledEvent(byData []byte) error {
	logger.Warn("Unhandled Deepgram event", "data", string(byData))
	return nil
}
