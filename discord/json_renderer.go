package discord

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	keyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#87CEEB"))
	valueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#98FB98"))
	braceStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA07A"))
)

func RenderJSON(data interface{}) string {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error marshaling JSON: %v", err)
	}

	lines := strings.Split(string(jsonBytes), "\n")
	var renderedLines []string

	for _, line := range lines {
		renderedLine := renderJSONLine(line)
		renderedLines = append(renderedLines, renderedLine)
	}

	return strings.Join(renderedLines, "\n")
}

func renderJSONLine(line string) string {
	parts := strings.SplitN(line, ":", 2)

	if len(parts) == 2 {
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes from key
		key = strings.Trim(key, "\"")

		renderedKey := keyStyle.Render(key)
		renderedValue := renderJSONValue(value)

		return fmt.Sprintf("%s%s: %s", strings.Repeat("  ", countLeadingSpaces(line)/2), renderedKey, renderedValue)
	}

	return renderJSONValue(strings.TrimSpace(line))
}

func renderJSONValue(value string) string {
	if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
		return value // Don't apply braceStyle to objects and arrays
	}
	return valueStyle.Render(strings.Trim(value, ",")) // Remove trailing comma
}

func countLeadingSpaces(s string) int {
	return len(s) - len(strings.TrimLeft(s, " "))
}
