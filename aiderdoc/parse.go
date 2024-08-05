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
type InputMode int

const (
	EntryTypeCode EntryType = iota
	EntryTypeAsk
	EntryTypeRun
	EntryTypeUndo
	EntryTypeClear
	EntryTypeAdd
	EntryTypeDrop
)

const (
	InputModeCode InputMode = iota
	InputModeAsk
)

type Entry struct {
	Timestamp  time.Time
	Content    []Span
	LineNumber int
	Type       EntryType
	InputMode  InputMode
	IsVoice    bool
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
	var expectVoice bool
	currentInputMode := InputModeCode

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
			var entryType EntryType
			if currentInputMode == InputModeAsk {
				entryType = EntryTypeAsk
			} else {
				entryType = EntryTypeCode
			}
			isVoice := expectVoice
			expectVoice = false

			if strings.HasPrefix(currentContent, "/chat-mode ") {
				mode := strings.TrimPrefix(currentContent, "/chat-mode ")
				if mode == "ask" {
					currentInputMode = InputModeAsk
				} else if mode == "code" {
					currentInputMode = InputModeCode
				}
				continue // Skip this line as it's just changing the mode
			} else if strings.HasPrefix(currentContent, "/ask ") {
				currentContent = strings.TrimPrefix(currentContent, "/ask ")
				entryType = EntryTypeAsk
			} else if strings.HasPrefix(currentContent, "/run ") {
				currentContent = strings.TrimPrefix(currentContent, "/run ")
				entryType = EntryTypeRun
			} else if currentContent == "/undo" {
				currentContent = "undo"
				entryType = EntryTypeUndo
			} else if currentContent == "/clear" {
				currentContent = "clear"
				entryType = EntryTypeClear
			} else if strings.HasPrefix(currentContent, "/add ") {
				currentContent = strings.TrimPrefix(currentContent, "/add ")
				entryType = EntryTypeAdd
			} else if strings.HasPrefix(currentContent, "/drop ") {
				currentContent = strings.TrimPrefix(currentContent, "/drop ")
				entryType = EntryTypeDrop
			} else if currentContent == "/voice" {
				expectVoice = true
				continue // Skip this line and wait for the next one
			}

			processedContent := processBackticks(currentContent)
			entries = append(entries, Entry{
				Timestamp:  currentTimestamp,
				Content:    processedContent,
				LineNumber: lineNumber,
				Type:       entryType,
				InputMode:  currentInputMode,
				IsVoice:    isVoice,
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
	words := regexp.MustCompile(`[ =()]`).Split(content, -1)
	inBackticks := false

	// Regular expressions for words with underscores, camel case, and filenames
	underscoreRegex := regexp.MustCompile(`\b[a-zA-Z0-9_]+_+[a-zA-Z0-9_]+\b`)
	camelCaseRegex := regexp.MustCompile(
		`[a-z][A-Z]`,
	)
	filenameRegex := regexp.MustCompile(`^[\w/-]+\.([a-z]{1,5})$`)

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
