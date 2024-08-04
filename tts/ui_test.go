package tts

import (
	"testing"
)

func TestTranscriptView(t *testing.T) {
	t.Run("Single Session", func(t *testing.T) {
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
	})

	t.Run("Two Interleaved Sessions", func(t *testing.T) {
		// Create a model with two sessions
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
							{
								Content:    "from",
								StartTime:  0.5,
								EndTime:    1.0,
								Confidence: 0.8,
							},
							{
								Content:    "session",
								StartTime:  1.0,
								EndTime:    1.5,
								Confidence: 0.9,
								IsEOS:      true,
							},
						},
					},
					CurrentTranscript: []TranscriptWord{
						{
							Content:    "one",
							StartTime:  2.5,
							EndTime:    3.0,
							Confidence: 0.7,
							IsEOS:      true,
						},
					},
				},
				2: {
					FinalTranscripts: [][]TranscriptWord{
						{
							{
								Content:    "Greetings",
								StartTime:  0.2,
								EndTime:    0.7,
								Confidence: 0.9,
							},
							{
								Content:    "from",
								StartTime:  0.7,
								EndTime:    1.2,
								Confidence: 0.8,
								IsEOS:      true,
							},
						},
					},
					CurrentTranscript: []TranscriptWord{
						{
							Content:    "session",
							StartTime:  2.0,
							EndTime:    2.5,
							Confidence: 0.8,
						},
						{
							Content:    "two",
							StartTime:  2.5,
							EndTime:    3.0,
							Confidence: 0.8,
							IsEOS:      true,
						},
					},
				},
			},
		}

		expected := "Hello from session\nGreetings from\none\nsession two\n"
		result := m.TranscriptView()

		if result != expected {
			t.Errorf(
				"TranscriptView() returned incorrect result.\nExpected:\n%s\nGot:\n%s",
				expected,
				result,
			)
		}
	})
}
