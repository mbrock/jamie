package discord

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	keyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#87CEEB"))
	valueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#98FB98"))
)

func RenderJSON(data interface{}) string {
	var sb strings.Builder
	renderJSONValue(data, 0, &sb)
	return sb.String()
}

func renderJSONValue(v interface{}, indent int, sb *strings.Builder) {
	switch val := v.(type) {
	case map[string]interface{}:
		sb.WriteString("\n")
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			writeIndent(indent+1, sb)
			sb.WriteString(keyStyle.Render(k))
			sb.WriteString(": ")
			renderJSONValue(val[k], indent+1, sb)
			sb.WriteString("\n")
		}
		writeIndent(indent, sb)
	case []interface{}:
		sb.WriteString("\n")
		for i, item := range val {
			writeIndent(indent+1, sb)
			sb.WriteString(fmt.Sprintf("%d: ", i))
			renderJSONValue(item, indent+1, sb)
			sb.WriteString("\n")
		}
		writeIndent(indent, sb)
	case string:
		sb.WriteString(valueStyle.Render(fmt.Sprintf("%q", val)))
	case float64:
		sb.WriteString(valueStyle.Render(strconv.FormatFloat(val, 'f', -1, 64)))
	case bool:
		sb.WriteString(valueStyle.Render(strconv.FormatBool(val)))
	case nil:
		sb.WriteString(valueStyle.Render("null"))
	default:
		sb.WriteString(valueStyle.Render(fmt.Sprintf("%v", val)))
	}
}

func writeIndent(n int, sb *strings.Builder) {
	sb.WriteString(strings.Repeat("  ", n))
}
