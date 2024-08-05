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

type renderState struct {
	sb            strings.Builder
	indent        int
	needsNewline  bool
	isFirstInList bool
}

func RenderJSON(data interface{}) string {
	state := &renderState{isFirstInList: true}
	err := renderJSONValue(data, state)
	if err != nil {
		return fmt.Sprintf("Error rendering JSON: %v", err)
	}
	return state.sb.String()
}

func renderJSONValue(v interface{}, state *renderState) error {
	if state.needsNewline {
		state.sb.WriteString("\n")
		writeIndent(state.indent, &state.sb)
		state.needsNewline = false
	}

	switch val := v.(type) {
	case map[string]interface{}:
		return renderMap(val, state)
	case []interface{}:
		return renderSlice(val, state)
	case string:
		state.sb.WriteString(stringStyle.Render(fmt.Sprintf("%q", val)))
	case float64:
		state.sb.WriteString(numberStyle.Render(strconv.FormatFloat(val, 'f', -1, 64)))
	case bool:
		state.sb.WriteString(booleanStyle.Render(strconv.FormatBool(val)))
	case nil:
		state.sb.WriteString(nullStyle.Render("null"))
	case json.Number:
		state.sb.WriteString(numberStyle.Render(string(val)))
	default:
		return fmt.Errorf("unsupported type: %T", v)
	}
	return nil
}

func renderMap(m map[string]interface{}, state *renderState) error {
	if len(m) == 0 {
		state.sb.WriteString(lipgloss.NewStyle().Faint(true).Render("empty object"))
		return nil
	}

	keys := sortedKeys(m)
	state.indent++
	for i, k := range keys {
		if i > 0 {
			state.sb.WriteString("\n")
			writeIndent(state.indent, &state.sb)
		}
		state.sb.WriteString(keyStyle.Render(fmt.Sprintf("%q", k)))
		state.sb.WriteString(": ")
		if err := renderJSONValue(m[k], state); err != nil {
			return err
		}
	}
	state.indent--
	state.needsNewline = true
	return nil
}

func renderSlice(s []interface{}, state *renderState) error {
	if len(s) == 0 {
		state.sb.WriteString(lipgloss.NewStyle().Faint(true).Render("empty array"))
		return nil
	}

	state.indent++
	for i, item := range s {
		if i > 0 {
			state.sb.WriteString("\n")
			writeIndent(state.indent, &state.sb)
		}
		state.isFirstInList = i == 0
		if err := renderJSONValue(item, state); err != nil {
			return err
		}
	}
	state.indent--
	state.needsNewline = true
	state.isFirstInList = false
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
