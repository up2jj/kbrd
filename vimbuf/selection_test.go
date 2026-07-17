package vimbuf

import "testing"

func TestSelectionNormalizesMultibyteCharacterwiseRange(t *testing.T) {
	b := New("aż\n猫x")
	b.anchor = Pos{Row: 1, Col: 0}
	b.cursor = Pos{Row: 0, Col: 1}
	b.mode = ModeVisual

	selection, ok := b.Selection()
	if !ok {
		t.Fatal("Selection returned inactive")
	}
	if selection.Start != (Pos{Row: 0, Col: 1}) || selection.End != (Pos{Row: 1, Col: 1}) {
		t.Fatalf("positions = %+v..%+v", selection.Start, selection.End)
	}
	if selection.Text != "ż\n猫" {
		t.Fatalf("text = %q", selection.Text)
	}
}

func TestSelectionLinewiseDoesNotInventTrailingNewline(t *testing.T) {
	b := New("one\ntwo")
	b.anchor = Pos{Row: 0}
	b.cursor = Pos{Row: 1}
	b.mode = ModeVisualLine

	selection, ok := b.Selection()
	if !ok || selection.Text != "one\ntwo" {
		t.Fatalf("selection = (%+v, %v)", selection, ok)
	}
}
