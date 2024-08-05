package discord

import (
	"encoding/json"
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
	err := renderJSONValue(data, 0, &sb)
	if err != nil {
		return fmt.Sprintf("Error rendering JSON: %v", err)
	}
	return sb.String()
}

func renderJSONValue(v interface{}, indent int, sb *strings.Builder) error {
	switch val := v.(type) {
	case map[string]interface{}:
		return renderMap(val, indent, sb)
	case []interface{}:
		return renderSlice(val, indent, sb)
	case string:
		sb.WriteString(stringStyle.Render(fmt.Sprintf("%q", val)))
	case float64:
		sb.WriteString(numberStyle.Render(strconv.FormatFloat(val, 'f', -1, 64)))
	case bool:
		sb.WriteString(booleanStyle.Render(strconv.FormatBool(val)))
	case nil:
		sb.WriteString(nullStyle.Render("null"))
	case json.Number:
		sb.WriteString(numberStyle.Render(string(val)))
	default:
		return fmt.Errorf("unsupported type: %T", v)
	}
	return nil
}

func renderMap(m map[string]interface{}, indent int, sb *strings.Builder) error {
	if len(m) == 0 {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("{}"))
		return nil
	}

	sb.WriteString("{\n")
	keys := sortedKeys(m)
	for i, k := range keys {
		writeIndent(indent+1, sb)
		sb.WriteString(keyStyle.Render(fmt.Sprintf("%q", k)))
		sb.WriteString(": ")
		if err := renderJSONValue(m[k], indent+1, sb); err != nil {
			return err
		}
		if i < len(keys)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	writeIndent(indent, sb)
	sb.WriteString("}")
	return nil
}

func renderSlice(s []interface{}, indent int, sb *strings.Builder) error {
	if len(s) == 0 {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("[]"))
		return nil
	}

	sb.WriteString("[\n")
	for i, item := range s {
		writeIndent(indent+1, sb)
		if err := renderJSONValue(item, indent+1, sb); err != nil {
			return err
		}
		if i < len(s)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	writeIndent(indent, sb)
	sb.WriteString("]")
	return nil
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeIndent(n int, sb *strings.Builder) {
	sb.WriteString(strings.Repeat("  ", n))
}
