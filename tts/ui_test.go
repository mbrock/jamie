package tts

import (
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type testModel model

func newTestModel() testModel {
	return testModel{
		sessions: make(map[int64]*SessionTranscript),
	}
}

func TestTranscriptBuilderHTML(t *testing.T) {
	builder := NewTranscriptBuilder()
	builder.WriteWord(word("Hello", 0, false), false)
	builder.WriteWord(word("world", 1, true), false)
	builder.WriteWord(word("How", 2, false), true)

	html, err := builder.RenderHTML()
	if err != nil {
		t.Fatalf("Failed to render HTML: %v", err)
	}

	expectedHTML := `<div class="transcript"><div class="line"><span class="timestamp">(00:00:00)</span><span style="color:#FFFFFF">Hello</span><span> </span><span style="color:#FFFFFF">world</span></div><div class="line"><span class="timestamp">(00:00:02)</span><span style="color:#808080">How</span></div></div>`

	if html != expectedHTML {
		t.Errorf("HTML rendering doesn't match expected output.\nExpected:\n%s\nGot:\n%s", expectedHTML, html)
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

func TestTranscriptBuilder(t *testing.T) {
	t.Run("Basic Functionality", func(t *testing.T) {
		builder := NewTranscriptBuilder()
		builder.WriteWord(word("Hello", 0, false), false)
		builder.WriteWord(word("world", 1, true), false)
		builder.WriteWord(word("How", 2, false), true)

		lines := builder.GetLines()
		if len(lines) != 2 {
			t.Errorf("Expected 2 lines, got %d", len(lines))
		}

		if len(lines[0].Spans) != 2 {
			t.Errorf("Expected 2 spans in first line, got %d", len(lines[0].Spans))
		}

		if lines[0].Spans[0].Content != "Hello" {
			t.Errorf("Expected 'Hello', got '%s'", lines[0].Spans[0].Content)
		}

		if lines[0].Spans[1].Content != " world" {
			t.Errorf("Expected ' world', got '%s'", lines[0].Spans[1].Content)
		}

		if len(lines[1].Spans) != 1 {
			t.Errorf("Expected 1 span in second line, got %d", len(lines[1].Spans))
		}

		if lines[1].Spans[0].Content != "How" {
			t.Errorf("Expected 'How', got '%s'", lines[1].Spans[0].Content)
		}

		if lines[1].Spans[0].Style.GetForeground() != lipgloss.Color("240") {
			t.Errorf("Expected partial word color, got %v", lines[1].Spans[0].Style.GetForeground())
		}
	})

	t.Run("Att aches To", func(t *testing.T) {
		builder := NewTranscriptBuilder()
		builder.WriteWord(word("Hello", 0, false), false)
		builder.WriteWord(TranscriptWord{
			Content:    ".",
			Confidence: 1.0,
			IsEOS:      true,
			AttachesTo: "previous",
		}, false)

		lines := builder.GetLines()
		if len(lines) != 1 {
			t.Errorf("Expected 1 line, got %d", len(lines))
		}

		if len(lines[0].Spans) != 2 {
			t.Errorf("Expected 2 spans, got %d", len(lines[0].Spans))
		}

		if lines[0].Spans[0].Content != "Hello" {
			t.Errorf("Expected 'Hello', got '%s'", lines[0].Spans[0].Content)
		}

		if lines[0].Spans[1].Content != "." {
			t.Errorf("Expected '.', got '%s'", lines[0].Spans[1].Content)
		}
	})
}
