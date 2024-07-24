package llm

import (
	"context"
	"fmt"
	"io"
	"jamie/db"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

func SummarizeTranscript(
	apiKey string,
	duration time.Duration,
	promptName string,
) (<-chan string, error) {
	// Get transcriptions for the specified duration
	transcriptions, err := db.GetDB().GetTranscriptionsForDuration(duration)
	if err != nil {
		return nil, fmt.Errorf("get transcriptions for duration: %w", err)
	}

	if len(transcriptions) == 0 {
		return nil, fmt.Errorf(
			"no transcriptions found for the last %s",
			duration,
		)
	}

	// Format transcriptions
	var formattedTranscript strings.Builder
	for _, t := range transcriptions {
		formattedTranscript.WriteString(
			fmt.Sprintf(
				"%s %s: %s\n",
				t.Timestamp.Format("15:04:05"),
				t.Emoji,
				t.Text,
			),
		)
	}

	// Create OpenAI client
	client := openai.NewClient(apiKey)
	ctx := context.Background()

	// Get the system prompt
	var systemPrompt string
	if promptName != "" {
		systemPrompt, err = db.GetDB().GetSystemPrompt(promptName)
		if err != nil {
			return nil, fmt.Errorf("get system prompt: %w", err)
		}
	} else {
		systemPrompt = "Analyze the following transcript and provide a narrative synopsis. " +
			"Write punchy single sentence paragraphs, each one prefixed by a relevant emoji, different ones. " +
			"Emphasize key words and salient concepts with CAPS."
	}

	req := openai.ChatCompletionRequest{
		Model: openai.GPT3Dot5Turbo,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: systemPrompt,
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: formattedTranscript.String(),
			},
		},
		MaxTokens: 500,
		Stream:    true,
	}

	stream, err := client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf(
			"error creating chat completion stream: %w",
			err,
		)
	}

	summaryChannel := make(chan string, 50)

	go func() {
		defer close(summaryChannel)
		defer stream.Close()

		for {
			response, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					return
				}
				summaryChannel <- fmt.Sprintf("Stream error: %v", err)
				return
			}

			if len(response.Choices) > 0 &&
				response.Choices[0].Delta.Content != "" {
				summaryChannel <- response.Choices[0].Delta.Content
			}
		}
	}()

	return summaryChannel, nil
}
