package scratchpad

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreIsBoardScopedAndOutsideBoard(t *testing.T) {
	storeDir := t.TempDir()
	boardA := filepath.Join(t.TempDir(), "board-a")
	boardB := filepath.Join(t.TempDir(), "board-b")
	for _, path := range []string{boardA, boardB} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	store, err := Open(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(boardA, "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(boardB, "bravo"); err != nil {
		t.Fatal(err)
	}

	gotA, err := store.Load(boardA)
	if err != nil || gotA != "alpha" {
		t.Fatalf("Load(boardA) = %q, %v", gotA, err)
	}
	gotB, err := store.Load(boardB)
	if err != nil || gotB != "bravo" {
		t.Fatalf("Load(boardB) = %q, %v", gotB, err)
	}
	path, err := store.Path(boardA)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(path, boardA+string(filepath.Separator)) {
		t.Fatalf("scratchpad path %q is inside board %q", path, boardA)
	}
	if filepath.Ext(path) != ".md" {
		t.Fatalf("scratchpad path = %q, want Markdown file", path)
	}
}

func TestStoreAppendSeparatesEntries(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	boardPath := t.TempDir()
	if _, err := store.Append(boardPath, "first"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Append(boardPath, "second")
	if err != nil {
		t.Fatal(err)
	}
	if got != "first\n\nsecond" {
		t.Fatalf("Append = %q", got)
	}
}

func TestStoreUsesPrivatePermissions(t *testing.T) {
	storeDir := filepath.Join(t.TempDir(), "scratchpads")
	store, err := Open(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	boardPath := t.TempDir()
	if err := store.Save(boardPath, "secret"); err != nil {
		t.Fatal(err)
	}
	path, err := store.Path(boardPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 600", got)
	}
	dirInfo, err := os.Stat(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("directory mode = %o, want 700", got)
	}
}
