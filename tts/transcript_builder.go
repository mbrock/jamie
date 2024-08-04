package tts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type Span struct {
	Content string
	Style   lipgloss.Style
}

type Line struct {
	Spans     []Span
	StartTime time.Time
}

type TranscriptBuilder struct {
	lines            []Line
	currentLine      []Span
	currentStartTime time.Time
	lastWasEOS       bool
}

func NewTranscriptBuilder() *TranscriptBuilder {
	return &TranscriptBuilder{
		lines:       []Line{},
		currentLine: []Span{},
	}
}

func (tb *TranscriptBuilder) WriteWord(word TranscriptWord, isPartial bool) {
	if !tb.lastWasEOS && word.AttachesTo != "previous" {
		tb.currentLine = append(
			tb.currentLine,
			Span{Content: " ", Style: lipgloss.NewStyle()},
		)
	}

	style := lipgloss.NewStyle()
	if isPartial {
		style = style.Foreground(lipgloss.Color("240"))
	} else {
		style = style.Foreground(getConfidenceColor(word.Confidence))
	}

	tb.currentLine = append(
		tb.currentLine,
		Span{Content: word.Content, Style: style},
	)

	tb.lastWasEOS = word.IsEOS

	if tb.currentStartTime.IsZero() {
		tb.currentStartTime = word.AbsoluteStartTime
	}

	if word.IsEOS {
		tb.lines = append(tb.lines, Line{
			Spans:     tb.currentLine,
			StartTime: tb.currentStartTime,
		})

		tb.currentLine = []Span{}
		tb.currentStartTime = time.Time{}
	}
}

func (tb *TranscriptBuilder) AppendWords(
	words []TranscriptWord,
	isPartial bool,
) {
	for _, word := range words {
		tb.WriteWord(word, isPartial)
	}
}

func (tb *TranscriptBuilder) GetLines() []Line {
	lines := tb.lines
	if len(tb.currentLine) > 0 {
		lines = append(lines, Line{
			Spans:     tb.currentLine,
			StartTime: tb.currentStartTime,
		})
	}
	return lines
}

func (tb *TranscriptBuilder) RenderLines() string {
	var result strings.Builder
	for _, line := range tb.GetLines() {
		result.WriteString(
			fmt.Sprintf("(%s) ", line.StartTime.Format("15:04:05")),
		)
		for _, span := range line.Spans {
			result.WriteString(span.Style.Render(span.Content))
		}
		result.WriteString("\n")
	}
	return result.String()
}

func (tb *TranscriptBuilder) RenderHTML() (string, error) {
	component := TranscriptTemplate(tb.GetLines())
	var buf strings.Builder
	err := component.Render(context.Background(), &buf)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func getConfidenceColor(confidence float64) lipgloss.Color {
	switch {
	case confidence >= 0.9:
		return lipgloss.Color("#FFFFFF")
	case confidence >= 0.8:
		return lipgloss.Color("#FFFF00")
	default:
		return lipgloss.Color("#FF0000")
	}
}
