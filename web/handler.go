package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"jamie/db"
	"jamie/ogg"

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
	if r.URL.Path == "/stream-audio/" {
		h.handleStreamAudio(w, r)
	} else {
		switch r.URL.Path {
		case "/":
			h.handleIndex(w, r)
		case "/conversations":
			h.handleConversations(w, r)
		default:
			http.NotFound(w, r)
		}
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

func (h *Handler) handleConversations(
	w http.ResponseWriter,
	_ *http.Request,
) {
	conversations, err := h.db.GetConversationTimeRanges(3 * time.Minute)
	if err != nil {
		h.logger.Error("failed to get conversations", "error", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	type ConversationWithTranscriptions struct {
		StartTime      time.Time
		EndTime        time.Time
		Transcriptions []db.Transcription
	}

	conversationsWithTranscriptions := make(
		[]ConversationWithTranscriptions,
		0,
		len(conversations),
	)

	for _, conv := range conversations {
		transcriptions, err := h.db.GetTranscriptionsForTimeRange(
			conv.StartTime,
			conv.EndTime,
		)
		if err != nil {
			h.logger.Error(
				"failed to get transcriptions for conversation",
				"error",
				err.Error(),
				"start_time",
				conv.StartTime,
				"end_time",
				conv.EndTime,
			)
			http.Error(
				w,
				"Internal Server Error",
				http.StatusInternalServerError,
			)
			return
		}
		h.logger.Debug(
			"Fetched transcriptions",
			"count",
			len(transcriptions),
			"start_time",
			conv.StartTime,
			"end_time",
			conv.EndTime,
		)
		conversationsWithTranscriptions = append(
			conversationsWithTranscriptions,
			ConversationWithTranscriptions{
				StartTime:      conv.StartTime,
				EndTime:        conv.EndTime,
				Transcriptions: transcriptions,
			},
		)
	}

	tmpl := template.Must(template.New("conversations").Parse(`
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Conversations</title>
    <script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
    <div class="container mx-auto px-4 py-8">
        <h1 class="text-3xl font-bold mb-6">Conversations</h1>
        <div class="space-y-4">
            {{range .}}
            <div class="bg-white shadow rounded-lg p-4">
                <p class="text-gray-600 text-sm">Start: {{.StartTime.Format "2006-01-02 15:04:05"}}</p>
                <p class="text-gray-600 text-sm">End: {{.EndTime.Format "2006-01-02 15:04:05"}}</p>
                <p class="text-lg mb-2">Duration: {{.EndTime.Sub .StartTime}}</p>
                <audio controls class="w-full mb-2">
                    <source src="/stream-audio/?stream={{.Transcriptions.0.StreamID}}&start={{.StartTime.Format "2006-01-02T15:04:05Z07:00"}}&end={{.EndTime.Format "2006-01-02T15:04:05Z07:00"}}" type="audio/ogg">
                    Your browser does not support the audio element.
                </audio>
                <details>
                    <summary class="cursor-pointer text-blue-600 hover:text-blue-800">Show Transcriptions ({{len .Transcriptions}})</summary>
                    <div class="mt-2">
                        {{if .Transcriptions}}
                            <div class="flex flex-wrap gap-2">
                                {{range .Transcriptions}}
                                <div class="bg-gray-100 p-2 rounded flex-grow">
                                    <span class="font-bold">{{.Emoji}}</span> {{.Text}}
                                </div>
                                {{end}}
                            </div>
                        {{else}}
                            <p>No transcriptions found for this conversation.</p>
                        {{end}}
                    </div>
                </details>
            </div>
            {{end}}
        </div>
    </div>
</body>
</html>
`))

	err = tmpl.Execute(w, conversationsWithTranscriptions)
	if err != nil {
		h.logger.Error("failed to execute template", "error", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

type Transcription struct {
	Emoji     string
	Text      string
	Timestamp time.Time
	StreamID  string
}

func (h *Handler) handleStreamAudio(w http.ResponseWriter, r *http.Request) {
	streamID := r.URL.Query().Get("stream")
	startTimeStr := r.URL.Query().Get("start")
	endTimeStr := r.URL.Query().Get("end")

	if streamID == "" || startTimeStr == "" || endTimeStr == "" {
		http.Error(w, "Missing required parameters", http.StatusBadRequest)
		return
	}

	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		http.Error(w, "Invalid start time format", http.StatusBadRequest)
		return
	}

	endTime, err := time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		http.Error(w, "Invalid end time format", http.StatusBadRequest)
		return
	}

	transcriptions, err := h.db.GetTranscriptionsForTimeRange(
		startTime,
		endTime,
	)
	if err != nil {
		h.logger.Error("failed to get transcriptions", "error", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if len(transcriptions) == 0 {
		http.Error(
			w,
			"No transcriptions found for the given time range",
			http.StatusNotFound,
		)
		return
	}

	startSample := transcriptions[0].SampleIdx
	endSample := transcriptions[len(transcriptions)-1].SampleIdx

	oggData, err := generateOggOpusBlob(streamID, startSample, endSample)
	if err != nil {
		h.logger.Error(
			"failed to generate OGG Opus blob",
			"error",
			err.Error(),
		)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "audio/ogg")
	w.Header().
		Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"audio_%s_%s_%s.ogg\"", streamID, startTimeStr, endTimeStr))
	w.Header().Set("Content-Length", strconv.Itoa(len(oggData)))

	_, err = w.Write(oggData)
	if err != nil {
		h.logger.Error(
			"failed to write OGG data to response",
			"error",
			err.Error(),
		)
	}
}

func generateOggOpusBlob(
	streamID string,
	startSample, endSample int,
) ([]byte, error) {
	return ogg.GenerateOggOpusBlob(streamID, startSample, endSample)
}
