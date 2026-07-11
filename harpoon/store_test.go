package harpoon

import (
	"path/filepath"
	"testing"
)

func TestStore_SaveLoadAndBoardScoping(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	boardA := filepath.Join(t.TempDir(), "a")
	boardB := filepath.Join(t.TempDir(), "b")
	file := filepath.Join(boardA, "TODO", "card.md")

	store, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(boardA, 2, file); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.ForBoard(boardA)[2]; got != file {
		t.Fatalf("slot = %q, want %q", got, file)
	}
	if got := reloaded.ForBoard(boardB)[2]; got != "" {
		t.Fatalf("other board slot = %q, want empty", got)
	}
}

func TestStore_SetRejectsInvalidSlot(t *testing.T) {
	store := Store{}
	if err := store.Set("/board", SlotCount, "/board/a.md"); err == nil {
		t.Fatal("Set accepted an out-of-range slot")
	}
}
