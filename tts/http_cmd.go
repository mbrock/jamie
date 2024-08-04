package tts

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
	"node.town/db"
)

var HTTPCmd = &cobra.Command{
	Use:   "http",
	Short: "Start an HTTP server to display transcripts",
	Long:  `This command starts an HTTP server that displays the past eight hours of transcripts using HTML rendering.`,
	Run:   runHTTPServer,
}

func init() {
	HTTPCmd.Flags().IntP("port", "p", 8080, "Port to run the HTTP server on")
}

func runHTTPServer(cmd *cobra.Command, args []string) {
	port, _ := cmd.Flags().GetInt("port")

	sqlDB, queries, err := db.OpenDatabase()
	if err != nil {
		fmt.Printf("Failed to open database: %v\n", err)
		return
	}
	defer sqlDB.Close()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		transcripts, err := LoadRecentTranscripts(queries)
		if err != nil {
			http.Error(
				w,
				fmt.Sprintf("Failed to load transcripts: %v", err),
				http.StatusInternalServerError,
			)
			return
		}

		builder := NewTranscriptBuilder()
		for _, segment := range transcripts {
			builder.AppendWords(segment.Words, false)
		}

		html, err := builder.RenderHTML()
		if err != nil {
			http.Error(
				w,
				"Failed to render HTML",
				http.StatusInternalServerError,
			)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, html)
	})

	fmt.Printf("Starting HTTP server on port %d...\n", port)
	err = http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	if err != nil {
		fmt.Printf("Failed to start HTTP server: %v\n", err)
	}
}
