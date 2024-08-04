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

	tts.Routes(r, queries)
	aiderdoc.Routes(r)

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<h1>Available Routes</h1>")
		tree := r.Routes()
		for _, route := range tree {
			for method := range route.Handlers {
				if method == "GET" {
					fmt.Fprintf(
						w,
						"<p><a href='%s'>%s %s</a></p>",
						route.Pattern,
						method,
						route.Pattern,
					)
				} else {
					fmt.Fprintf(w, "<p>%s %s</p>", method, route.Pattern)
				}
			}
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
