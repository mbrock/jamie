package tts

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"node.town/db"
	"node.town/snd"
	"node.town/speechmatics"
)

type TranscriptionService interface {
	ConnectWebSocket(
		ctx context.Context,
		config speechmatics.TranscriptionConfig,
		audioFormat speechmatics.AudioFormat,
	) error
	SendAudio(audio []byte) error
	ReceiveTranscript(
		ctx context.Context,
	) (<-chan speechmatics.RTTranscriptResponse, <-chan error)
	EndStream(seqNo int) error
	CloseWebSocket() error
}

type SpeechmaticsService struct {
	client *speechmatics.Client
}

func NewSpeechmaticsService(apiKey string) *SpeechmaticsService {
	return &SpeechmaticsService{
		client: speechmatics.NewClient(apiKey),
	}
}

func (s *SpeechmaticsService) ConnectWebSocket(
	ctx context.Context,
	config speechmatics.TranscriptionConfig,
	audioFormat speechmatics.AudioFormat,
) error {
	return s.client.ConnectWebSocket(ctx, config, audioFormat)
}

func (s *SpeechmaticsService) SendAudio(audio []byte) error {
	return s.client.SendAudio(audio)
}

func (s *SpeechmaticsService) ReceiveTranscript(
	ctx context.Context,
) (<-chan speechmatics.RTTranscriptResponse, <-chan error) {
	return s.client.ReceiveTranscript(ctx)
}

func (s *SpeechmaticsService) EndStream(seqNo int) error {
	return s.client.EndStream(seqNo)
}

func (s *SpeechmaticsService) CloseWebSocket() error {
	return s.client.CloseWebSocket()
}

type TranscriptionHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
	service TranscriptionService
}

func NewTranscriptionHandler(
	queries *db.Queries,
	pool *pgxpool.Pool,
	service TranscriptionService,
) *TranscriptionHandler {
	return &TranscriptionHandler{
		queries: queries,
		pool:    pool,
		service: service,
	}
}

