package search

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestColumnPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, dir := range []string{"1. todo", "2. done", ".hidden", "_private"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "root.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := columnPaths([]Root{{Path: root}})
	sort.Strings(got)
	want := []string{filepath.Join(root, "1. todo"), filepath.Join(root, "2. done")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("columnPaths = %v, want %v", got, want)
	}
}

func TestParseRipgrep(t *testing.T) {
	t.Parallel()

	roots := []Root{{Path: "/board", Name: "Board"}}
	out := []byte(`{"type":"begin","data":{"path":{"text":"/board/todo/task.md"}}}
{"type":"match","data":{"path":{"text":"/board/todo/task.md"},"lines":{"text":"find phrase here\n"},"line_number":3,"submatches":[{"start":5,"end":11}]}}
{"type":"end","data":{}}`)

	got := parseRipgrep(out, roots)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	match := got[0]
	if match.BoardPath != "/board" || match.BoardName != "Board" {
		t.Errorf("board = %q/%q, want /board/Board", match.BoardPath, match.BoardName)
	}
	if match.Column != "todo" || match.Item != "task" {
		t.Errorf("column/item = %q/%q, want todo/task", match.Column, match.Item)
	}
	if match.Line != 3 || match.Text != "find phrase here" {
		t.Errorf("line/text = %d/%q", match.Line, match.Text)
	}
	if match.MatchCol != 5 || match.MatchLen != 6 {
		t.Errorf("match span = (%d, %d), want (5, 6)", match.MatchCol, match.MatchLen)
	}
}

func TestBoardForPathUsesLongestRoot(t *testing.T) {
	t.Parallel()

	roots := []Root{
		{Path: "/a", Name: "A"},
		{Path: "/a/sub", Name: "Sub"},
	}
	path, name := boardForPath("/a/sub/col/file.md", roots)
	if path != "/a/sub" || name != "Sub" {
		t.Errorf("boardForPath = %q/%q, want /a/sub/Sub", path, name)
	}
}
