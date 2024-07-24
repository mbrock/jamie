package llm

import (
	"context"
	"fmt"
	"jamie/db"
	"strings"

	"github.com/charmbracelet/glamour"
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
		Model: openai.GPT4o,
		Messages: []openai.ChatCompletionMessage{
			{
				Role: openai.ChatMessageRoleSystem,
				Content: "You are an AI assistant tasked with summarizing and explaining conversations. " +
					"Please analyze the following transcript and provide a concise summary of the main topics discussed, " +
					"key points made, and any important decisions or actions mentioned. " +
					"Try to capture the essence of the conversation and explain it clearly.",
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
	renderedSummary, err := glamour.Render(summary, "dark")
	if err != nil {
		return "", fmt.Errorf("failed to render summary: %w", err)
	}

	return renderedSummary, nil
}
