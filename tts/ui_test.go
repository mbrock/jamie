package tts

import (
	"testing"
)

func TestTranscriptView(t *testing.T) {
	// Create a model with one session
	m := model{
		sessions: map[int64]*SessionTranscript{
			1: {
				FinalTranscripts: [][]TranscriptWord{
					{
						{
							Content:    "Hello",
							StartTime:  0.0,
							EndTime:    0.5,
							Confidence: 0.9,
						},
					},
					{
						{
							Content:    "World",
							StartTime:  0.5,
							EndTime:    1.0,
							Confidence: 0.8,
						},
					},
				},
				CurrentTranscript: []TranscriptWord{
					{
						Content:    "Partial",
						StartTime:  1.1,
						EndTime:    1.3,
						Confidence: 0.7,
					},
				},
			},
		},
	}

	expected := "Hello World Partial\n"
	result := m.TranscriptView()

	if result != expected {
		t.Errorf(
			"TranscriptView() returned incorrect result.\nExpected:\n%s\nGot:\n%s",
			expected,
			result,
		)
	}
}
