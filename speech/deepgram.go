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
		transcriptions: make(chan string),
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
	sb             strings.Builder
	transcriptions chan string
}

func (s *DeepgramSession) Stop() error {
	s.client.Stop()
	return nil
}

func (s *DeepgramSession) SendAudio(data []byte) error {
	return s.client.WriteBinary(data)
}

func (s *DeepgramSession) Transcriptions() <-chan string {
	return s.transcriptions
}

func (c *DeepgramClient) Message(mr *api.MessageResponse) error {
	sentence := strings.TrimSpace(mr.Channel.Alternatives[0].Transcript)

	if len(mr.Channel.Alternatives) == 0 || len(sentence) == 0 {
		return nil
	}

	if mr.IsFinal {
		c.sb.WriteString(sentence)
		c.sb.WriteString(" ")

		if mr.SpeechFinal {
			transcript := strings.TrimSpace(c.sb.String())
			logger.Info("Transcript", "text", transcript)

			// Send the transcript through the channel
			c.transcriptions <- transcript

			c.sb.Reset()
		}
	}

	return nil
}

func (c *DeepgramClient) Open(ocr *api.OpenResponse) error {
	logger.Info("Deepgram connection opened")
	return nil
}

func (c *DeepgramClient) Metadata(md *api.MetadataResponse) error {
	logger.Info("Received metadata", "metadata", md)
	return nil
}

func (c *DeepgramClient) SpeechStarted(ssr *api.SpeechStartedResponse) error {
	logger.Info("Speech started", "timestamp", ssr.Timestamp)
	return nil
}

func (c *DeepgramClient) UtteranceEnd(ur *api.UtteranceEndResponse) error {
	logger.Info("Utterance ended", "timestamp", ur.LastWordEnd)
	return nil
}

func (c *DeepgramClient) Close(ocr *api.CloseResponse) error {
	logger.Info("Deepgram connection closed", "reason", ocr.Type)
	return nil
}

func (c *DeepgramClient) Error(er *api.ErrorResponse) error {
	logger.Error("Deepgram error", "type", er.Type, "description", er.Description)
	return nil
}

func (c *DeepgramClient) UnhandledEvent(byData []byte) error {
	logger.Warn("Unhandled Deepgram event", "data", string(byData))
	return nil
}
