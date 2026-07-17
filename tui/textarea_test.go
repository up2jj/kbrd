package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTextareaActionReturnsUTF8CursorAndSelectionOffsets(t *testing.T) {
	var textarea Textarea
	textarea.SetSize(80, 24)
	textarea.Open(TextareaOptions{
		Initial: "aą\n猫x", Wrap: true, LineNumbers: true,
		Actions: []Action{{ID: "promote", Label: "Promote", Key: "ctrl+enter", RequiresSelection: true}},
	})
	for _, msg := range []tea.KeyPressMsg{
		{Code: '[', Mod: tea.ModCtrl}, // leave insert mode without cancelling
		{Code: 'l'},
		{Code: 'v'},
		{Code: 'j'},
	} {
		textarea.Update(msg)
	}
	textarea.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})

	result, ok := textarea.TakeResult()
	if !ok || !result.Submitted || result.Action != "promote" {
		t.Fatalf("result = (%+v, %v)", result, ok)
	}
	if result.Value != "aą\n猫x" || result.Cursor != (TextareaCursor{Line: 2, Column: 2, Offset: 7}) {
		t.Fatalf("value/cursor = %q / %+v", result.Value, result.Cursor)
	}
	if result.Selection == nil || *result.Selection != (TextareaSelection{StartOffset: 1, EndOffset: 8, Text: "ą\n猫x"}) {
		t.Fatalf("selection = %+v", result.Selection)
	}
}

func TestTextareaRequiresSelectionAndEscapeCancels(t *testing.T) {
	var textarea Textarea
	textarea.SetSize(80, 24)
	textarea.Open(TextareaOptions{Initial: "text", Wrap: true, Actions: []Action{{ID: "promote", Label: "Promote", Key: "ctrl+enter", RequiresSelection: true}}})
	textarea.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	if _, ok := textarea.TakeResult(); ok {
		t.Fatal("action without selection completed")
	}
	textarea.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	result, ok := textarea.TakeResult()
	if !ok || !result.Cancelled || result.Submitted {
		t.Fatalf("cancel result = (%+v, %v)", result, ok)
	}
}
