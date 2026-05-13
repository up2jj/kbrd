package model

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"kbrd/config"
)

func TestBoard_LoadColumns_SkipsHiddenAndUnderscore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dirs := []string{
		"1. TO DO",
		"2. IN PROGRESS",
		".git",
		"_archive",
	}
	for _, name := range dirs {
		if err := os.Mkdir(filepath.Join(dir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// A stray top-level file shouldn't crash or be considered a column.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	b := NewBoard(config.Config{Path: dir, ColumnWidth: 32, PreviewLines: 3})
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}

	got := make([]string, len(b.columns))
	for i, c := range b.columns {
		got[i] = c.Name
	}
	sort.Strings(got)
	want := []string{"1. TO DO", "2. IN PROGRESS"}
	if len(got) != len(want) {
		t.Fatalf("columns = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("columns[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBoard_LoadColumns_EmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	b := NewBoard(config.Config{Path: dir, ColumnWidth: 32, PreviewLines: 3})
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}
	if len(b.columns) != 0 {
		t.Errorf("columns = %d, want 0", len(b.columns))
	}
}

func TestBoard_CreateDefaultColumns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	b := NewBoard(config.Config{Path: dir, ColumnWidth: 32, PreviewLines: 3})
	if err := b.createDefaultColumns(); err != nil {
		t.Fatalf("createDefaultColumns: %v", err)
	}
	for _, name := range []string{"1. TO DO", "2. IN PROGRESS", "3. DONE"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("missing %q: %v", name, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q is not a directory", name)
		}
	}

	// Re-running is idempotent (MkdirAll is a no-op on existing dirs).
	if err := b.createDefaultColumns(); err != nil {
		t.Errorf("re-run failed: %v", err)
	}

	// After creation, loadColumns should see all three.
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}
	if len(b.columns) != 3 {
		t.Errorf("columns = %d, want 3", len(b.columns))
	}
}
