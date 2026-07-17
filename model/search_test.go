package model

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"kbrd/config"
	"kbrd/events"
	"kbrd/recents"
	searchbackend "kbrd/search"
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

func TestGroupSearchResultsMergesFilesystemAndVirtualPath(t *testing.T) {
	t.Parallel()

	path := "/board/Todo/task.md"
	results := []searchResult{
		{BoardPath: "/board", FilePath: path, Column: "Todo", Item: "task", Line: 3, Text: "body phrase"},
		{BoardPath: "/board", FilePath: path, Column: "Focus", Item: "Task title", Text: "Task title", virtualVID: "focus", virtualItem: "task-1"},
		{BoardPath: "/board", FilePath: path, Column: "Todo", Item: "task", Line: 3, Text: "body phrase"},
	}
	virtuals := []searchbackend.VirtualItem{{
		BoardPath: "/board", Column: "Focus", VID: "focus", ID: "task-1", Title: "Task title", FilePath: path,
	}}

	groups := groupSearchResults(results, virtuals)
	if len(groups) != 1 {
		t.Fatalf("groups = %+v, want one path-deduplicated result", groups)
	}
	group := groups[0]
	if group.Column != "Focus" || group.Item != "Task title" || group.VirtualVID != "focus" || group.VirtualItem != "task-1" {
		t.Fatalf("merged group identity = %+v", group)
	}
	if len(group.Matches) != 2 {
		t.Fatalf("merged matches = %+v, want duplicate body match removed", group.Matches)
	}
}

func TestSearchActionsActivateFilePrefersVisibleVirtualItem(t *testing.T) {
	boardDir := makeSearchBoard(t, map[string][]string{"Todo": {"target"}})
	b := NewBoard(config.Config{Path: boardDir, ColumnWidth: 32, PreviewLines: 3, NotifyBackend: "none"})
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(boardDir, "Todo", "target.md")
	b.setVirtualColumn("focus", events.VirtualColumnSpec{
		Name:  "Focus",
		Items: []events.VirtualItem{{ID: "virtual-target", Title: "Target", Path: path}},
	})
	if err := b.hideAllColumns(events.ColumnKindReal); err != nil {
		t.Fatal(err)
	}

	_, cmd := b.searchActions().activateFile(boardDir, path)
	if cmd != nil {
		t.Fatalf("activate virtual result command = %T, want nil", cmd)
	}
	if !b.columns[b.selectedCol].Virtual || b.columns[b.selectedCol].VID != "focus" {
		t.Fatalf("selected column = %+v, want virtual focus", b.columns[b.selectedCol])
	}
	if got := b.columns[b.selectedCol].SelectedItem().Name; got != "virtual-target" {
		t.Fatalf("selected item = %q, want virtual-target", got)
	}
}

func TestSearchActionsActivateFileRevealsHiddenFilesystemColumn(t *testing.T) {
	boardDir := makeSearchBoard(t, map[string][]string{"Todo": {"first"}, "Archive": {"target"}})
	b := NewBoard(config.Config{Path: boardDir, ColumnWidth: 32, PreviewLines: 3, NotifyBackend: "none"})
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	if err := b.hideColumn("Archive"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(boardDir, "Archive", "target.md")
	_, _ = b.searchActions().activateFile(boardDir, path)
	if b.columns[b.selectedCol].Name != "Archive" || b.columns[b.selectedCol].SelectedItem().Name != "target" {
		t.Fatalf("selection = %q/%q, want revealed Archive/target", b.columns[b.selectedCol].Name, b.columns[b.selectedCol].SelectedItem().Name)
	}
	if b.columnHidden("Archive") {
		t.Fatal("search activation left Archive hidden")
	}
}

func TestSearchActionsActivateFilelessVirtualResult(t *testing.T) {
	boardDir := makeSearchBoard(t, map[string][]string{"Todo": {"first"}})
	b := NewBoard(config.Config{Path: boardDir, ColumnWidth: 32, PreviewLines: 3, NotifyBackend: "none"})
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	b.setVirtualColumn("focus", events.VirtualColumnSpec{
		Name:  "Focus",
		Items: []events.VirtualItem{{ID: "virtual-target", Title: "Target without a file"}},
	})

	_, cmd := b.searchActions().activateResult(searchSelectMsg{
		BoardPath: b.cfg.Path, VirtualVID: "focus", VirtualItem: "virtual-target",
	})
	if cmd != nil {
		t.Fatalf("activate fileless result command = %T, want nil", cmd)
	}
	if !b.columns[b.selectedCol].Virtual || b.columns[b.selectedCol].SelectedItem().Name != "virtual-target" {
		t.Fatalf("fileless selection = %+v", b.columns[b.selectedCol].SelectedItem())
	}
}
