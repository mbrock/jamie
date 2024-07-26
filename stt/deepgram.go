package stt

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	api "github.com/deepgram/deepgram-go-sdk/pkg/api/listen/v1/websocket/interfaces"
	"github.com/deepgram/deepgram-go-sdk/pkg/client/interfaces"
	"github.com/deepgram/deepgram-go-sdk/pkg/client/listen"
)

type DeepgramClient struct {
	token  string
	logger *log.Logger
}

func NewDeepgramClient(
	token string,
	logger *log.Logger,
) (*DeepgramClient, error) {
	return &DeepgramClient{
		token:  token,
		logger: logger,
	}, nil
}

func (c *DeepgramClient) Start(
	ctx context.Context,
) (LiveTranscriptionSession, error) {
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
		transcriptions: make(chan chan Result),
		logger:         c.logger,
		audioBuffer: make(
			chan []byte,
			100,
		), // Adjust buffer size as needed
		isOpen: false,
	}

	client, err := listen.NewWebSocket(
		ctx,
		c.token,
		cOptions,
		tOptions,
		session,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"error creating LiveTranscription connection: %w",
			err,
		)
	}

	session.client = client

	go session.client.Connect()

	return session, nil
}

type DeepgramSession struct {
	client              *listen.WebSocketClient
	transcriptions      chan chan Result
	logger              *log.Logger
	currentTranscriptCh chan Result
	audioBuffer         chan []byte
	isOpen              bool
}

func (s *DeepgramSession) Stop() error {
	close(s.audioBuffer)
	s.client.Stop()
	return nil
}

func (s *DeepgramSession) Close(ocr *api.CloseResponse) error {
	s.logger.Info("closed", "reason", ocr.Type)
	close(s.audioBuffer)
	return nil
}

func (s *DeepgramSession) SendAudio(data []byte) error {
	select {
	case s.audioBuffer <- data:
		return nil
	default:
		return fmt.Errorf("audio buffer full")
	}
}

func (s *DeepgramSession) Receive() <-chan chan Result {
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

	result := Result{
		Text:       transcript,
		Start:      mr.Start,
		Duration:   mr.Duration,
		Confidence: mr.Channel.Alternatives[0].Confidence,
	}

	s.logger.Info(
		"hear",
		"txt",
		transcript,
		"start",
		mr.Start,
		"duration",
		mr.Duration,
	)

	if s.currentTranscriptCh == nil {
		s.logger.Info("next")
		s.currentTranscriptCh = make(chan Result)
		s.transcriptions <- s.currentTranscriptCh
	}

	s.currentTranscriptCh <- result

	if mr.IsFinal {
		close(s.currentTranscriptCh)
		s.currentTranscriptCh = nil
	} else {
		s.logger.Info("hear", "tmp", transcript)
	}

	return nil
}

func (s *DeepgramSession) Open(ocr *api.OpenResponse) error {
	s.logger.Info("open", "kind", "deepgram")
	s.isOpen = true
	go func() {
		for data := range s.audioBuffer {
			if err := s.client.WriteBinary(data); err != nil {
				s.logger.Error("failed to write audio data", "error", err)
			}
		}
	}()
	return nil
}

func (s *DeepgramSession) Metadata(md *api.MetadataResponse) error {
	s.logger.Info("metadata", "metadata", md)
	return nil
}

func (s *DeepgramSession) SpeechStarted(
	ssr *api.SpeechStartedResponse,
) error {
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
