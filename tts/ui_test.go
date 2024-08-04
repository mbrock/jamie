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
						},
						{
							{
								Content:    "from",
								StartTime:  1.0,
								EndTime:    1.5,
								Confidence: 0.8,
							},
						},
					},
					CurrentTranscript: []TranscriptWord{
						{
							Content:    "session",
							StartTime:  2.0,
							EndTime:    2.5,
							Confidence: 0.7,
						},
						{
							Content:    "one",
							StartTime:  2.5,
							EndTime:    3.0,
							Confidence: 0.7,
						},
					},
				},
				2: {
					FinalTranscripts: [][]TranscriptWord{
						{
							{
								Content:    "Greetings",
								StartTime:  0.5,
								EndTime:    1.0,
								Confidence: 0.9,
							},
						},
					},
					CurrentTranscript: []TranscriptWord{
						{
							Content:    "from",
							StartTime:  1.5,
							EndTime:    2.0,
							Confidence: 0.8,
						},
						{
							Content:    "session",
							StartTime:  3.0,
							EndTime:    3.5,
							Confidence: 0.8,
						},
						{
							Content:    "two",
							StartTime:  3.5,
							EndTime:    4.0,
							Confidence: 0.8,
						},
					},
				},
			},
		}

		expected := "Hello Greetings from from session one session two\n"
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
