package model

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestHarpoonViewListsFiveSlots(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	b := boardWithNCols(t, 1, 1)
	b.handleKey(keyPressText("h"))

	view := b.View().Content
	if !strings.Contains(view, "Harpoon") {
		t.Fatalf("harpoon title missing:\n%s", view)
	}
	if got := strings.Count(view, "empty"); got != 5 {
		t.Fatalf("empty slots rendered = %d, want 5:\n%s", got, view)
	}
}

func TestHarpoonAssignAndJumpSelectsSavedFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	b := boardWithNCols(t, 2, 2)
	writeColItem(t, b.columns[0], "first")
	writeColItem(t, b.columns[1], "target")
	b.selectedCol = 1
	b.columns[1].SelectByName("target")

	b.handleKey(keyPressText("h"))
	if !b.harpoon.Active() {
		t.Fatal("h did not open harpoon")
	}
	b.handleKey(keyPressText("a"))
	if got := b.harpoon.SelectedPath(); !samePath(got, filepath.Join(b.columns[1].Path, "target.md")) {
		t.Fatalf("assigned path = %q, want target file", got)
	}

	b.selectedCol = 0
	b.columns[0].SelectByName("first")
	b.handleKey(keyPressText("1"))
	if b.harpoon.Active() {
		t.Fatal("jump did not close harpoon")
	}
	if b.selectedCol != 1 {
		t.Fatalf("selected column = %d, want 1", b.selectedCol)
	}
	if got := b.columns[1].SelectedItem(); got == nil || got.Name != "target" {
		t.Fatalf("selected item = %#v, want target", got)
	}
}

func TestHarpoonJumpExpandsCollapsedTargetWithoutPersisting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	b := boardWithNCols(t, 2, 2)
	writeColItem(t, b.columns[1], "target")
	b.selectedCol = 1
	b.columns[1].SelectByName("target")
	b.handleKey(keyPressText("h"))
	b.handleKey(keyPressText("a"))
	b.columns[1].ToggleCollapse()
	b.selectedCol = 0

	b.handleKey(keyPressText("1"))
	if b.columns[1].Collapsed {
		t.Fatal("harpoon jump left target column collapsed")
	}

	reopened := NewColumn(b.columns[1].Name, b.columns[1].Path, ItemOptions{})
	reopened.RestoreCollapsed()
	if !reopened.Collapsed {
		t.Fatal("harpoon jump overwrote the target column's persisted collapse preference")
	}
}

func TestHarpoonJumpMissingTargetKeepsMenuOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	b := boardWithNCols(t, 1, 1)
	b.handleKey(keyPressText("h"))
	if err := b.harpoon.SetSelected(filepath.Join(b.cfg.Path, "c0", "missing.md")); err != nil {
		t.Fatal(err)
	}

	_, cmd := b.handleKey(keyPressText("1"))
	if cmd == nil {
		t.Fatal("missing target did not report an error")
	}
	if !b.harpoon.Active() {
		t.Fatal("missing target unexpectedly closed harpoon")
	}
}
