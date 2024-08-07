package main

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"node.town/tts"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"node.town/db"
	"node.town/snd"
)

func Routes(r chi.Router, queries *db.Queries) {
	r.Get("/tts/", handleTranscriptPage(queries))
	r.Get(
		"/tts/audio/{sessionID}/{startTime}/{endTime}",
		handleAudioRequest(queries),
	)
}

func handleTranscriptPage(queries *db.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		transcripts, err := tts.LoadRecentTranscripts(queries)
		if err != nil {
			http.Error(
				w,
				fmt.Sprintf("Failed to load transcripts: %v", err),
				http.StatusInternalServerError,
			)
			return
		}

		builder := tts.NewTranscriptBuilder()
		for _, segment := range transcripts {
			if segment.IsFinal {
				builder.AppendWords(segment.Words, false)
			} else {
				builder.AppendWords(segment.Words, true)
			}
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
	}
}

func handleAudioRequest(queries *db.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) != 6 {
			http.Error(w, "Invalid URL format", http.StatusBadRequest)
			return
		}

		sessionID, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			http.Error(w, "Invalid session ID", http.StatusBadRequest)
			return
		}

		startTime, err := time.Parse(time.RFC3339, parts[4])
		if err != nil {
			http.Error(w, "Invalid start time", http.StatusBadRequest)
			return
		}

		endTime, err := time.Parse(time.RFC3339, parts[5])
		if err != nil {
			http.Error(w, "Invalid end time", http.StatusBadRequest)
			return
		}

		// Fetch the SSRC for the given session ID
		ssrc, err := queries.GetSSRCForSession(r.Context(), sessionID)
		if err != nil {
			http.Error(
				w,
				"Failed to get SSRC for session",
				http.StatusInternalServerError,
			)
			return
		}

		// Fetch opus packets for the given time range
		packets, err := queries.GetOpusPacketsForTimeRange(
			r.Context(),
			db.GetOpusPacketsForTimeRangeParams{
				Ssrc:        ssrc,
				CreatedAt:   pgtype.Timestamptz{Time: startTime, Valid: true},
				CreatedAt_2: pgtype.Timestamptz{Time: endTime, Valid: true},
			},
		)
		if err != nil {
			http.Error(
				w,
				"Failed to fetch opus packets",
				http.StatusInternalServerError,
			)
			return
		}

		// Generate OGG file
		oggData, err := generateOggFile(packets)
		if err != nil {
			http.Error(
				w,
				"Failed to generate OGG file",
				http.StatusInternalServerError,
			)
			return
		}

		w.Header().Set("Content-Type", "audio/ogg")
		w.Header().
			Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"audio_%d_%s_%s.ogg\"", sessionID, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339)))
		w.Write(oggData)
	}
}

func generateOggFile(packets []db.OpusPacket) ([]byte, error) {
	var buf bytes.Buffer
	oggWriter, err := snd.NewOggWriter(&buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create OGG writer: %w", err)
	}

	ogg, err := snd.NewOgg(
		packets[0].Ssrc,
		packets[0].CreatedAt.Time,
		packets[len(packets)-1].CreatedAt.Time,
		oggWriter,
		&snd.RealTimeProvider{},
		log.Default(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OGG: %w", err)
	}

	for _, dbPacket := range packets {
		packet := snd.OpusPacket{
			ID:        int(dbPacket.ID),
			Sequence:  uint16(dbPacket.Sequence),
			Timestamp: uint32(dbPacket.Timestamp),
			CreatedAt: dbPacket.CreatedAt.Time,
			OpusData:  dbPacket.OpusData,
		}
		ogg.WritePacket(packet)
	}

	if err := ogg.Close(); err != nil {
		return nil, fmt.Errorf("failed to close OGG: %w", err)
	}

	return buf.Bytes(), nil
}
