package llm

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

type LanguageModel interface {
	ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error)
}

type OpenAILanguageModel struct {
	client *openai.Client
}

func NewOpenAILanguageModel(apiKey string) *OpenAILanguageModel {
	return &OpenAILanguageModel{
		client: openai.NewClient(apiKey),
	}
}

type ChatCompletionRequest struct {
	SystemPrompt string
	UserMessages []string
	MaxTokens    int
	Stream       bool
}

func (r *ChatCompletionRequest) WithUserMessage(message string) *ChatCompletionRequest {
	r.UserMessages = append(r.UserMessages, message)
	return r
}

func (r *ChatCompletionRequest) Stream() *ChatCompletionRequest {
	r.Stream = true
	return r
}

type ChatCompletionResponse struct {
	Content string
}

func (o *OpenAILanguageModel) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: req.SystemPrompt,
		},
	}

	for _, userMessage := range req.UserMessages {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: userMessage,
		})
	}

	resp, err := o.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model:     openai.GPT3Dot5Turbo,
			Messages:  messages,
			MaxTokens: req.MaxTokens,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI")
	}

	return &ChatCompletionResponse{
		Content: resp.Choices[0].Message.Content,
	}, nil
}
