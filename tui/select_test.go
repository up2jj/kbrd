package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSelectInitialDisabledNavigationAndStableID(t *testing.T) {
	var selectOne Select
	selectOne.Open(SelectOptions{
		InitialID: "blocked",
		Items: []SelectItem{
			{ID: "one", Label: "One"},
			{ID: "blocked", Label: "Blocked", Disabled: true, DisabledReason: "Unavailable"},
			{ID: "three", Label: "Three"},
		},
	})
	selectOne.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !selectOne.Active() || !strings.Contains(selectOne.View(), "Unavailable") {
		t.Fatalf("disabled item should explain why it cannot submit: %q", selectOne.View())
	}
	selectOne.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	selectOne.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	result, ok := selectOne.TakeResult()
	if !ok || result.ID != "three" || !result.Submitted {
		t.Fatalf("TakeResult() = (%+v, %v)", result, ok)
	}
}

func TestSelectFuzzySearchAndBoundedScrolling(t *testing.T) {
	items := make([]SelectItem, 20)
	for index := range items {
		items[index] = SelectItem{ID: strconv.Itoa(index), Label: "item " + strconv.Itoa(index)}
	}
	items[17].Label = "special target"
	var selectOne Select
	selectOne.SetSize(60, 14)
	selectOne.Open(SelectOptions{Searchable: true, Items: items})
	for _, r := range "target" {
		selectOne.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if selectOne.SelectedIndex() != 17 {
		t.Fatalf("search selected index = %d, want 17", selectOne.SelectedIndex())
	}
	if strings.Contains(selectOne.View(), "item 0") {
		t.Fatalf("filtered view contains unrelated item: %q", selectOne.View())
	}
}

func TestSelectSearchAcceptsVimNavigationLetters(t *testing.T) {
	var selectOne Select
	selectOne.Open(SelectOptions{
		Searchable: true,
		Items: []SelectItem{
			{ID: "project", Label: "Project journal"},
			{ID: "inbox", Label: "Inbox"},
		},
	})
	for _, r := range "project" {
		selectOne.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	selectOne.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	result, ok := selectOne.TakeResult()
	if !ok || result.ID != "project" || !result.Submitted {
		t.Fatalf("search result = (%+v, %v)", result, ok)
	}
}

func TestSelectEmptySubmitCancels(t *testing.T) {
	var selectOne Select
	selectOne.Open(SelectOptions{})
	selectOne.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	result, ok := selectOne.TakeResult()
	if !ok || !result.Cancelled || result.Submitted {
		t.Fatalf("empty submit result = (%+v, %v)", result, ok)
	}
}

func TestActionsShortcutAndConfirmSafeDefault(t *testing.T) {
	var actions Actions
	actions.Open(ActionsOptions{Actions: []Action{{ID: "save", Label: "Save", Key: "ctrl+s"}}})
	actions.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	result, ok := actions.TakeResult()
	if !ok || result.ID != "save" || !result.Submitted {
		t.Fatalf("action result = (%+v, %v)", result, ok)
	}

	var confirm Confirm
	confirm.Open(ConfirmOptions{})
	confirm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	confirmed, ok := confirm.TakeResult()
	if !ok || !confirmed.Submitted || confirmed.Value {
		t.Fatalf("safe default should submit false: (%+v, %v)", confirmed, ok)
	}
}

func TestConfirmNavigationFollowsButtonOrder(t *testing.T) {
	var confirm Confirm
	confirm.Open(ConfirmOptions{Default: true})
	confirm.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	confirm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	left, ok := confirm.TakeResult()
	if !ok || !left.Submitted || !left.Value {
		t.Fatalf("left result = (%+v, %v), want confirmation", left, ok)
	}

	confirm.Open(ConfirmOptions{Default: true})
	confirm.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	confirm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	right, ok := confirm.TakeResult()
	if !ok || !right.Submitted || right.Value {
		t.Fatalf("right result = (%+v, %v), want rejection", right, ok)
	}
}
