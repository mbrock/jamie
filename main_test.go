package main

import (
	"testing"
)

func TestGreet(t *testing.T) {
	result := greet("node.town")
	expected := "Hello, node.town!"
	if result != expected {
		t.Errorf("greet(\"node.town\") = %q, want %q", result, expected)
	}
}
