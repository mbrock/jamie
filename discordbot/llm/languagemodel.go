package llm

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

type LanguageModel interface {
	GenerateResponse(prompt string) (string, error)
}

type OpenAILanguageModel struct {
	client *openai.Client
}

func NewOpenAILanguageModel(apiKey string) *OpenAILanguageModel {
	return &OpenAILanguageModel{
		client: openai.NewClient(apiKey),
	}
}

func (o *OpenAILanguageModel) GenerateResponse(prompt string) (string, error) {
	resp, err := o.client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: openai.GPT3Dot5Turbo,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a helpful assistant.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
		},
	)

	if err != nil {
		return "", fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}

	return resp.Choices[0].Message.Content, nil
}
