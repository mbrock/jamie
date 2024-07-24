package llm

import (
	"context"
	"fmt"
	"jamie/db"
	"strings"

	"github.com/sashabaranov/go-openai"
)

func SummarizeTranscript(openaiAPIKey string) (string, error) {
	// Get today's transcriptions
	transcriptions, err := db.GetDB().GetTodayTranscriptions()
	if err != nil {
		return "", fmt.Errorf("get today's transcriptions: %w", err)
	}

	if len(transcriptions) == 0 {
		return "No transcriptions found for today", nil
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

	// Prepare the chat completion request
	req := openai.ChatCompletionRequest{
		Model:     openai.GPT4o,
		MaxTokens: 500,
		Messages: []openai.ChatCompletionMessage{
			{
				Role: openai.ChatMessageRoleSystem,
				Content: "Analyze the following transcript and provide a narrative synopsis. " +
					"Write punchy single sentence paragraphs, each one prefixed by a relevant emoji, different ones. " +
					"Emphasize key words and salient concepts with CAPS. " +
					"Keep it real, authentic, and not too long. Write in lower case weird style.",
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
