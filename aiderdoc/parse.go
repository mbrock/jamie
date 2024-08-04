package aiderdoc

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

type EntryType int

const (
	EntryTypeNormal EntryType = iota
	EntryTypeAsk
	EntryTypeRun
	EntryTypeUndo
	EntryTypeClear
)

type Entry struct {
	Timestamp  time.Time
	Content    []Span
	LineNumber int
	Type       EntryType
}

type Span struct {
	Text   string
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
				Type:       EntryTypeNormal,
			})

		case strings.HasPrefix(line, "/ask ") || strings.HasPrefix(line, "/run "):
			// Ask or Run command
			entryType := EntryTypeAsk
			if strings.HasPrefix(line, "/run ") {
				entryType = EntryTypeRun
			}
			currentContent = strings.TrimSpace(strings.TrimPrefix(line, "/ask "))
			currentContent = strings.TrimSpace(strings.TrimPrefix(currentContent, "/run "))
			processedContent := processBackticks(currentContent)
			entries = append(entries, Entry{
				Timestamp:  currentTimestamp,
				Content:    processedContent,
				LineNumber: lineNumber,
				Type:       entryType,
			})

		case line == "/undo" || line == "/clear":
			// Undo or Clear command
			entryType := EntryTypeUndo
			if line == "/clear" {
				entryType = EntryTypeClear
			}
			entries = append(entries, Entry{
				Timestamp:  currentTimestamp,
				Content:    []Span{{Text: line, IsCode: false}},
				LineNumber: lineNumber,
				Type:       entryType,
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
	words := regexp.MustCompile(`[ =]`).Split(content, -1)
	inBackticks := false

	// Regular expressions for words with underscores, camel case, and filenames
	underscoreRegex := regexp.MustCompile(`\b[a-zA-Z0-9_]+_+[a-zA-Z0-9_]+\b`)
	camelCaseRegex := regexp.MustCompile(
		`[a-z][A-Z]`,
	)
	filenameRegex := regexp.MustCompile(`^[\w-]+\.([a-z]{1,5})$`)

	for _, word := range words {
		if strings.Contains(word, "`") {
			parts := strings.Split(word, "`")
			for i, part := range parts {
				if part != "" {
					spans = append(
						spans,
						Span{Text: part, IsCode: inBackticks},
					)
				}
				if i < len(parts)-1 {
					inBackticks = !inBackticks
				}
			}
		} else if inBackticks || underscoreRegex.MatchString(word) || camelCaseRegex.MatchString(word) || filenameRegex.MatchString(word) {
			spans = append(spans, Span{Text: word, IsCode: true})
		} else {
			spans = append(spans, Span{Text: word, IsCode: false})
		}

		// Add a space after each word, except for the last one
		if len(spans) > 0 && spans[len(spans)-1].Text != "" {
			spans = append(spans, Span{Text: " ", IsCode: false})
		}
	}

	// Remove the trailing space if it exists
	if len(spans) > 0 && spans[len(spans)-1].Text == " " {
		spans = spans[:len(spans)-1]
	}

	return spans
}
