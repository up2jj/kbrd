package model

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	cardhistory "kbrd/history"
)

func TestWriteRestoredCopyNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	preferred := filepath.Join(dir, "task (restored Jul 13).md")
	if err := os.WriteFile(preferred, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := writeRestoredCopy(preferred, []byte("snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "task (restored Jul 13) 2.md" {
		t.Fatalf("path = %q", path)
	}
	data, _ := os.ReadFile(preferred)
	if string(data) != "original" {
		t.Fatalf("existing copy overwritten: %q", data)
	}
}

func TestRestoredCopyPath(t *testing.T) {
	e := cardhistory.Event{Time: time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)}
	got := restoredCopyPath(filepath.Join("Doing", "task.md"), e)
	if got != filepath.Join("Doing", "task (restored Jul 13).md") {
		t.Fatalf("path = %q", got)
	}
}
