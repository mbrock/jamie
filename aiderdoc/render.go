package aiderdoc

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var AiderdocCmd = &cobra.Command{
	Use:   "aiderdoc [input file]",
	Short: "Render the history of an aider code agent input session",
	Long:  `This command parses the specified input file (or the default .aider.input.history file) and renders the entries of an aider code agent input session using the EntriesTemplate. It provides a formatted view of the interaction history between the user and the AI assistant.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var inputFile string
		if len(args) > 0 {
			inputFile = args[0]
		} else {
			inputFile = filepath.Join(".", ".aider.input.history")
		}

		entries, err := ParseFile(inputFile)
		if err != nil {
			log.Fatalf("Error parsing aider input history file: %v", err)
		}

		component := EntriesTemplate(entries)
		err = component.Render(context.Background(), os.Stdout)
		if err != nil {
			log.Fatalf(
				"Error rendering aider input history template: %v",
				err,
			)
		}
	},
}

func init() {
	// Add any flags here if needed
}
