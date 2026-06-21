package board

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestItemPathSanitizes(t *testing.T) {
	col := t.TempDir()
	got, err := ItemPath(col, " task .md ")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(col, "task.md"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	for _, bad := range []string{"", "..", "a/b", `a\b`, "../escape"} {
		if _, err := ItemPath(col, bad); err == nil {
			t.Fatalf("ItemPath(%q) accepted a bad name", bad)
		}
	}
}

func TestReadItem(t *testing.T) {
	root := makeBoard(t, map[string][]string{"todo": nil})
	col := filepath.Join(root, "todo")
	os.WriteFile(filepath.Join(col, "a.md"), []byte("hello\n"), 0o644)

	got, err := ReadItem(col, "a")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello\n" {
		t.Fatalf("got %q", got)
	}

	if _, err := ReadItem(col, "missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want os.ErrNotExist, got %v", err)
	}
}

func TestWriteItem(t *testing.T) {
	root := makeBoard(t, map[string][]string{"todo": {"a"}})
	col := filepath.Join(root, "todo")
	path := filepath.Join(col, "a.md")

	// Overwrites existing content and normalizes the trailing newline.
	if err := WriteItem(col, "a", "new body"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new body\n" {
		t.Fatalf("got %q", got)
	}

	// Empty content stays empty (no stray newline).
	if err := WriteItem(col, "a", ""); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(filepath.Join(col, "a.md"))
	if string(got) != "" {
		t.Fatalf("got %q, want empty", got)
	}

	// Editing a missing item must never create it.
	if err := WriteItem(col, "ghost", "x"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want os.ErrNotExist, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(col, "ghost.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("WriteItem created a missing item")
	}
}

func TestReplaceFileContentAtomicPreservesMode(t *testing.T) {
	root := makeBoard(t, map[string][]string{"todo": {"a"}})
	path := filepath.Join(root, "todo", "a.md")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ReplaceFileContent(path, "secret"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("left temp file: %v", err)
	}
}

func TestWriteAtomicCleansTempOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := writeAtomic(target, []byte("body"), 0o644); err == nil {
		t.Fatal("expected rename over directory to fail")
	}
	if _, err := os.Stat(target + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("left temp file after failed rename: %v", err)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("target directory damaged: info=%v err=%v", info, err)
	}
}

