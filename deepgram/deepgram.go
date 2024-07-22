package deepgram

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	api "github.com/deepgram/deepgram-go-sdk/pkg/api/listen/v1/websocket/interfaces"
	interfaces "github.com/deepgram/deepgram-go-sdk/pkg/client/interfaces"
	client "github.com/deepgram/deepgram-go-sdk/pkg/client/listen"

	"jamie/db"
)

var logger *log.Logger

func SetLogger(l *log.Logger) {
	logger = l
}

type TranscriptionCallback func(guildID, channelID, transcript string)

type DeepgramClient struct {
	client   interfaces.LiveTranscriptionClient
	callback TranscriptionCallback
	sb       strings.Builder
	guildID  string
	channelID string
}

func NewDeepgramClient(token string, guildID, channelID string, callback TranscriptionCallback) (*DeepgramClient, error) {
	ctx := context.Background()
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

	dgClient := &DeepgramClient{
		callback:  callback,
		guildID:   guildID,
		channelID: channelID,
	}

	client, err := client.NewWebSocket(ctx, token, cOptions, tOptions, dgClient)
	if err != nil {
		return nil, fmt.Errorf("error creating LiveTranscription connection: %w", err)
	}

	dgClient.client = client

	return dgClient, nil
}

func (c *DeepgramClient) Connect() bool {
	return c.client.Connect()
}

func (c *DeepgramClient) WriteBinary(data []byte) error {
	return c.client.WriteBinary(data)
}

func (c *DeepgramClient) Stop() {
	c.client.Stop()
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

			// Store the transcript in the database
			err := db.SaveTranscript(c.guildID, c.channelID, transcript)
			if err != nil {
				logger.Error("Failed to save transcript to database", "error", err.Error())
			}

			// Call the callback function with the transcript
			c.callback(c.guildID, c.channelID, transcript)

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
