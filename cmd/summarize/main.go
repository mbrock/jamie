package main

import (
	"fmt"
	"jamie/db"
	"jamie/llm"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/log"
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

	// Get OpenAI API key
	openaiAPIKey := viper.GetString("openai_api_key")
	if openaiAPIKey == "" {
		logger.Fatal("missing OPENAI_API_KEY or --openai-api-key=")
	}

	summary, err := llm.SummarizeTranscript(openaiAPIKey)
	if err != nil {
		logger.Fatal("failed to summarize transcript", "error", err.Error())
	}

	renderedSummary, err := glamour.Render(summary, "dark")
	if err != nil {
		logger.Fatal("failed to render summary", "error", err.Error())
	}

	fmt.Println("Summary of today's conversation:")
	fmt.Println(renderedSummary)
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
