package tts

import (
	"testing"
	"time"
)

type testModel model

func newTestModel() testModel {
	return testModel{
		sessions: make(map[int64]*SessionTranscript),
	}
}

func (m *testModel) addFinalTranscript(
	sessionID int64,
	words ...TranscriptWord,
) {
	session, ok := m.sessions[sessionID]
	if !ok {
		session = &SessionTranscript{}
		m.sessions[sessionID] = session
	}
	session.FinalTranscript = append(session.FinalTranscript, words...)
}

func (m *testModel) setCurrentTranscript(
	sessionID int64,
	words ...TranscriptWord,
) {
	session, ok := m.sessions[sessionID]
	if !ok {
		session = &SessionTranscript{}
		m.sessions[sessionID] = session
	}
	session.CurrentTranscript = words
}

func word(
	content string,
	startTime, endTime float64,
	isEOS bool,
) TranscriptWord {
	now := time.Now()
	midnight := time.Date(
		now.Year(),
		now.Month(),
		now.Day(),
		0,
		0,
		0,
		0,
		time.UTC,
	)
	return TranscriptWord{
		Content:       content,
		Confidence:    1.0,
		IsEOS:         isEOS,
		RealStartTime: midnight.Add(time.Duration(startTime) * time.Second),
	}
}

func TestTranscriptView(t *testing.T) {
	t.Run("Single Session", func(t *testing.T) {
		m := newTestModel()
		m.addFinalTranscript(
			1,
			word("A", 0.0, 0.5, false),
			word("B", 0.5, 1.0, true),
		)
		m.setCurrentTranscript(
			1,
			word("C", 1.1, 1.3, false),
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

	t.Run("Session with AttachesTo Period", func(t *testing.T) {
		m := newTestModel()
		m.addFinalTranscript(
			1,
			word("This is a sentence", 0.0, 1.0, false),
			TranscriptWord{
				Content:    ".",
				Confidence: 1.0,
				IsEOS:      true,
				AttachesTo: "previous",
			},
		)
		m.setCurrentTranscript(
			1,
			word("Another sentence", 1.2, 2.0, false),
		)

		expected := "This is a sentence.\nAnother sentence\n"
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
			word("A", 0.0, 0.5, false),
			word("B", 0.5, 1.0, false),
			word("C", 1.0, 1.5, false),
			word("D", 1.5, 2.0, true),
		)
		m.setCurrentTranscript(
			1,
			word("E", 2.5, 3.0, true),
		)

		m.addFinalTranscript(
			2,
			word("1", 0.2, 0.7, false),
			word("2", 0.7, 1.2, true),
		)
		m.setCurrentTranscript(
			2,
			word("3", 2.0, 2.5, false),
			word("4", 2.5, 3.0, true),
		)

		expected := "A B C D\n1 2\n3 4\nE\n"
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
