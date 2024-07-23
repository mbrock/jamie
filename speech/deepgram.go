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
	client         *listen.WebSocketClient
	sb             *strings.Builder
	transcriptions chan chan string
	logger         *log.Logger
}

func (s *DeepgramSession) Stop() error {
	s.client.Stop()
	return nil
}

func (s *DeepgramSession) Close(ocr *api.CloseResponse) error {
	s.logger.Info("Deepgram connection closed", "reason", ocr.Type)
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
		transcript := strings.TrimSpace(s.sb.String())
		s.logger.Info("Transcript", "text", transcript)

		// Create a new channel for this transcription
		transcriptChan := make(chan string)
		s.transcriptions <- transcriptChan

		// Send the transcript and close the channel
		transcriptChan <- transcript
		close(transcriptChan)

		if mr.SpeechFinal {
			s.sb.Reset()
		}
	}

	return nil
}

func (s *DeepgramSession) Open(ocr *api.OpenResponse) error {
	s.logger.Info("Deepgram connection opened")
	return nil
}

func (s *DeepgramSession) Metadata(md *api.MetadataResponse) error {
	s.logger.Info("Received metadata", "metadata", md)
	return nil
}

func (s *DeepgramSession) SpeechStarted(ssr *api.SpeechStartedResponse) error {
	s.logger.Info("Speech started", "timestamp", ssr.Timestamp)
	return nil
}

func (s *DeepgramSession) UtteranceEnd(ur *api.UtteranceEndResponse) error {
	s.logger.Info("Utterance ended", "timestamp", ur.LastWordEnd)
	return nil
}

func (s *DeepgramSession) Error(er *api.ErrorResponse) error {
	s.logger.Error("Deepgram error", "type", er.Type, "description", er.Description)
	return nil
}

func (s *DeepgramSession) UnhandledEvent(byData []byte) error {
	s.logger.Warn("Unhandled Deepgram event", "data", string(byData))
	return nil
}
