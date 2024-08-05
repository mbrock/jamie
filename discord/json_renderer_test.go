package discord

import (
	"strings"
	"testing"
)

func TestRenderJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "Empty object",
			input:    map[string]interface{}{},
			expected: "{}",
		},
		{
			name:     "Empty array",
			input:    []interface{}{},
			expected: "[]",
		},
		{
			name: "Simple object",
			input: map[string]interface{}{
				"name": "John",
				"age":  30,
			},
			expected: `
age: 30
name: "John"
`,
		},
		{
			name: "Nested object",
			input: map[string]interface{}{
				"person": map[string]interface{}{
					"name": "Alice",
					"age":  25,
				},
			},
			expected: `
person: 
  age: 25
  name: "Alice"
`,
		},
		{
			name: "Array of objects",
			input: []interface{}{
				map[string]interface{}{"name": "Bob", "age": 40},
				map[string]interface{}{"name": "Charlie", "age": 35},
			},
			expected: `
0: 
  age: 40
  name: "Bob"
1: 
  age: 35
  name: "Charlie"
`,
		},
		{
			name: "Mixed types",
			input: map[string]interface{}{
				"name":    "David",
				"age":     50,
				"married": true,
				"hobbies": []interface{}{"reading", "cycling"},
				"address": map[string]interface{}{
					"city":  "New York",
					"state": "NY",
				},
			},
			expected: `
address: 
  city: "New York"
  state: "NY"
age: 50
hobbies: 
  0: "reading"
  1: "cycling"
married: true
name: "David"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RenderJSON(tt.input)
			// Trim leading and trailing whitespace from both result and expected
			result = strings.TrimSpace(result)
			expected := strings.TrimSpace(tt.expected)
			if result != expected {
				t.Errorf("RenderJSON() = %v, want %v", result, expected)
			}
		})
	}
}
