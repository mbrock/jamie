package web

import "time"

type Transcription struct {
	Emoji     string
	Text      string
	Timestamp time.Time
}
