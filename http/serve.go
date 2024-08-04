package http

import (
	"fmt"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"node.town/aiderdoc"
	"node.town/db"
	"node.town/tts"
)

func Serve(port int) error {
	mux := http.NewServeMux()

	_, queries, err := db.OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	tts.Routes(mux, queries)
	aiderdoc.Routes(mux)

	log.Info("http", "url", fmt.Sprintf("http://localhost:%d", port))
	return http.ListenAndServe(fmt.Sprintf(":%d", port), mux)
}

var ServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start an HTTP server",
	Long:  `This command starts an HTTP server.`,
	Run: func(cmd *cobra.Command, args []string) {
		port, _ := cmd.Flags().GetInt("port")
		if err := Serve(port); err != nil {
			log.Fatal("Failed to start server", "error", err)
		}
	},
}

func init() {
	ServeCmd.Flags().IntP("port", "p", 4444, "Port to run the HTTP server on")
}
