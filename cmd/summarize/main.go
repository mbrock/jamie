package main

import (
	"context"
	"fmt"
	"jamie/db"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sashabaranov/go-openai"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func runSummarizeTranscript(cmd *cobra.Command, args []string) {
	logger := log.New(cmd.OutOrStdout())
	sqlLogger := logger.With("component", "sql")

	err := db.InitDB(sqlLogger)
	if err != nil {
		logger.Fatal("initialize database", "error", err.Error())
	}
	defer db.Close()

	// Get today's transcriptions
	transcriptions, err := db.GetDB().GetTodayTranscriptions()
	if err != nil {
		logger.Fatal("get today's transcriptions", "error", err.Error())
	}

	if len(transcriptions) == 0 {
		logger.Info("No transcriptions found for today")
		return
	}

	// Format transcriptions
	var formattedTranscript strings.Builder
	for _, t := range transcriptions {
		formattedTranscript.WriteString(fmt.Sprintf("%s %s: %s\n", t.Timestamp.Format("15:04:05"), t.Emoji, t.Text))
	}

	// Get OpenAI API key
	openaiAPIKey := viper.GetString("openai_api_key")
	if openaiAPIKey == "" {
		logger.Fatal("missing OPENAI_API_KEY or --openai-api-key=")
	}

	// Create OpenAI client
	client := openai.NewClient(openaiAPIKey)
	ctx := context.Background()

	// Prepare the chat completion request
	req := openai.ChatCompletionRequest{
		Model: openai.GPT3Dot5Turbo,
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
		logger.Fatal("OpenAI API error", "error", err.Error())
	}

	// Print the summary
	fmt.Println("Summary of today's conversation:")
	fmt.Println(resp.Choices[0].Message.Content)
}

func main() {
	cmd := &cobra.Command{
		Use:   "summarize",
		Short: "Summarize today's transcript using OpenAI",
		Run:   runSummarizeTranscript,
	}

	cmd.Flags().String("openai-api-key", "", "OpenAI API key")
	viper.BindPFlag("openai_api_key", cmd.Flags().Lookup("openai-api-key"))

	if err := cmd.Execute(); err != nil {
		fmt.Println(err)
	}
}
