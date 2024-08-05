package discord

import (
	"encoding/json"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	keyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#87CEEB"))
	stringStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA07A"))
	numberStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#98FB98"))
	booleanStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#DDA0DD"))
	nullStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#D3D3D3"))
)

func RenderJSON(data interface{}) string {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "Error rendering JSON: " + err.Error()
	}

	jsonStr := string(jsonBytes)
	var sb strings.Builder

	var inString bool
	var currentKey strings.Builder

	for i := 0; i < len(jsonStr); i++ {
		char := jsonStr[i]

		switch {
		case char == '"':
			if !inString {
				inString = true
				currentKey.Reset()
			} else {
				inString = false
				if currentKey.Len() > 0 {
					sb.WriteString(keyStyle.Render(currentKey.String()))
					currentKey.Reset()
				} else {
					sb.WriteString(stringStyle.Render(string(char)))
				}
			}
		case inString:
			if currentKey.Len() > 0 {
				currentKey.WriteByte(char)
			} else {
				sb.WriteString(stringStyle.Render(string(char)))
			}
		case char == ':':
			sb.WriteString(string(char))
			if i+1 < len(jsonStr) && jsonStr[i+1] == ' ' {
				i++ // Skip the space after the colon
			}
		case char >= '0' && char <= '9' || char == '-' || char == '.':
			sb.WriteString(numberStyle.Render(string(char)))
		case char == 't' || char == 'f':
			if strings.HasPrefix(jsonStr[i:], "true") {
				sb.WriteString(booleanStyle.Render("true"))
				i += 3
			} else if strings.HasPrefix(jsonStr[i:], "false") {
				sb.WriteString(booleanStyle.Render("false"))
				i += 4
			}
		case char == 'n':
			if strings.HasPrefix(jsonStr[i:], "null") {
				sb.WriteString(nullStyle.Render("null"))
				i += 3
			}
		default:
			sb.WriteByte(char)
		}
	}

	return sb.String()
}
