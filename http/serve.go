package http

import (
	"fmt"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/cobra"
	"node.town/aiderdoc"
	"node.town/db"
	"node.town/tts"
)

func Serve(port int) error {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	_, queries, err := db.OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		component := RoutesList(r.Routes())
		err := component.Render(req.Context(), w)
		if err != nil {
			http.Error(
				w,
				"Failed to render routes list",
				http.StatusInternalServerError,
			)
		}
	})

	log.Info("http", "url", fmt.Sprintf("http://localhost:%d", port))
	return http.ListenAndServe(fmt.Sprintf(":%d", port), r)
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
