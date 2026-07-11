package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/recents"
)

func runIngestCommand(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newIngestCmd()
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := cmd.Execute()
	return out.String(), err
}

func makeIngestBoard(t *testing.T, columns ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range columns {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestIngestUsesRecentBoardNumericColumnAndSanitizedName(t *testing.T) {
	isolateConfig(t)
	root := makeIngestBoard(t, "1. TODO", "2. DOING", "_archive")
	store := recents.Store{Entries: []recents.Entry{{Name: "Work", Path: root}}}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	out, err := runIngestCommand(t, "# Fix it", "--board", "work", "--column", "2", "--name", "Fix / OAuth: P1!")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "2. DOING", "fix-oauth-p1.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "# Fix it\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if !strings.Contains(out, "ingested fix-oauth-p1.md in [Work] 2. DOING") {
		t.Fatalf("output = %q", out)
	}
}

func TestIngestUsesFilesystemBoardAndFirstColumn(t *testing.T) {
	isolateConfig(t)
	root := makeIngestBoard(t, "2. Doing", "1. Todo")

	_, err := runIngestCommand(t, "", "--board", root, "--name", "A Card.md", "--content", "body")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "1. Todo", "a-card.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "body\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestIngestReadsFileAndRejectsInvalidInputs(t *testing.T) {
	isolateConfig(t)
	root := makeIngestBoard(t, "todo")
	input := filepath.Join(t.TempDir(), "input.md")
	if err := os.WriteFile(input, []byte("from file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := runIngestCommand(t, "", "--board", root, "--name", "File", "--file", input)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "todo", "file.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "from file\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}

	_, err = runIngestCommand(t, "", "--board", root, "--name", "Other", "--content", "x", "--file", input)
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("mutually exclusive error = %v", err)
	}

	_, err = runIngestCommand(t, "", "--board", root, "--column", "2", "--name", "Other", "--content", "x")
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("column range error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "2")); !os.IsNotExist(err) {
		t.Fatalf("missing column was created or stat failed: %v", err)
	}
}
