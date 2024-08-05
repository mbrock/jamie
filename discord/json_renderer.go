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
	isFirstInList bool
}

func RenderJSON(data interface{}) string {
	state := &renderState{isFirstInList: true}
	renderJSONValue(data, state)
	return strings.TrimSpace(state.sb.String())
}

func renderJSONValue(v interface{}, state *renderState) {
	switch val := v.(type) {
	case map[string]interface{}:
		renderMap(val, state)
	case []interface{}:
		renderSlice(val, state)
	case string:
		state.sb.WriteString(stringStyle.Render(fmt.Sprintf("%q", val)))
	case float64:
		state.sb.WriteString(numberStyle.Render(strconv.FormatFloat(val, 'f', -1, 64)))
	case int:
		state.sb.WriteString(numberStyle.Render(strconv.Itoa(val)))
	case bool:
		state.sb.WriteString(booleanStyle.Render(strconv.FormatBool(val)))
	case nil:
		state.sb.WriteString(nullStyle.Render("null"))
	case json.Number:
		state.sb.WriteString(numberStyle.Render(string(val)))
	default:
		state.sb.WriteString(fmt.Sprintf("unsupported type: %T", v))
	}
}

func renderMap(m map[string]interface{}, state *renderState) {
	if len(m) == 0 {
		state.sb.WriteString("{}")
		return
	}

	if !state.isFirstInList {
		state.sb.WriteString("\n")
		writeIndent(state.indent, &state.sb)
	}

	keys := sortedKeys(m)
	for i, k := range keys {
		if i > 0 {
			state.sb.WriteString("\n")
		}
		writeIndent(state.indent, &state.sb)
		state.sb.WriteString(keyStyle.Render(k))
		state.sb.WriteString(": ")
		state.indent++
		renderJSONValue(m[k], state)
		state.isFirstInList = false
		state.indent--
	}
}

func renderSlice(s []interface{}, state *renderState) {
	if len(s) == 0 {
		state.sb.WriteString("[]")
		return
	}

	if !state.isFirstInList {
		state.sb.WriteString("\n")
		writeIndent(state.indent, &state.sb)
	}
	state.isFirstInList = true

	for i, item := range s {
		if i > 0 {
			state.sb.WriteString("\n")
		}
		writeIndent(state.indent, &state.sb)
		state.sb.WriteString(fmt.Sprintf("%d: ", i))
		state.indent++
		renderJSONValue(item, state)
		state.isFirstInList = false
		state.indent--
	}
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
