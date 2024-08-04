package tts

import (
	"testing"
)

type testModel model

func newTestModel() testModel {
	return testModel{
		sessions: make(map[int64]*SessionTranscript),
	}
}

func (m *testModel) addFinalTranscript(sessionID int64, words ...TranscriptWord) {
	session, ok := m.sessions[sessionID]
	if !ok {
		session = &SessionTranscript{}
		m.sessions[sessionID] = session
	}
	session.FinalTranscripts = append(session.FinalTranscripts, words)
}

func (m *testModel) setCurrentTranscript(sessionID int64, words ...TranscriptWord) {
	session, ok := m.sessions[sessionID]
	if !ok {
		session = &SessionTranscript{}
		m.sessions[sessionID] = session
	}
	session.CurrentTranscript = words
}

func newWord(content string, startTime, endTime float64, isEOS bool) TranscriptWord {
	return TranscriptWord{
		Content:    content,
		StartTime:  startTime,
		EndTime:    endTime,
		Confidence: 1.0,
		IsEOS:      isEOS,
	}
}

func TestTranscriptView(t *testing.T) {
	t.Run("Single Session", func(t *testing.T) {
		m := newTestModel()
		m.addFinalTranscript(
			1,
			newWord("A", 0.0, 0.5, false),
			newWord("B", 0.5, 1.0, true),
		)
		m.setCurrentTranscript(
			1,
			newWord("C", 1.1, 1.3, false),
		)

		expected := "A B\nC\n"
		result := model(m).TranscriptView()

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
		m.addFinalTranscript(
			1,
			newWord("A", 0.0, 0.5, false),
			newWord("B", 0.5, 1.0, false),
			newWord("1", 1.0, 1.5, true),
		)
		m.setCurrentTranscript(
			1,
			newWord("2", 2.5, 3.0, true),
		)

		m.addFinalTranscript(
			2,
			newWord("C", 0.2, 0.7, false),
			newWord("D", 0.7, 1.2, true),
		)
		m.setCurrentTranscript(
			2,
			newWord("3", 2.0, 2.5, false),
			newWord("4", 2.5, 3.0, true),
		)

		expected := "A B 1\nC D\n2\n3 4\n"
		result := model(m).TranscriptView()

		if result != expected {
			t.Errorf(
				"TranscriptView() returned incorrect result.\nExpected:\n%s\nGot:\n%s",
				expected,
				result,
			)
		}
	})
}
