package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMultiSelectToggleSubmitAndStableOrder(t *testing.T) {
	var control MultiSelect
	control.SetSize(80, 24)
	control.Open(MultiSelectOptions{
		Title:      "Areas",
		Items:      []SelectItem{{ID: "ui", Label: "UI"}, {ID: "data", Label: "Data"}, {ID: "ops", Label: "Ops"}},
		InitialIDs: []string{"ops"},
	})
	control.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	control.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	control.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	control.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	result, ok := control.TakeResult()
	if !ok || !result.Submitted || !slices.Equal(result.IDs, []string{"ui", "data", "ops"}) {
		t.Fatalf("result = (%+v, %v)", result, ok)
	}
}

func TestMultiSelectDisabledAndSearch(t *testing.T) {
	var control MultiSelect
	control.SetSize(50, 16)
	control.Open(MultiSelectOptions{
		Searchable: true,
		Items: []SelectItem{
			{ID: "todo", Label: "Todo"},
			{ID: "done", Label: "Done", Disabled: true, DisabledReason: "Archived"},
		},
	})
	control.Update(tea.KeyPressMsg{Text: "done"})
	control.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if got := control.SelectedIDs(); len(got) != 0 {
		t.Fatalf("disabled item selected: %v", got)
	}
	if view := control.View(); !strings.Contains(view, "Archived") {
		t.Fatalf("disabled reason missing from view: %q", view)
	}
}

func TestMultiSelectEscapeCancels(t *testing.T) {
	var control MultiSelect
	control.Open(MultiSelectOptions{Items: []SelectItem{{ID: "x", Label: "X"}}})
	control.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	result, ok := control.TakeResult()
	if !ok || !result.Cancelled || result.Submitted {
		t.Fatalf("result = (%+v, %v)", result, ok)
	}
}
