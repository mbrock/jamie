package web

import (
	"html/template"
	"net/http"
	"time"

	"jamie/db"

	"github.com/charmbracelet/log"
)

type Handler struct {
	db     *db.DB
	logger *log.Logger
}

func NewHandler(db *db.DB, logger *log.Logger) *Handler {
	return &Handler{
		db:     db,
		logger: logger,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		h.handleIndex(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleIndex(w http.ResponseWriter, _ *http.Request) {
	transcriptions, err := h.db.GetRecentTranscriptions()
	if err != nil {
		h.logger.Error("failed to get transcriptions", "error", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	tmpl := template.Must(template.New("index").Parse(`
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>All Transcriptions</title>
    <script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
    <div class="container mx-auto px-4 py-8">
        <h1 class="text-3xl font-bold mb-6">All Transcriptions</h1>
        <div class="space-y-4">
            {{range .}}
            <div class="bg-white shadow rounded-lg p-4">
                <p class="text-gray-600 text-sm">{{.Timestamp.Format "2006-01-02 15:04:05"}}</p>
                <p class="text-lg">{{.Emoji}} {{.Text}}</p>
            </div>
            {{end}}
        </div>
    </div>
</body>
</html>
`))

	err = tmpl.Execute(w, transcriptions)
	if err != nil {
		h.logger.Error("failed to execute template", "error", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

type Transcription struct {
	Emoji     string
	Text      string
	Timestamp time.Time
}
