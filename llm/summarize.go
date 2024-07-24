package llm

import (
	"context"
	"fmt"
	"jamie/db"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

func SummarizeTranscript(
	openaiAPIKey string,
	duration time.Duration,
	promptName string,
) (string, error) {
	// Get transcriptions for the specified duration
	transcriptions, err := db.GetDB().GetTranscriptionsForDuration(duration)
	if err != nil {
		return "", fmt.Errorf("get transcriptions for duration: %w", err)
	}

	if len(transcriptions) == 0 {
		return fmt.Sprintf(
			"No transcriptions found for the last %s",
			duration,
		), nil
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
	client := openai.NewClient(openaiAPIKey)
	ctx := context.Background()

	// Get the system prompt
	var systemPrompt string
	if promptName != "" {
		systemPrompt, err = db.GetDB().GetSystemPrompt(promptName)
		if err != nil {
			return "", fmt.Errorf("get system prompt: %w", err)
		}
	} else {
		systemPrompt = "Analyze the following transcript and provide a narrative synopsis. " +
			"Write punchy single sentence paragraphs, each one prefixed by a relevant emoji, different ones. " +
			"Emphasize key words and salient concepts with CAPS."
	}

	// Prepare the chat completion request
	req := openai.ChatCompletionRequest{
		Model:     openai.GPT4o,
		MaxTokens: 500,
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
	}

	// Send the request to OpenAI
	resp, err := client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("OpenAI API error: %w", err)
	}

	summary := resp.Choices[0].Message.Content
	return summary, nil
}
