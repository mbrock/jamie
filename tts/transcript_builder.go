package tts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

type SpanStyle int

const (
	StyleNormal SpanStyle = iota
	StylePartial
	StyleHighConfidence
	StyleMediumConfidence
	StyleLowConfidence
)

func (s SpanStyle) Render(content string) string {
	return content // For now, just return the content without styling
}

type Span struct {
	Content string
	Style   SpanStyle
}

type Line struct {
	Spans     []Span
	StartTime time.Time
	EndTime   time.Time
	SessionID int64
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
	style := StyleNormal
	if isPartial {
		style = StylePartial
	} else {
		style = getConfidenceStyle(word.Confidence)
	}

	content := word.Content
	if !tb.lastWasEOS && word.AttachesTo != "previous" &&
		len(tb.currentLine) > 0 {
		content = " " + content
	}

	tb.currentLine = append(
		tb.currentLine,
		Span{Content: content, Style: style},
	)

	tb.lastWasEOS = word.IsEOS

	if tb.currentStartTime.IsZero() {
		tb.currentStartTime = word.AbsoluteStartTime
	}

	if word.IsEOS || tb.currentStartTime.IsZero() {
		tb.lines = append(tb.lines, Line{
			Spans:     tb.currentLine,
			StartTime: tb.currentStartTime,
			EndTime: tb.currentStartTime.Add(
				time.Duration(word.RelativeEndTime * float64(time.Second)),
			),
			SessionID: word.SessionID,
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
		lastLine := Line{
			Spans:     tb.currentLine,
			StartTime: tb.currentStartTime,
		}
		if len(lines) > 0 {
			lastLine.SessionID = lines[len(lines)-1].SessionID
		}
		lines = append(lines, lastLine)
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
	lines := tb.GetLines()
	// sort.Slice(lines, func(i, j int) bool {
	// 	return lines[i].StartTime.Before(lines[j].StartTime)
	// })
	for _, line := range lines {
		log.Info("line", "start", line.StartTime, "text", line.Spans)
	}
	var buf strings.Builder
	err := TranscriptTemplate(lines).Render(context.Background(), &buf)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func getConfidenceStyle(confidence float64) SpanStyle {
	switch {
	case confidence >= 0.9:
		return StyleHighConfidence
	case confidence >= 0.8:
		return StyleMediumConfidence
	default:
		return StyleLowConfidence
	}
}
