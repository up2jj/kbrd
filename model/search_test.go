package model

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"kbrd/config"
	"kbrd/recents"
)

func TestBuildSearchRoots(t *testing.T) {
	t.Parallel()

	roots := buildSearchRoots("/boards/active", "Active", []recents.Entry{
		{Path: "/boards/other", Name: "Other"},
		{Path: "/boards/active", Name: "Active dup"}, // dedup against active
		{Path: "/boards/other", Name: "Other dup"},   // dedup against itself
	})

	want := []recents.Entry{
		{Path: "/boards/active", Name: "Active"},
		{Path: "/boards/other", Name: "Other"},
	}
	if !reflect.DeepEqual(roots, want) {
		t.Errorf("roots = %+v, want %+v", roots, want)
	}
}

func TestBuildSearchRootsEmptyActive(t *testing.T) {
	t.Parallel()

	roots := buildSearchRoots("", "", []recents.Entry{{Path: "/boards/a"}})
	if len(roots) != 1 || roots[0].Path != "/boards/a" {
		t.Errorf("roots = %+v, want only /boards/a", roots)
	}
}

func TestSamePath(t *testing.T) {
	t.Parallel()

	abs, _ := filepath.Abs("foo/bar")
	cases := []struct {
		a, b string
		want bool
	}{
		{"/x/y", "/x/y", true},
		{"/x/y", "/x/z", false},
		{"foo/bar", abs, true}, // relative resolves to absolute
	}
	for _, c := range cases {
		if got := samePath(c.a, c.b); got != c.want {
			t.Errorf("samePath(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestLocateFile(t *testing.T) {
	t.Parallel()

	columns := []*Column{
		{Path: "/board/todo", Items: []Item{{Name: "a"}, {Name: "b"}}},
		{Path: "/board/done", Items: []Item{{Name: "c"}}},
	}

	t.Run("found", func(t *testing.T) {
		col, item, ok := locateFile(columns, "/board/done/c.md")
		if !ok || col != 1 || item != 0 {
			t.Errorf("locateFile = (%d, %d, %v), want (1, 0, true)", col, item, ok)
		}
	})

	t.Run("item not in column", func(t *testing.T) {
		if _, _, ok := locateFile(columns, "/board/todo/missing.md"); ok {
			t.Error("locateFile ok = true, want false for unknown item")
		}
	})

	t.Run("dir not a column", func(t *testing.T) {
		if _, _, ok := locateFile(columns, "/board/other/a.md"); ok {
			t.Error("locateFile ok = true, want false for unknown column")
		}
	})
}

func TestSearchActionsActivateFile_CurrentBoardSelectsByPath(t *testing.T) {
	t.Parallel()

	boardDir := makeSearchBoard(t, map[string][]string{
		"1. todo": {"a"},
		"2. done": {"target"},
	})
	b := NewBoard(config.Config{Path: boardDir, ColumnWidth: 32, PreviewLines: 3, NotifyBackend: "none"})
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}

	_, cmd := b.searchActions().activateFile(boardDir, filepath.Join(boardDir, "2. done", "target.md"))
	if cmd != nil {
		t.Fatalf("activate current board returned cmd, want nil")
	}
	if b.selectedCol != 1 {
		t.Fatalf("selectedCol = %d, want 1", b.selectedCol)
	}
	if got := b.columns[b.selectedCol].SelectedItem().Name; got != "target" {
		t.Fatalf("selected item = %q, want target", got)
	}
}

func TestSearchActionsActivateFile_CrossBoardSwitchesAndSelectsByPath(t *testing.T) {
	t.Parallel()

	activeDir := makeSearchBoard(t, map[string][]string{"1. active": {"a"}})
	targetDir := makeSearchBoard(t, map[string][]string{
		"1. todo": {"first"},
		"2. done": {"target"},
	})
	b := NewBoard(config.Config{Path: activeDir, ColumnWidth: 32, PreviewLines: 3, NotifyBackend: "none"})
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}

	_, cmd := b.searchActions().activateFile(targetDir, filepath.Join(targetDir, "2. done", "target.md"))
	if cmd == nil {
		t.Fatalf("activate cross-board returned nil cmd, want switch notification/watch command")
	}
	if !samePath(b.cfg.Path, targetDir) {
		t.Fatalf("board path = %q, want %q", b.cfg.Path, targetDir)
	}
	if b.selectedCol != 1 {
		t.Fatalf("selectedCol = %d, want 1", b.selectedCol)
	}
	if got := b.columns[b.selectedCol].SelectedItem().Name; got != "target" {
		t.Fatalf("selected item = %q, want target", got)
	}
}

func TestSearchActionsActivateFile_CrossBoardMissingFileKeepsSwitch(t *testing.T) {
	t.Parallel()

	activeDir := makeSearchBoard(t, map[string][]string{"1. active": {"a"}})
	targetDir := makeSearchBoard(t, map[string][]string{"1. todo": {"first"}})
	b := NewBoard(config.Config{Path: activeDir, ColumnWidth: 32, PreviewLines: 3, NotifyBackend: "none"})
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}

	_, cmd := b.searchActions().activateFile(targetDir, filepath.Join(targetDir, "1. todo", "missing.md"))
	if cmd == nil {
		t.Fatalf("activate cross-board missing file returned nil cmd, want switch notification/watch command")
	}
	if !samePath(b.cfg.Path, targetDir) {
		t.Fatalf("board path = %q, want %q", b.cfg.Path, targetDir)
	}
	if got := b.columns[b.selectedCol].SelectedItem().Name; got != "first" {
		t.Fatalf("selected item = %q, want first fallback selection", got)
	}
}

