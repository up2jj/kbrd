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

func TestSetPinned_InsertsAndToggles(t *testing.T) {
	s := Store{}
	s.SetPinned("/a", "A", true)
	if len(s.Entries) != 1 || !s.Entries[0].Pinned {
		t.Fatalf("after pin insert: %+v", s.Entries)
	}
	s.SetPinned("/a", "A", false)
	if len(s.Entries) != 1 || s.Entries[0].Pinned {
		t.Fatalf("after unpin: %+v", s.Entries)
	}
	// Unpinning an absent path is a no-op (doesn't insert).
	s.SetPinned("/missing", "", false)
	if len(s.Entries) != 1 {
		t.Fatalf("unpin absent should not insert: %+v", s.Entries)
	}
}

func TestTouch_PreservesPinned(t *testing.T) {
	s := Store{}
	s.Touch("/a", "A")
	s.SetPinned("/a", "A", true)
	s.Touch("/b", "B")
	s.Touch("/a", "A-new")
	if s.Entries[0].Path != "/a" || !s.Entries[0].Pinned || s.Entries[0].Name != "A-new" {
		t.Fatalf("pin not preserved across Touch: %+v", s.Entries[0])
	}
}

func TestTouch_CapDoesNotEvictPinned(t *testing.T) {
	s := Store{}
	s.SetPinned("/pin", "P", true)
	for i := 0; i < MaxEntries+5; i++ {
		s.Touch(filepath.Join("/p", string(rune('a'+i))), "")
	}
	// All pinned survive; unpinned capped at MaxEntries.
	pinned, unpinned := 0, 0
	for _, e := range s.Entries {
		if e.Pinned {
			pinned++
		} else {
			unpinned++
		}
	}
	if pinned != 1 || unpinned != MaxEntries {
		t.Fatalf("pinned=%d unpinned=%d want 1/%d (entries=%+v)", pinned, unpinned, MaxEntries, s.Entries)
	}
}

func TestPrune_KeepsPinnedEvenIfMissing(t *testing.T) {
	gone := t.TempDir()
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}
	s := Store{Entries: []Entry{{Path: gone, Pinned: true}}}
	removed := s.Prune()
	if removed != 0 || len(s.Entries) != 1 {
		t.Fatalf("pinned missing dir got pruned: removed=%d entries=%+v", removed, s.Entries)
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
