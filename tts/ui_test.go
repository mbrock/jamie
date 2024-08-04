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
	startTime int,
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
		Content:    content,
		Confidence: 1.0,
		IsEOS:      isEOS,
		AbsoluteStartTime: midnight.Add(
			time.Duration(startTime) * time.Second,
		),
	}
}

func TestTranscriptView(t *testing.T) {
	t.Run("Single Session", func(t *testing.T) {
		m := newTestModel()
		m.addFinalTranscript(
			1,
			word("A", 0, false),
			word("B", 1, true),
		)
		m.setCurrentTranscript(
			1,
			word("C", 2, false),
		)

		expected := "(00:00:00) A B\n(00:00:02) C\n"
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
			word("This is a sentence", 0, false),
			TranscriptWord{
				Content:    ".",
				Confidence: 1.0,
				IsEOS:      true,
				AttachesTo: "previous",
			},
		)
		m.setCurrentTranscript(
			1,
			word("Another sentence", 2, false),
		)

		expected := "(00:00:00) This is a sentence.\n(00:00:02) Another sentence\n"
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
			word("A", 0, false),
			word("B", 1, false),
			word("C", 2, false),
			word("D", 3, true),
		)
		m.setCurrentTranscript(
			1,
			word("E", 5, true),
		)

		m.addFinalTranscript(
			2,
			word("1", 1, false),
			word("2", 2, true),
		)
		m.setCurrentTranscript(
			2,
			word("3", 4, false),
			word("4", 5, true),
		)

		expected := "(00:00:00) A B C D\n(00:00:01) 1 2\n(00:00:04) 3 4\n(00:00:05) E\n"
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
