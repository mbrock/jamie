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

type DeepgramClient struct {
	token  string
	logger *log.Logger
}

func NewDeepgramClient(token string, logger *log.Logger) (*DeepgramClient, error) {
	return &DeepgramClient{token: token, logger: logger}, nil
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
		logger:         c.logger,
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
	client              *listen.WebSocketClient
	transcriptions      chan chan string
	logger              *log.Logger
	currentTranscriptCh chan string
}

func (s *DeepgramSession) Stop() error {
	s.client.Stop()
	return nil
}

func (s *DeepgramSession) Close(ocr *api.CloseResponse) error {
	s.logger.Info("closed", "reason", ocr.Type)
	return nil
}

func (s *DeepgramSession) SendAudio(data []byte) error {
	return s.client.WriteBinary(data)
}

func (s *DeepgramSession) Transcriptions() <-chan chan string {
	return s.transcriptions
}

func (s *DeepgramSession) Message(mr *api.MessageResponse) error {
	if len(mr.Channel.Alternatives) == 0 {
		return nil
	}

	transcript := strings.TrimSpace(mr.Channel.Alternatives[0].Transcript)

	if len(transcript) == 0 {
		return nil
	}

	if mr.IsFinal {
		s.logger.Info("hear", "txt", transcript)

		// Close the current transcript channel if it exists
		if s.currentTranscriptCh != nil {
			close(s.currentTranscriptCh)
		}

		// Create a new channel for this transcription
		s.currentTranscriptCh = make(chan string)
		s.transcriptions <- s.currentTranscriptCh

		// Send the transcript
		s.currentTranscriptCh <- transcript

		// Reset the current transcript channel
		s.currentTranscriptCh = nil
	} else {
		s.logger.Info("hear", "tmp", transcript)

		// If there's no current transcript channel, create one
		if s.currentTranscriptCh == nil {
			s.currentTranscriptCh = make(chan string)
			s.transcriptions <- s.currentTranscriptCh
		}

		// Send the interim transcript
		s.currentTranscriptCh <- transcript
	}

	return nil
}

func (s *DeepgramSession) Open(ocr *api.OpenResponse) error {
	s.logger.Info("open", "kind", "deepgram")
	return nil
}

func (s *DeepgramSession) Metadata(md *api.MetadataResponse) error {
	s.logger.Info("metadata", "metadata", md)
	return nil
}

func (s *DeepgramSession) SpeechStarted(ssr *api.SpeechStartedResponse) error {
	s.logger.Debug("speech start", "timestamp", ssr.Timestamp)
	return nil
}

func (s *DeepgramSession) UtteranceEnd(ur *api.UtteranceEndResponse) error {
	s.logger.Debug("utterance end", "timestamp", ur.LastWordEnd)
	return nil
}

func (s *DeepgramSession) Error(er *api.ErrorResponse) error {
	s.logger.Error("error", "type", er.Type, "description", er.Description)
	return nil
}

func (s *DeepgramSession) UnhandledEvent(byData []byte) error {
	s.logger.Warn("unhandled event", "data", string(byData))
	return nil
}
