package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestInputValidationUsesRuneLengthAndPatternHint(t *testing.T) {
	var input Input
	input.Open(InputOptions{
		Initial: "ą", Required: true, MinLength: 2, Pattern: `^ąb$`, PatternHint: "Use ą followed by b",
	})
	input.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !input.Active() || !strings.Contains(input.View(), "at least 2 characters") {
		t.Fatalf("input should remain open with a rune-length error: %q", input.View())
	}
	input.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	input.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	result, ok := input.TakeResult()
	if !ok || !result.Submitted || result.Value != "ąb" {
		t.Fatalf("TakeResult() = (%+v, %v)", result, ok)
	}
}

func TestInputPatternHint(t *testing.T) {
	var input Input
	input.Open(InputOptions{Initial: "wrong", Pattern: `^right$`, PatternHint: "Enter right"})
	input.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(input.View(), "Enter right") {
		t.Fatalf("pattern hint missing from view: %q", input.View())
	}
}

func TestInputInitialValueHonorsCharacterLimit(t *testing.T) {
	var input Input
	input.Open(InputOptions{Initial: "abcd", MaxLength: 3})
	if input.Value() != "abc" {
		t.Fatalf("initial value = %q, want %q", input.Value(), "abc")
	}
}
