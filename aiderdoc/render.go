package aiderdoc

import (
	"context"
	"fmt"
	"log"
	"net/http"
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

		articles := ProcessEntries(entries)
		component := EntriesTemplate(articles)
		err = component.Render(context.Background(), os.Stdout)
		if err != nil {
			log.Fatalf(
				"Error rendering aider input history template: %v",
				err,
			)
		}
	},
}

func Routes(mux *http.ServeMux) {
	mux.HandleFunc("/aider/", handleAiderRequest)
}

func handleAiderRequest(w http.ResponseWriter, r *http.Request) {
	inputFile := filepath.Join(".", ".aider.input.history")
	entries, err := ParseFile(inputFile)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("Error parsing aider input history file: %v", err),
			http.StatusInternalServerError,
		)
		return
	}

	articles := ProcessEntries(entries)
	component := EntriesTemplate(articles)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = component.Render(r.Context(), w)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf(
				"Error rendering aider input history template: %v",
				err,
			),
			http.StatusInternalServerError,
		)
		return
	}
}
