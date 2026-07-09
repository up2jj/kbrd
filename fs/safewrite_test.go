package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteExistingFileAtomicDurablePreservesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "card.md")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WriteExistingFileAtomicDurable(path, []byte("secret")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "secret" {
		t.Fatalf("content = %q, want secret", got)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("left temp files: %v", matches)
	}
}

func TestWriteExistingFileAtomicDurableMissingDoesNotCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.md")
	if err := WriteExistingFileAtomicDurable(path, []byte("body")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want os.ErrNotExist, got %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file was created, stat err = %v", err)
	}
}

func TestWriteFileAtomicDurableCleansTempOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := WriteFileAtomicDurable(target, []byte("body"), 0o644); err == nil {
		t.Fatal("expected rename over directory to fail")
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".target.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("left temp files after failed rename: %v", matches)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("target directory damaged: info=%v err=%v", info, err)
	}
}

func TestWriteNewFileNoClobberDurableRefusesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.md")
	if err := WriteNewFileNoClobberDurable(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewFileNoClobberDurable(path, []byte("second"), 0o644); !errors.Is(err, os.ErrExist) {
		t.Fatalf("want os.ErrExist, got %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Fatalf("content = %q, want first", got)
	}
}
