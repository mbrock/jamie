package llm

import (
	"context"
	"fmt"
	"jamie/db"
	"sort"
	"strings"
)

func SummarizeTranscript(
	queries *db.Queries,
	languageModel LanguageModel,
	promptName string,
) (<-chan string, error) {
	// Get recent text messages
	messages, err := queries.GetRecentTextMessages(
		context.Background(),
		db.GetRecentTextMessagesParams{
			DiscordChannel: "", // We'll need to pass this parameter or modify the query
			Limit:          50,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("get recent text messages: %w", err)
	}

	// Get recent recognitions
	recognitions, err := queries.GetRecentRecognitions(
		context.Background(),
		50,
	)
	if err != nil {
		return nil, fmt.Errorf("get recent recognitions: %w", err)
	}

	// Combine and sort messages and recognitions
	type contextItem struct {
		content   string
		createdAt float64
	}
	var items []contextItem

	for _, msg := range messages {
		sender := "User"
		if msg.IsBot {
			sender = "Bot"
		}
		items = append(items, contextItem{
			content:   fmt.Sprintf("%s: %s", sender, msg.Content),
			createdAt: msg.CreatedAt,
		})
	}

	for _, rec := range recognitions {
		items = append(items, contextItem{
			content:   fmt.Sprintf("%s: %s", rec.Emoji, rec.Text),
			createdAt: rec.CreatedAt,
		})
	}

	// Sort items by createdAt
	sort.Slice(items, func(i, j int) bool {
		return items[i].createdAt < items[j].createdAt
	})

	// Format the context
	var formattedContext strings.Builder
	formattedContext.WriteString(
		"Recent conversation and voice transcriptions:\n",
	)
	for _, item := range items {
		formattedContext.WriteString(item.content + "\n")
	}

	ctx := context.Background()

	// Get the system prompt
	var systemPrompt string
	if promptName != "" {
		systemPrompt, err = queries.GetSystemPrompt(
			ctx,
			promptName,
		)
		if err != nil {
			return nil, fmt.Errorf("get system prompt: %w", err)
		}
	} else {
		systemPrompt = "Analyze the following transcript and provide a narrative synopsis. " +
			"Write punchy single sentence paragraphs, each one prefixed by a relevant emoji, different ones. " +
			"Emphasize key words and salient concepts with CAPS."
	}

	// req := openai.ChatCompletionRequest{
	// 	Model: openai.GPT4o,
	// 	Messages: []openai.ChatCompletionMessage{
	// 		{
	// 			Role:    openai.ChatMessageRoleSystem,
	// 			Content: systemPrompt,
	// 		},
	// 		{
	// 			Role: openai.ChatMessageRoleUser,
	// 			Content: fmt.Sprintf(
	// 				"CONTEXT: %s",
	// 				formattedContext.String(),
	// 			),
	// 		},
	// 	},
	// 	MaxTokens: 400,
	// 	Stream:    true,
	// }

	summaryChannel := make(chan string, 50)

	go func() {
		defer close(summaryChannel)

		response, err := languageModel.ChatCompletion(
			ctx,
			(&ChatCompletionRequest{
				SystemPrompt: systemPrompt,
				MaxTokens:    400,
			}).WithUserMessage(formattedContext.String()),
		)

		if err != nil {
			summaryChannel <- fmt.Sprintf("Error generating response: %v", err)
			return
		}

		var chunk *ChatCompletionResponse
		for chunk = range response {
			summaryChannel <- chunk.Content
		}

		if chunk.Err != nil {
			summaryChannel <- fmt.Sprintf("Error generating response: %v", chunk.Err)
			return
		}
	}()

	return summaryChannel, nil
}
