package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTextareaActionReturnsEditedValue(t *testing.T) {
	var textarea Textarea
	textarea.SetSize(80, 24)
	textarea.Open(TextareaOptions{
		Initial: "hello", LineNumbers: true,
		Actions: []Action{{ID: "save", Label: "Save", Key: "ctrl+s"}},
	})
	textarea.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	textarea.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	result, ok := textarea.TakeResult()
	if !ok || !result.Submitted || result.Cancelled || result.Action != "save" || result.Value != "hello!" {
		t.Fatalf("result = (%+v, %v)", result, ok)
	}
}

func TestTextareaEscapeCancels(t *testing.T) {
	var textarea Textarea
	textarea.SetSize(80, 24)
	textarea.Open(TextareaOptions{Initial: "text", Actions: []Action{{ID: "save", Label: "Save", Key: "ctrl+s"}}})
	textarea.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

	result, ok := textarea.TakeResult()
	if !ok || !result.Cancelled || result.Submitted {
		t.Fatalf("cancel result = (%+v, %v)", result, ok)
	}
}
