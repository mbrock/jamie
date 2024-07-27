package llm

import (
	"context"
	"fmt"
	"io"

	"github.com/sashabaranov/go-openai"
)

type LanguageModel interface {
	ChatCompletion(
		ctx context.Context,
		req *ChatCompletionRequest,
	) (chan *ChatCompletionResponse, error)
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
	Temperature  float32
}

func (r *ChatCompletionRequest) WithUserMessage(
	message string,
) *ChatCompletionRequest {
	r.UserMessages = append(r.UserMessages, message)
	return r
}

type ChatCompletionResponse struct {
	Err     error
	Content string
}

func (o *OpenAILanguageModel) ChatCompletion(
	ctx context.Context,
	req *ChatCompletionRequest,
) (chan *ChatCompletionResponse, error) {
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: req.SystemPrompt,
		},
	}

	fmt.Printf("System message: %s\n", req.SystemPrompt)

	for _, userMessage := range req.UserMessages {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: userMessage,
		})
		fmt.Printf("User message: %s\n", userMessage)
	}

	resp, err := o.client.CreateChatCompletionStream(
		ctx,
		openai.ChatCompletionRequest{
			Model:       openai.GPT4o,
			Messages:    messages,
			MaxTokens:   req.MaxTokens,
			Temperature: req.Temperature,
			Stream:      true,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}

	result := make(chan *ChatCompletionResponse)
	go func() {
		defer close(result)
		for {
			response, err := resp.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				result <- &ChatCompletionResponse{
					Err: err,
				}
				break
			}
			result <- &ChatCompletionResponse{
				Content: response.Choices[0].Delta.Content,
			}
		}
	}()

	return result, nil
}