func TestAppendLine(t *testing.T) {
	root := makeBoard(t, map[string][]string{"todo": nil})
	path := filepath.Join(root, "todo", "a.md")

	// Missing trailing newline gets a separator inserted.
	os.WriteFile(path, []byte("body"), 0o644)
	if err := AppendLine(path, "extra"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "body\nextra\n" {
		t.Fatalf("got %q", got)
	}

	// Trailing newline present: no doubled blank line.
	if err := AppendLine(path, "more"); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "body\nextra\nmore\n" {
		t.Fatalf("got %q", got)
	}

	if err := AppendLine(filepath.Join(root, "todo", "ghost.md"), "x"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want os.ErrNotExist, got %v", err)
	}
}

func TestPrependLine(t *testing.T) {
	root := makeBoard(t, map[string][]string{"todo": nil})
	path := filepath.Join(root, "todo", "a.md")
	os.WriteFile(path, []byte("body\n"), 0o644)

	if err := PrependLine(path, "first"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "first\nbody\n" {
		t.Fatalf("got %q", got)
	}
}

func TestJournalLine(t *testing.T) {
	root := makeBoard(t, map[string][]string{"todo": nil})
	path := filepath.Join(root, "todo", "a.md")
	os.WriteFile(path, []byte("body\n"), 0o644)

	if err := JournalLine(path, time.Now(), "did a thing"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	// "body\n" + "YYYY-MM-DD HH:MM:SS - did a thing\n"
	lines := strings.Split(strings.TrimSuffix(string(got), "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.HasSuffix(last, " - did a thing") || len(last) != len("2006-01-02 15:04:05 - did a thing") {
		t.Fatalf("journal line %q", last)
	}
}

func TestDetectDate(t *testing.T) {
	// Fixed reference: Friday 2026-06-19, 14:30. Date-only phrases inherit this
	// wall clock, so detected dates keep 14:30:00.
	now := time.Date(2026, 6, 19, 14, 30, 0, 0, time.UTC)
	dateOnly := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 14, 30, 0, 0, time.UTC)
	}

	tests := []struct {
		name     string
		text     string
		wantTime time.Time
		wantBody string
	}{
		{"no date", "fixed the login bug", now, "fixed the login bug"},
		{"yesterday", "yesterday fixed the bug", dateOnly(2026, 6, 18), "fixed the bug"},
		{"next monday two-token strip", "next monday call client", dateOnly(2026, 6, 22), "call client"},
		{"longest prefix wins", "2 weeks ago reviewed pr", dateOnly(2026, 6, 5), "reviewed pr"},
		{"date only, empty body falls back", "tomorrow", now, "tomorrow"},
		{"polish", "wczoraj naprawiłem bug", dateOnly(2026, 6, 18), "naprawiłem bug"},
		{"absolute date", "2026-01-02 shipped release", dateOnly(2026, 1, 2), "shipped release"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTime, gotBody := DetectDate(tt.text, now)
			if !gotTime.Equal(tt.wantTime) {
				t.Errorf("time = %s, want %s", gotTime, tt.wantTime)
			}
			if gotBody != tt.wantBody {
				t.Errorf("body = %q, want %q", gotBody, tt.wantBody)
			}
		})
	}
}

func TestRenameNoClobber(t *testing.T) {
	root := makeBoard(t, map[string][]string{"todo": {"a", "b"}})
	col := filepath.Join(root, "todo")

	// Destination taken: refuse, both files intact.
	err := RenameNoClobber(filepath.Join(col, "a.md"), filepath.Join(col, "b.md"))
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("want os.ErrExist, got %v", err)
	}
	for _, f := range []string{"a.md", "b.md"} {
		if _, err := os.Stat(filepath.Join(col, f)); err != nil {
			t.Fatalf("%s damaged: %v", f, err)
		}
	}

	// Free destination: renames.
	if err := RenameNoClobber(filepath.Join(col, "a.md"), filepath.Join(col, "c.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(col, "c.md")); err != nil {
		t.Fatal("rename did not happen")
	}
}

func TestMoveItem(t *testing.T) {
	root := makeBoard(t, map[string][]string{"todo": {"a"}, "done": {"taken"}})
	src, dest := filepath.Join(root, "todo"), filepath.Join(root, "done")

	if err := MoveItem(src, dest, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.md")); err != nil {
		t.Fatal("item not moved")
	}

	if err := MoveItem(src, dest, "a"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("moving missing item: want os.ErrNotExist, got %v", err)
	}

	os.WriteFile(filepath.Join(src, "taken.md"), []byte("src\n"), 0o644)
	if err := MoveItem(src, dest, "taken"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("clobbering move: want os.ErrExist, got %v", err)
	}

	if err := MoveItem(src, dest, "../escape"); err == nil {
		t.Fatal("MoveItem accepted a bad name")
	}
}

func TestRenameItemBoard(t *testing.T) {
	root := makeBoard(t, map[string][]string{"todo": {"a", "b"}})
	col := filepath.Join(root, "todo")

	if err := RenameItem(col, "a", "b"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("want os.ErrExist, got %v", err)
	}
	if err := RenameItem(col, "ghost", "x"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want os.ErrNotExist, got %v", err)
	}
	if err := RenameItem(col, "a", "renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(col, "renamed.md")); err != nil {
		t.Fatal("rename did not happen")
	}
}

func TestDeleteItem(t *testing.T) {
	root := makeBoard(t, map[string][]string{"todo": {"a"}})
	col := filepath.Join(root, "todo")

	if err := DeleteItem(col, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(col, "a.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("item still exists after DeleteItem")
	}

	if err := DeleteItem(col, "a"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want os.ErrNotExist, got %v", err)
	}

	if err := DeleteItem(col, "../escape"); err == nil {
		t.Fatal("DeleteItem accepted a bad name")
	}
}
