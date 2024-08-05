package discord

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

var (
	keyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#87CEEB"))
	stringStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA07A"))
	numberStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#98FB98"))
	booleanStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#DDA0DD"))
	nullStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#D3D3D3"))
	typeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))
)

func RenderJSON(data interface{}) string {
	return renderValue(reflect.ValueOf(data), 0)
}

func renderValue(v reflect.Value, indent int) string {
	switch v.Kind() {
	case reflect.Map:
		return renderMap(v, indent)
	case reflect.Slice, reflect.Array:
		return renderSlice(v, indent)
	case reflect.Struct:
		return renderStruct(v, indent)
	case reflect.String:
		return stringStyle.Render(fmt.Sprintf("%q", v.String()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return numberStyle.Render(fmt.Sprintf("%v", v.Interface()))
	case reflect.Bool:
		return booleanStyle.Render(fmt.Sprintf("%v", v.Bool()))
	case reflect.Interface, reflect.Ptr:
		if v.IsNil() {
			return nullStyle.Render("null")
		}
		return renderValue(v.Elem(), indent)
	default:
		return fmt.Sprintf("%v", v.Interface())
	}
}

func renderMap(v reflect.Value, indent int) string {
	if v.Len() == 0 {
		return "{}"
	}

	var sb strings.Builder
	sb.WriteString("{\n")

	keys := v.MapKeys()
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprintf("%v", keys[i].Interface()) < fmt.Sprintf("%v", keys[j].Interface())
	})

	for i, key := range keys {
		sb.WriteString(strings.Repeat("  ", indent+1))
		keyStr := fmt.Sprintf("%v", key.Interface())
		if needsQuotes(keyStr) {
			sb.WriteString(keyStyle.Render(fmt.Sprintf("%q", keyStr)))
		} else {
			sb.WriteString(keyStyle.Render(keyStr))
		}
		sb.WriteString(": ")
		sb.WriteString(renderValue(v.MapIndex(key), indent+1))
		if i < len(keys)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}

	sb.WriteString(strings.Repeat("  ", indent))
	sb.WriteString("}")
	return sb.String()
}

func needsQuotes(s string) bool {
	if s == "" {
		return true
	}
	if !unicode.IsLetter(rune(s[0])) && s[0] != '_' {
		return true
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return true
		}
	}
	return false
}

func renderSlice(v reflect.Value, indent int) string {
	if v.Len() == 0 {
		return "[]"
	}

	var sb strings.Builder
	sb.WriteString("[\n")

	for i := 0; i < v.Len(); i++ {
		sb.WriteString(strings.Repeat("  ", indent+1))
		sb.WriteString(renderValue(v.Index(i), indent+1))
		if i < v.Len()-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}

	sb.WriteString(strings.Repeat("  ", indent))
	sb.WriteString("]")
	return sb.String()
}

func renderStruct(v reflect.Value, indent int) string {
	t := v.Type()
	if t.NumField() == 0 {
		return "{}"
	}

	var sb strings.Builder
	sb.WriteString("{\n")

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue // Skip unexported fields
		}

		sb.WriteString(strings.Repeat("  ", indent+1))
		sb.WriteString(keyStyle.Render(fmt.Sprintf("%q", field.Name)))
		sb.WriteString(": ")
		sb.WriteString(renderValue(v.Field(i), indent+1))
		sb.WriteString(" ")
		sb.WriteString(typeStyle.Render(fmt.Sprintf("(%s)", field.Type)))
		if i < t.NumField()-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}

	sb.WriteString(strings.Repeat("  ", indent))
	sb.WriteString("}")
	return sb.String()
}
