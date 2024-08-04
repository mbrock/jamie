package tts

import (
	"testing"
)

func newTestModel() model {
	return model{
		sessions: make(map[int64]*SessionTranscript),
	}
}

func addFinalTranscript(m *model, sessionID int64, words []TranscriptWord) {
	session, ok := m.sessions[sessionID]
	if !ok {
		session = &SessionTranscript{}
		m.sessions[sessionID] = session
	}
	session.FinalTranscripts = append(session.FinalTranscripts, words)
}

func setCurrentTranscript(m *model, sessionID int64, words []TranscriptWord) {
	session, ok := m.sessions[sessionID]
	if !ok {
		session = &SessionTranscript{}
		m.sessions[sessionID] = session
	}
	session.CurrentTranscript = words
}

func newWord(content string, startTime, endTime float64, confidence float64, isEOS bool) TranscriptWord {
	return TranscriptWord{
		Content:    content,
		StartTime:  startTime,
		EndTime:    endTime,
		Confidence: confidence,
		IsEOS:      isEOS,
	}
}

func TestTranscriptView(t *testing.T) {
	t.Run("Single Session", func(t *testing.T) {
		m := newTestModel()
		addFinalTranscript(
			&m,
			1,
			[]TranscriptWord{
				newWord("A", 0.0, 0.5, 0.9, false),
				newWord("B", 0.5, 1.0, 0.8, true),
			},
		)
		setCurrentTranscript(
			&m,
			1,
			[]TranscriptWord{newWord("C", 1.1, 1.3, 0.7, false)},
		)

		expected := "A B\nC\n"
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
		m := newTestModel()
		addFinalTranscript(
			&m,
			1,
			[]TranscriptWord{
				newWord("A", 0.0, 0.5, 0.9, false),
				newWord("B", 0.5, 1.0, 0.8, false),
				newWord("1", 1.0, 1.5, 0.9, true),
			},
		)
		setCurrentTranscript(
			&m,
			1,
			[]TranscriptWord{newWord("2", 2.5, 3.0, 0.7, true)},
		)

		addFinalTranscript(
			&m,
			2,
			[]TranscriptWord{
				newWord("C", 0.2, 0.7, 0.9, false),
				newWord("D", 0.7, 1.2, 0.8, true),
			},
		)
		setCurrentTranscript(
			&m,
			2,
			[]TranscriptWord{
				newWord("3", 2.0, 2.5, 0.8, false),
				newWord("4", 2.5, 3.0, 0.8, true),
			},
		)

		expected := "A B 1\nC D\n2\n3 4\n"
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
