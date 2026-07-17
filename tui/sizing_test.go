package tui

import (
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func TestSizeSetAndFit(t *testing.T) {
	var size Size
	size.Set(-1, 40)
	if size != (Size{Width: 0, Height: 40}) {
		t.Fatalf("Set = %+v", size)
	}
	size.Set(120, 40)
	if got := size.Fit(80, 0); got != (Size{Width: 80, Height: 40}) {
		t.Fatalf("Fit = %+v", got)
	}
}

func TestDefaultKeyMapConventions(t *testing.T) {
	keys := DefaultKeyMap()
	if !key.Matches(tea.KeyPressMsg{Code: tea.KeyEsc}, keys.Cancel) {
		t.Fatal("escape must cancel")
	}
	if !key.Matches(tea.KeyPressMsg{Code: tea.KeyEnter}, keys.Submit) {
		t.Fatal("enter must submit")
	}
}
