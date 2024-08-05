package discord

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	keyStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#87CEEB"))
	stringStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA07A"))
	numberStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#98FB98"))
	booleanStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#DDA0DD"))
	nullStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#D3D3D3"))
	structureStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))
)

func RenderJSON(data interface{}) string {
	var sb strings.Builder
	renderJSONValue(data, 0, &sb)
	return sb.String()
}

func renderJSONValue(v interface{}, indent int, sb *strings.Builder) {
	switch val := v.(type) {
	case map[string]interface{}:
		if len(val) == 0 {
			sb.WriteString(lipgloss.NewStyle().Faint(true).Render("{}"))
		} else {
			sb.WriteString("\n")
			keys := make([]string, 0, len(val))
			for k := range val {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				writeIndent(indent, sb)
				sb.WriteString(keyStyle.Render(k))
				sb.WriteString(": ")
				renderJSONValue(val[k], indent+1, sb)
				sb.WriteString("\n")
			}
			writeIndent(indent-1, sb)
		}
	case []interface{}:
		if len(val) == 0 {
			sb.WriteString(lipgloss.NewStyle().Faint(true).Render("[]"))
		} else {
			sb.WriteString("\n")
			for i, item := range val {
				writeIndent(indent, sb)
				sb.WriteString(fmt.Sprintf("%d: ", i))
				renderJSONValue(item, indent+1, sb)
				sb.WriteString("\n")
			}
			writeIndent(indent-1, sb)
		}
	case string:
		sb.WriteString(stringStyle.Render(fmt.Sprintf("%q", val)))
	case float64:
		sb.WriteString(numberStyle.Render(strconv.FormatFloat(val, 'f', -1, 64)))
	case bool:
		sb.WriteString(booleanStyle.Render(strconv.FormatBool(val)))
	case nil:
		sb.WriteString(nullStyle.Render("null"))
	default:
		sb.WriteString(structureStyle.Render(fmt.Sprintf("%v", val)))
	}
}

func writeIndent(n int, sb *strings.Builder) {
	sb.WriteString(strings.Repeat("  ", n))
}
