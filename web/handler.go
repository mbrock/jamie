package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
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

func (h *Handler) handleConversations(w http.ResponseWriter, _ *http.Request) {
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

	conversationsWithTranscriptions := make([]ConversationWithTranscriptions, 0, len(conversations))

	for _, conv := range conversations {
		transcriptions, err := h.db.GetTranscriptionsForTimeRange(conv.StartTime, conv.EndTime)
		if err != nil {
			h.logger.Error("failed to get transcriptions for conversation", "error", err.Error(), "start_time", conv.StartTime, "end_time", conv.EndTime)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		h.logger.Debug("Fetched transcriptions", "count", len(transcriptions), "start_time", conv.StartTime, "end_time", conv.EndTime)
		conversationsWithTranscriptions = append(conversationsWithTranscriptions, ConversationWithTranscriptions{
			StartTime:      conv.StartTime,
			EndTime:        conv.EndTime,
			Transcriptions: transcriptions,
		})
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

	transcriptions, err := h.db.GetTranscriptionsForTimeRange(startTime, endTime)
	if err != nil {
		h.logger.Error("failed to get transcriptions", "error", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if len(transcriptions) == 0 {
		http.Error(w, "No transcriptions found for the given time range", http.StatusNotFound)
		return
	}

	startSample := transcriptions[0].SampleIdx
	endSample := transcriptions[len(transcriptions)-1].SampleIdx

	oggData, err := generateOggOpusBlob(streamID, startSample, endSample)
	if err != nil {
		h.logger.Error("failed to generate OGG Opus blob", "error", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "audio/ogg")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"audio_%s_%s_%s.ogg\"", streamID, startTimeStr, endTimeStr))
	w.Header().Set("Content-Length", strconv.Itoa(len(oggData)))

	_, err = w.Write(oggData)
	if err != nil {
		h.logger.Error("failed to write OGG data to response", "error", err.Error())
	}
}
import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"jamie/db"

	"github.com/charmbracelet/log"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)
func generateOggOpusBlob(streamID string, startSample, endSample int) ([]byte, error) {
	packets, err := db.GetDB().GetPacketsForStreamInSampleRange(streamID, startSample, endSample)
	if err != nil {
		return nil, fmt.Errorf("fetch packets: %w", err)
	}

	var oggBuffer bytes.Buffer

	oggWriter, err := oggwriter.NewWith(&oggBuffer, 48000, 2)
	if err != nil {
		return nil, fmt.Errorf("create OGG writer: %w", err)
	}

	var lastSampleIdx int
	for _, packet := range packets {
		if lastSampleIdx != 0 {
			gap := packet.SampleIdx - lastSampleIdx
			if gap > 960 { // 960 samples = 20ms at 48kHz
				silentPacketsCount := gap / 960
				for j := 0; j < silentPacketsCount; j++ {
					silentPacket := []byte{0xf8, 0xff, 0xfe}
					if err := oggWriter.WriteRTP(&rtp.Packet{
						Header: rtp.Header{
							Timestamp: uint32(lastSampleIdx + (j * 960)),
						},
						Payload: silentPacket,
					}); err != nil {
						return nil, fmt.Errorf("write silent Opus packet: %w", err)
					}
				}
			}
		}

		if err := oggWriter.WriteRTP(&rtp.Packet{
			Header: rtp.Header{
				Timestamp: uint32(packet.SampleIdx),
			},
			Payload: packet.Payload,
		}); err != nil {
			return nil, fmt.Errorf("write Opus packet: %w", err)
		}

		lastSampleIdx = packet.SampleIdx
	}

	if err := oggWriter.Close(); err != nil {
		return nil, fmt.Errorf("close OGG writer: %w", err)
	}

	return oggBuffer.Bytes(), nil
}
