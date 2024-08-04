package http

import (
	"fmt"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var routes = make(map[string]func(http.ResponseWriter, *http.Request))

func RegisterRoute(
	path string,
	handler func(http.ResponseWriter, *http.Request),
) {
	routes[path] = handler
}

func Serve(port int) error {
	for path, handler := range routes {
		http.HandleFunc(path, handler)
	}

	log.Info("http", "url", fmt.Sprintf("http://localhost:%d", port))
	return http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
}

var ServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start an HTTP server",
	Long:  `This command starts an HTTP server.`,
	Run: func(cmd *cobra.Command, args []string) {
		port, _ := cmd.Flags().GetInt("port")
		Serve(port)
	},
}

func init() {
	ServeCmd.Flags().IntP("port", "p", 4444, "Port to run the HTTP server on")
}
