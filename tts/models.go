package tts

import "time"

// TranscriptWord represents a single word in a transcript
type TranscriptWord struct {
	Content           string    // The text content of the word
	RelativeStartTime float64   // Start time of the word relative to the beginning of the session (in seconds)
	RelativeEndTime   float64   // End time of the word relative to the beginning of the session (in seconds)
	Confidence        float64   // Confidence score of the transcription
	IsEOS             bool      // Indicates if this word is at the end of a sentence
	AttachesTo        string    // Indicates how this word attaches to the previous word
	AbsoluteStartTime time.Time // The absolute start time of the word in real-world time
}

// TranscriptSegment represents a segment of transcription, which may be partial or final
type TranscriptSegment struct {
	SessionID int64           // ID of the transcription session
	IsFinal   bool            // Indicates if this is a final transcription
	Words     []TranscriptWord // The words in this segment
}

// AudioStreamUpdate represents an update from the audio stream processing
type AudioStreamUpdate struct {
	SessionID int64     // ID of the transcription session
	SSRC      int64     // Synchronization Source identifier
	Timestamp time.Time // Timestamp of the update
	Data      []byte    // Raw audio data
}
