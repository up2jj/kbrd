package editdraft

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDraftLifecycle(t *testing.T) {
	document := filepath.Join(t.TempDir(), "task.md")
	wantPath := filepath.Join(filepath.Dir(document), ".task.md.kbrd-swap")
	if got := Path(document); got != wantPath {
		t.Fatalf("Path() = %q, want %q", got, wantPath)
	}

	want := []byte("---\ntitle: task\n---\nbody\n")
	if err := Write(document, want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(document)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("Read() = %q, want %q", got, want)
	}
	if err := Clear(document); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if err := Clear(document); err != nil {
		t.Fatalf("second Clear: %v", err)
	}
	if _, err := os.Stat(wantPath); !os.IsNotExist(err) {
		t.Fatalf("sidecar still exists: %v", err)
	}
}