func makeSearchBoard(t *testing.T, columns map[string][]string) string {
	t.Helper()
	dir := t.TempDir()
	for colName, items := range columns {
		colDir := filepath.Join(dir, colName)
		if err := os.MkdirAll(colDir, 0o755); err != nil {
			t.Fatalf("mkdir column: %v", err)
		}
		for _, item := range items {
			writeFile(t, colDir, item+".md", item+"\n")
		}
	}
	return dir
}

func TestGroupByFile(t *testing.T) {
	t.Parallel()

	results := []searchResult{
		{FilePath: "/b/c/x.md", Column: "c", Item: "x", Line: 2, Text: "two"},
		{FilePath: "/b/c/y.md", Column: "c", Item: "y", Line: 1, Text: "one"},
		{FilePath: "/b/c/x.md", Column: "c", Item: "x", Line: 5, Text: "five"},
	}

	groups := groupByFile(results)

	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2", len(groups))
	}
	// Order preserved: x first (it appeared first), then y.
	if groups[0].FilePath != "/b/c/x.md" || groups[1].FilePath != "/b/c/y.md" {
		t.Errorf("group order = [%s, %s], want [x, y]", groups[0].FilePath, groups[1].FilePath)
	}
	if len(groups[0].Matches) != 2 {
		t.Errorf("x matches = %d, want 2", len(groups[0].Matches))
	}
	if groups[0].Matches[0].Line != 2 || groups[0].Matches[1].Line != 5 {
		t.Errorf("x match lines = %v, want [2 5]", groups[0].Matches)
	}
}

func TestColumnPaths(t *testing.T) {
	t.Parallel()

	board := t.TempDir()
	for _, d := range []string{"1. todo", "2. done", ".hidden", "_private"} {
		if err := os.Mkdir(filepath.Join(board, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, board, "root.md", "x") // a file, not a dir — must be ignored

	got := columnPaths([]recents.Entry{{Path: board}})
	sort.Strings(got)
	want := []string{filepath.Join(board, "1. todo"), filepath.Join(board, "2. done")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("columnPaths = %v, want %v (hidden/private dirs and files excluded)", got, want)
	}
}

func TestParseRipgrep(t *testing.T) {
	t.Parallel()

	roots := []recents.Entry{{Path: "/board", Name: "Board"}}
	// One match line as ripgrep --json emits it. "phrase" starts at byte 5.
	out := []byte(`{"type":"begin","data":{"path":{"text":"/board/todo/task.md"}}}
{"type":"match","data":{"path":{"text":"/board/todo/task.md"},"lines":{"text":"find phrase here\n"},"line_number":3,"submatches":[{"start":5,"end":11}]}}
{"type":"end","data":{}}`)

	got := parseRipgrep(out, roots)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (only match events kept)", len(got))
	}
	r := got[0]
	if r.BoardPath != "/board" || r.BoardName != "Board" {
		t.Errorf("board = %q/%q, want /board/Board", r.BoardPath, r.BoardName)
	}
	if r.Column != "todo" || r.Item != "task" {
		t.Errorf("column/item = %q/%q, want todo/task", r.Column, r.Item)
	}
	if r.Line != 3 || r.Text != "find phrase here" {
		t.Errorf("line/text = %d/%q, want 3/\"find phrase here\"", r.Line, r.Text)
	}
	if r.matchCol != 5 || r.matchLen != 6 {
		t.Errorf("match span = (%d, %d), want (5, 6)", r.matchCol, r.matchLen)
	}
}

func TestBoardForPath(t *testing.T) {
	t.Parallel()

	roots := []recents.Entry{
		{Path: "/a", Name: "A"},
		{Path: "/a/sub", Name: "Sub"}, // longer prefix must win
	}
	path, name := boardForPath("/a/sub/col/file.md", roots)
	if path != "/a/sub" || name != "Sub" {
		t.Errorf("boardForPath = %q/%q, want /a/sub/Sub (longest prefix)", path, name)
	}
}
