package tts

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"node.town/db"
)

// TranscriptWord represents a single word in a transcript
type TranscriptWord struct {
	Content           string    // The text content of the word
	RelativeStartTime float64   // Start time of the word relative to the beginning of the session (in seconds)
	RelativeEndTime   float64   // End time of the word relative to the beginning of the session (in seconds)
	Confidence        float64   // Confidence score of the transcription
	IsEOS             bool      // Indicates if this word is at the end of a sentence
	AttachesTo        string    // Indicates how this word attaches to the previous word
	AbsoluteStartTime time.Time // The absolute start time of the word in real-world time
	SessionID         int64     // ID of the transcription session
}

// TranscriptSegment represents a segment of transcription, which may be partial or final
type TranscriptSegment struct {
	SessionID int64            // ID of the transcription session
	IsFinal   bool             // Indicates if this is a final transcription
	Words     []TranscriptWord // The words in this segment
}

// AudioStreamUpdate represents an update from the audio stream processing
type AudioStreamUpdate struct {
	SessionID int64     // ID of the transcription session
	SSRC      int64     // Synchronization Source identifier
	Timestamp time.Time // Timestamp of the update
	Data      []byte    // Raw audio data
}

// ConvertDBRowsToTranscriptSegments converts database rows to TranscriptSegment structs
func ConvertDBRowsToTranscriptSegments(
	rows []db.GetTranscriptsRow,
) []TranscriptSegment {
	segmentMap := make(map[int64]TranscriptSegment)

	for _, row := range rows {
		segment, ok := segmentMap[row.ID]
		if !ok {
			segment = TranscriptSegment{
				SessionID: row.SessionID,
				IsFinal:   row.IsFinal,
				Words:     []TranscriptWord{},
			}
		}

		word := TranscriptWord{
			Content:           row.Content,
			RelativeStartTime: float64(row.StartTime.Microseconds) / 1000000,
			RelativeEndTime: float64(
				row.StartTime.Microseconds+row.Duration.Microseconds,
			) / 1000000,
			Confidence:        row.Confidence,
			IsEOS:             row.IsEos,
			AttachesTo:        row.AttachesTo.String,
			AbsoluteStartTime: row.RealStartTime.Time,
			SessionID:         row.SessionID,
		}

		segment.Words = append(segment.Words, word)
		segmentMap[row.ID] = segment
	}

	segments := make([]TranscriptSegment, 0, len(segmentMap))
	for _, segment := range segmentMap {
		segments = append(segments, segment)
	}

	return segments
}

func LoadRecentTranscripts(
	dbQueries *db.Queries,
) ([]TranscriptSegment, error) {
	// Fetch transcripts from the last 8 hours
	eightHoursAgo := time.Now().Add(-16 * time.Hour)

	segments, err := dbQueries.GetTranscripts(
		context.Background(),
		db.GetTranscriptsParams{
			SegmentID: pgtype.Int8{Valid: false},
			CreatedAt: pgtype.Timestamptz{Time: eightHoursAgo, Valid: true},
		},
	)
	if err != nil {
		return nil, err
	}

	return ConvertDBRowsToTranscriptSegments(segments), nil
}
