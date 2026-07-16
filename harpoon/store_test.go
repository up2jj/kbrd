package harpoon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStore_SaveLoadAndBoardScoping(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	boardA := filepath.Join(t.TempDir(), "a")
	boardB := filepath.Join(t.TempDir(), "b")
	file := filepath.Join(boardA, "TODO", "card.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("card"), 0o644); err != nil {
		t.Fatal(err)
	}

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

func TestStore_ReconcileMovedPath(t *testing.T) {
	board := t.TempDir()
	oldPath := filepath.Join(board, "card.md")
	newPath := filepath.Join(board, "moved.md")
	if err := os.WriteFile(oldPath, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := Store{}
	if err := store.Set(board, 0, oldPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !store.Reconcile(board, []string{newPath}) {
		t.Fatal("Reconcile reported no change")
	}
	if got := store.ForBoard(board)[0]; got != newPath {
		t.Fatalf("slot = %q, want %q", got, newPath)
	}
}

func TestStore_ReconcileDoesNotRetargetDeletedFileByContent(t *testing.T) {
	board := t.TempDir()
	tracked := filepath.Join(board, "tracked.md")
	other := filepath.Join(board, "other.md")
	for _, path := range []string{tracked, other} {
		if err := os.WriteFile(path, []byte("same"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store := Store{}
	if err := store.Set(board, 0, tracked); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tracked); err != nil {
		t.Fatal(err)
	}

	if store.Reconcile(board, []string{other}) {
		t.Fatal("Reconcile retargeted a deleted file")
	}
	if got := store.ForBoard(board)[0]; got != tracked {
		t.Fatalf("slot = %q, want stale path %q", got, tracked)
	}
}

func TestStore_ReconcileBackfillsLegacyIdentity(t *testing.T) {
	board := t.TempDir()
	path := filepath.Join(board, "card.md")
	if err := os.WriteFile(path, []byte("card"), 0o644); err != nil {
		t.Fatal(err)
	}
	var slots Slots
	slots[0] = path
	store := Store{Boards: map[string]Slots{normalize(board): slots}}

	if !store.Reconcile(board, []string{path}) {
		t.Fatal("Reconcile did not backfill legacy identity")
	}
	if got := store.Identities[normalize(board)][0]; got == "" {
		t.Fatal("legacy slot identity is still empty")
	}
}
