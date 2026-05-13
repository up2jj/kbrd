package recents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTouch_DedupesAndMovesToFront(t *testing.T) {
	s := Store{}
	s.Touch("/a", "A")
	s.Touch("/b", "B")
	s.Touch("/a", "A2")

	if len(s.Entries) != 2 {
		t.Fatalf("len: got %d want 2", len(s.Entries))
	}
	if s.Entries[0].Path != "/a" || s.Entries[0].Name != "A2" {
		t.Fatalf("front: %+v", s.Entries[0])
	}
	if s.Entries[1].Path != "/b" {
		t.Fatalf("second: %+v", s.Entries[1])
	}
}

func TestTouch_CapsAtMax(t *testing.T) {
	s := Store{}
	for i := 0; i < MaxEntries+5; i++ {
		s.Touch(filepath.Join("/p", string(rune('a'+i))), "")
	}
	if len(s.Entries) != MaxEntries {
		t.Fatalf("len: got %d want %d", len(s.Entries), MaxEntries)
	}
}

func TestPrune_RemovesMissingDirs(t *testing.T) {
	keep := t.TempDir()
	gone := t.TempDir()
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}

	s := Store{Entries: []Entry{{Path: keep}, {Path: gone}}}
	removed := s.Prune()
	if removed != 1 {
		t.Fatalf("removed: got %d want 1", removed)
	}
	if len(s.Entries) != 1 || s.Entries[0].Path != keep {
		t.Fatalf("entries: %+v", s.Entries)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	s, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.Entries) != 0 {
		t.Fatalf("expected empty store, got %+v", s.Entries)
	}
}

func TestSaveAndLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	s := Store{}
	s.Touch("/tmp/x", "X")
	s.Touch("/tmp/y", "")
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Entries) != 2 || got.Entries[0].Path != "/tmp/y" {
		t.Fatalf("roundtrip: %+v", got.Entries)
	}
}