func (h *TranscriptionHandler) HandleTranscript(
	ctx context.Context,
	transcript speechmatics.RTTranscriptResponse,
	sessionID int64,
) error {
	if len(transcript.Results) == 0 {
		return nil
	}

	log.Info(
		"Handling transcript",
		"isPartial", transcript.IsPartial(),
		"resultCount", len(transcript.Results),
	)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	row, err := qtx.UpsertTranscriptionSegment(
		ctx,
		db.UpsertTranscriptionSegmentParams{
			SessionID: sessionID,
			IsFinal:   !transcript.IsPartial(),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to upsert transcription segment: %w", err)
	}
	segmentID := row.ResultSegmentID
	currentVersion := row.ResultVersion

	for _, result := range transcript.Results {
		wordID, err := qtx.InsertTranscriptionWord(
			ctx,
			db.InsertTranscriptionWordParams{
				SegmentID: segmentID,
				StartTime: result.StartTime,
				Duration:  result.EndTime - result.StartTime,
				IsEos:     result.IsEOS,
				Version:   int32(currentVersion),
				AttachesTo: pgtype.Text{
					String: result.AttachesTo,
					Valid:  true,
				},
			},
		)
		if err != nil {
			return fmt.Errorf("failed to insert transcription word: %w", err)
		}

		for _, alt := range result.Alternatives {
			err = qtx.InsertWordAlternative(
				ctx,
				db.InsertWordAlternativeParams{
					WordID:     wordID,
					Content:    alt.Content,
					Confidence: alt.Confidence,
				},
			)
			if err != nil {
				return fmt.Errorf(
					"failed to insert word alternative: %w",
					err,
				)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Info(
		"Processed transcript",
		"segmentID", segmentID,
		"wordCount", len(transcript.Results),
		"isPartial", transcript.IsPartial(),
		"version", currentVersion,
	)

	return nil
}

func (h *TranscriptionHandler) ProcessAudioStream(
	ctx context.Context,
	stream <-chan snd.OpusPacketNotification,
	sessionID int64,
) error {
	config := speechmatics.TranscriptionConfig{
		Language:       "en",
		EnablePartials: true,
	}
	audioFormat := speechmatics.AudioFormat{
		Type: "file",
	}

	if err := h.service.ConnectWebSocket(ctx, config, audioFormat); err != nil {
		return fmt.Errorf(
			"failed to connect to Speechmatics WebSocket: %w",
			err,
		)
	}
	defer h.service.CloseWebSocket()

	transcriptChan, errChan := h.service.ReceiveTranscript(ctx)

	oggWriter, buffer, err := setupOggWriter(sessionID)
	if err != nil {
		return fmt.Errorf("failed to setup Ogg writer: %w", err)
	}
	defer oggWriter.Close()

	seqNo := 0
	lastPacketTime := time.Now()
	silenceTimer := time.NewTicker(100 * time.Millisecond)
	defer silenceTimer.Stop()

	go h.handleTranscripts(ctx, transcriptChan, errChan, sessionID)

	for {
		select {
		case packet, ok := <-stream:
			if !ok {
				return h.finalizeStream(buffer, seqNo)
			}
			if err := h.processPacket(packet, oggWriter, buffer, &seqNo, &lastPacketTime); err != nil {
				return err
			}
		case <-silenceTimer.C:
			if err := h.handleSilence(oggWriter, buffer, &lastPacketTime); err != nil {
				return err
			}
		case <-ctx.Done():
			return h.finalizeStream(buffer, seqNo)
		}
	}
}

func (h *TranscriptionHandler) handleTranscripts(
	ctx context.Context,
	transcriptChan <-chan speechmatics.RTTranscriptResponse,
	errChan <-chan error,
	sessionID int64,
) {
	for {
		select {
		case transcript, ok := <-transcriptChan:
			if !ok {
				return
			}
			if err := h.HandleTranscript(ctx, transcript, sessionID); err != nil {
				log.Error("Failed to handle transcript", "error", err)
			}
		case err := <-errChan:
			log.Error("Received error from Speechmatics", "error", err)
		case <-ctx.Done():
			return
		}
	}
}

func (h *TranscriptionHandler) processPacket(
	packet snd.OpusPacketNotification,
	oggWriter *snd.Ogg,
	buffer *bytes.Buffer,
	seqNo *int,
	lastPacketTime *time.Time,
) error {
	createdAt, err := time.Parse(time.RFC3339Nano, packet.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to parse createdAt: %w", err)
	}
	opusPacket := snd.OpusPacket{
		ID:        int(packet.ID),
		Sequence:  uint16(packet.Sequence),
		Timestamp: uint32(packet.Timestamp),
		CreatedAt: createdAt,
		OpusData:  []byte(packet.OpusData),
	}

	if err := oggWriter.WritePacket(opusPacket); err != nil {
		return fmt.Errorf("failed to write packet to Ogg: %w", err)
	}

	if err := h.service.SendAudio(buffer.Bytes()); err != nil {
		return fmt.Errorf("failed to send audio to Speechmatics: %w", err)
	}
	buffer.Reset()

	*seqNo++
	*lastPacketTime = time.Now()
	return nil
}

func (h *TranscriptionHandler) handleSilence(
	oggWriter *snd.Ogg,
	buffer *bytes.Buffer,
	lastPacketTime *time.Time,
) error {
	if time.Since(*lastPacketTime) >= 100*time.Millisecond {
		if err := oggWriter.WriteSilence(time.Since(*lastPacketTime)); err != nil {
			return fmt.Errorf("failed to write silence to Ogg: %w", err)
		}

		if err := h.service.SendAudio(buffer.Bytes()); err != nil {
			return fmt.Errorf(
				"failed to send silence to Speechmatics: %w",
				err,
			)
		}
		buffer.Reset()

		*lastPacketTime = time.Now()
	}
	return nil
}

func (h *TranscriptionHandler) finalizeStream(
	buffer *bytes.Buffer,
	seqNo int,
) error {
	if buffer.Len() > 0 {
		if err := h.service.SendAudio(buffer.Bytes()); err != nil {
			log.Error(
				"Failed to send final audio to Speechmatics",
				"error",
				err,
			)
		}
	}
	if err := h.service.EndStream(seqNo); err != nil {
		log.Error("Failed to end Speechmatics stream", "error", err)
	}
	return nil
}

func setupOggWriter(sessionID int64) (*snd.Ogg, *bytes.Buffer, error) {
	tmpDir := "tmp"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create tmp directory: %w", err)
	}

	oggFilePath := filepath.Join(tmpDir, fmt.Sprintf("%d.ogg", sessionID))
	oggFile, err := os.Create(oggFilePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Ogg file: %w", err)
	}

	var buffer bytes.Buffer
	multiWriter := io.MultiWriter(oggFile, &buffer)
	oggWriterWrapper, err := snd.NewOggWriter(multiWriter)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Ogg writer: %w", err)
	}

	oggWriter, err := snd.NewOgg(
		sessionID,
		time.Now(),
		time.Now().Add(24*time.Hour),
		oggWriterWrapper,
		&snd.RealTimeProvider{},
		log.Default(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Ogg writer: %w", err)
	}

	log.Info("Created Ogg file", "path", oggFilePath)
	return oggWriter, &buffer, nil
}
