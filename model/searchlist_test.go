package model

import "testing"

func TestFuzzyListQueryAndSelection(t *testing.T) {
	items := []string{"alpha", "bravo", "charlie"}
	haystack := func(i int) string { return items[i] }

	var list fuzzyList
	list.Reset(len(items), 2, haystack)
	if index, ok := list.SelectedIndex(); !ok || index != 2 {
		t.Fatalf("initial selection = (%d, %t), want (2, true)", index, ok)
	}

	list.Move(10)
	if index, _ := list.SelectedIndex(); index != 2 {
		t.Fatalf("selection after move = %d, want 2", index)
	}

	list.Append("br")
	if list.filter != "br" {
		t.Fatalf("filter = %q, want br", list.filter)
	}
	if index, ok := list.SelectedIndex(); !ok || index != 1 {
		t.Fatalf("filtered selection = (%d, %t), want (1, true)", index, ok)
	}

	if !list.Backspace() || list.filter != "b" {
		t.Fatalf("first backspace = %q, want b", list.filter)
	}
	if !list.Backspace() || list.filter != "" {
		t.Fatalf("second backspace = %q, want empty", list.filter)
	}
	if list.Backspace() {
		t.Fatal("empty query backspace should not be consumed")
	}
}
