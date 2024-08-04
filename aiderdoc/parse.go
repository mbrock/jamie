package aiderdoc

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

type Entry struct {
	Timestamp  time.Time
	Content    []Span
	LineNumber int
}

type Span struct {
	Text  string
	IsCode bool
}

func ParseFile(filename string) ([]Entry, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("error opening file %s: %w", filename, err)
	}
	defer file.Close()

	var entries []Entry
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	var currentTimestamp time.Time
	var currentContent string

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "# "):
			// Timestamp line
			timestamp, err := time.Parse(
				"2006-01-02 15:04:05.999999",
				strings.TrimPrefix(line, "# "),
			)
			if err != nil {
				return nil, fmt.Errorf(
					"error parsing timestamp at line %d: %w",
					lineNumber,
					err,
				)
			}
			currentTimestamp = timestamp

		case strings.HasPrefix(line, "+"):
			// Content line
			currentContent = strings.TrimPrefix(line, "+")
			processedContent := processBackticks(currentContent)
			entries = append(entries, Entry{
				Timestamp:  currentTimestamp,
				Content:    processedContent,
				LineNumber: lineNumber,
			})

		case strings.TrimSpace(line) == "":
			// Empty line, ignore

		default:
			return nil, fmt.Errorf(
				"unexpected line format at line %d: %s",
				lineNumber,
				line,
			)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	return entries, nil
}

func processBackticks(content string) []Span {
	var spans []Span
	var currentSpan strings.Builder
	inBackticks := false

	for _, char := range content {
		if char == '`' {
			if currentSpan.Len() > 0 {
				spans = append(spans, Span{Text: currentSpan.String(), IsCode: inBackticks})
				currentSpan.Reset()
			}
			inBackticks = !inBackticks
		} else {
			currentSpan.WriteRune(char)
		}
	}

	if currentSpan.Len() > 0 {
		spans = append(spans, Span{Text: currentSpan.String(), IsCode: inBackticks})
	}

	return spans
}
