package colstore

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadMissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := Read(dir)
	if err != nil {
		t.Fatalf("Read on missing file: %v", err)
	}
	if got := s.All(); len(got) != 0 {
		t.Errorf("missing file: All() = %v, want empty", got)
	}
	if _, ok := s.Get("nope"); ok {
		t.Errorf("missing file: Get reported present")
	}
}

func TestRoundTripTypes(t *testing.T) {
	dir := t.TempDir()
	err := Update(dir, func(s *Store) error {
		s.Set("str", "hello")
		s.Set("int", int64(42))
		s.Set("float", 3.14)
		s.Set("bool", true)
		s.Set("arr", []interface{}{"a", "b"})
		s.Set("nested", map[string]interface{}{"k": "v"})
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Fresh load from disk to prove persistence (not just in-memory state).
	s, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	cases := map[string]interface{}{
		"str":    "hello",
		"int":    int64(42),
		"float":  3.14,
		"bool":   true,
		"arr":    []interface{}{"a", "b"},
		"nested": map[string]interface{}{"k": "v"},
	}
	for k, want := range cases {
		got, ok := s.Get(k)
		if !ok {
			t.Errorf("Get(%q): missing", k)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Get(%q) = %#v, want %#v", k, got, want)
		}
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	if err := Update(dir, func(s *Store) error { s.Set("a", "1"); s.Set("b", "2"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := Update(dir, func(s *Store) error { s.Delete("a"); return nil }); err != nil {
		t.Fatal(err)
	}
	s, _ := Read(dir)
	if _, ok := s.Get("a"); ok {
		t.Errorf("deleted key still present")
	}
	if _, ok := s.Get("b"); !ok {
		t.Errorf("untouched key was lost")
	}
	// Deleting an absent key is a no-op, not an error.
	if err := Update(dir, func(s *Store) error { s.Delete("ghost"); return nil }); err != nil {
		t.Errorf("delete absent key: %v", err)
	}
}

func TestInvalidTOMLErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("this is = = not toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir); err == nil {
		t.Errorf("Read on invalid TOML: want error, got nil")
	}
}

func TestSaveAtomicLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	if err := Update(dir, func(s *Store) error { s.Set("k", "v"); return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Errorf("store file missing after save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName+".tmp")); !os.IsNotExist(err) {
		t.Errorf("leftover .tmp file after save")
	}
}

func TestAllReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	_ = Update(dir, func(s *Store) error { s.Set("k", "v"); return nil })
	s, _ := Read(dir)
	m := s.All()
	m["k"] = "mutated"
	if got, _ := s.Get("k"); got != "v" {
		t.Errorf("All() did not return a copy: Get = %v", got)
	}
}
