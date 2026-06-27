package model

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"kbrd/config"
	"kbrd/events"
)

// newTestColumn creates a column rooted at a temporary directory with the
// supplied .md files written to disk and LoadItems already invoked.
// Each entry in `files` is mapped to "<key>.md" with the value as content.
func newTestColumn(t *testing.T, files map[string]string) *Column {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	col := NewColumn(filepath.Base(dir), dir, ItemOptions{PreviewLines: 3})
	if err := col.LoadItems(); err != nil {
		t.Fatalf("LoadItems: %v", err)
	}
	return col
}

func itemNames(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Name
	}
	return out
}

func TestColumn_LoadItems(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// .md files
	if err := os.WriteFile(filepath.Join(dir, "alpha.md"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bravo.md"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	// non-.md file (should be ignored)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0644); err != nil {
		t.Fatal(err)
	}
	// subdirectory (should be ignored)
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	col := NewColumn("c", dir, ItemOptions{PreviewLines: 3})
	if err := col.LoadItems(); err != nil {
		t.Fatalf("LoadItems: %v", err)
	}
	if col.TotalCount() != 2 {
		t.Errorf("TotalCount = %d, want 2", col.TotalCount())
	}
	got := itemNames(col.Items)
	want := []string{"alpha", "bravo"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Items[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestColumn_LoadItems_ReusesUnchangedFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	aPath := filepath.Join(dir, "alpha.md")
	bPath := filepath.Join(dir, "bravo.md")
	if err := os.WriteFile(aPath, []byte("aaa"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bPath, []byte("bbb"), 0644); err != nil {
		t.Fatal(err)
	}
	col := NewColumn("c", dir, ItemOptions{PreviewLines: 3})
	if err := col.LoadItems(); err != nil {
		t.Fatalf("LoadItems: %v", err)
	}

	aInfo, err := os.Stat(aPath)
	if err != nil {
		t.Fatal(err)
	}

	// Rewrite alpha with different content but the SAME size, then restore its
	// original mtime. The (mtime, size) cache must treat it as unchanged and
	// reuse the prior item — so its preview keeps the OLD content, proving the
	// file was not re-read.
	if err := os.WriteFile(aPath, []byte("ZZZ"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(aPath, aInfo.ModTime(), aInfo.ModTime()); err != nil {
		t.Fatal(err)
	}

	// Genuinely change bravo: different size and a bumped mtime.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(bPath, []byte("brand new bravo"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := col.LoadItems(); err != nil {
		t.Fatalf("LoadItems (reload): %v", err)
	}

	byName := map[string]Item{}
	for _, it := range col.Items {
		byName[it.Name] = it
	}
	if got := byName["alpha"].Preview; len(got) != 1 || got[0] != "aaa" {
		t.Errorf("alpha preview = %v, want [aaa] (cached, not re-read)", got)
	}
	if got := byName["bravo"].Preview; len(got) != 1 || got[0] != "brand new bravo" {
		t.Errorf("bravo preview = %v, want [brand new bravo] (re-read)", got)
	}
}

func TestColumn_LoadItems_SkipsHiddenAndUnderscore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := []string{
		"alpha.md",   // included
		".hidden.md", // skipped (dotfile)
		"_draft.md",  // skipped (underscore prefix)
		"bravo.md",   // included
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	col := NewColumn("c", dir, ItemOptions{PreviewLines: 3})
	if err := col.LoadItems(); err != nil {
		t.Fatalf("LoadItems: %v", err)
	}
	got := itemNames(col.Items)
	want := []string{"alpha", "bravo"}
	if len(got) != len(want) {
		t.Fatalf("Items = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Items[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestColumn_LoadItems_MissingDir(t *testing.T) {
	t.Parallel()
	col := NewColumn("c", filepath.Join(t.TempDir(), "nope"), ItemOptions{PreviewLines: 3})
	if err := col.LoadItems(); err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestColumn_CreateItem(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, nil)
	filename, err := col.CreateItem("brand-new")
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if filename != "brand-new.md" {
		t.Errorf("filename = %q, want %q", filename, "brand-new.md")
	}
	if _, err := os.Stat(filepath.Join(col.Path, "brand-new.md")); err != nil {
		t.Errorf("file not created: %v", err)
	}
	if col.TotalCount() != 1 {
		t.Errorf("TotalCount = %d, want 1", col.TotalCount())
	}
}

func TestColumn_DeleteItem(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, map[string]string{"task": "hi"})

	t.Run("happy path", func(t *testing.T) {
		if err := col.DeleteItem("task"); err != nil {
			t.Fatalf("DeleteItem: %v", err)
		}
		if _, err := os.Stat(filepath.Join(col.Path, "task.md")); !os.IsNotExist(err) {
			t.Errorf("file still exists, stat err = %v", err)
		}
	})

	t.Run("missing item", func(t *testing.T) {
		err := col.DeleteItem("ghost")
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("err = %v, want os.ErrNotExist", err)
		}
	})
}

func TestColumn_AppendText(t *testing.T) {
	t.Parallel()

	t.Run("file ending with newline", func(t *testing.T) {
		col := newTestColumn(t, map[string]string{"task": "header\n"})
		if err := col.AppendText("task", "more"); err != nil {
			t.Fatalf("AppendText: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(col.Path, "task.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "header\nmore\n" {
			t.Errorf("content = %q, want %q", got, "header\nmore\n")
		}
	})

	t.Run("file without trailing newline gets one injected", func(t *testing.T) {
		col := newTestColumn(t, map[string]string{"task": "header"})
		if err := col.AppendText("task", "more"); err != nil {
			t.Fatalf("AppendText: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(col.Path, "task.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "header\nmore\n" {
			t.Errorf("content = %q, want %q", got, "header\nmore\n")
		}
	})

	t.Run("missing item", func(t *testing.T) {
		col := newTestColumn(t, nil)
		if err := col.AppendText("ghost", "x"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("err = %v, want os.ErrNotExist", err)
		}
	})
}

func TestColumn_PrependText(t *testing.T) {
	t.Parallel()

	col := newTestColumn(t, map[string]string{"task": "original\n"})
	if err := col.PrependText("task", "before"); err != nil {
		t.Fatalf("PrependText: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(col.Path, "task.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "before\noriginal\n" {
		t.Errorf("content = %q, want %q", got, "before\noriginal\n")
	}

	if err := col.PrependText("ghost", "x"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}

func TestColumn_ReplaceFile(t *testing.T) {
	t.Parallel()

	col := newTestColumn(t, map[string]string{"task": "original\nlines\n"})

	if err := col.ReplaceFile("task", "fresh content"); err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(col.Path, "task.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fresh content\n" {
		t.Errorf("content = %q, want %q", got, "fresh content\n")
	}

	if err := col.ReplaceFile("task", "already terminated\n"); err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}
	got, err = os.ReadFile(filepath.Join(col.Path, "task.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "already terminated\n" {
		t.Errorf("content = %q, want %q", got, "already terminated\n")
	}

	if err := col.ReplaceFile("ghost", "x"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}

func TestColumn_JournalText(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, map[string]string{"log": ""})
	if err := col.JournalText("log", time.Now(), "did the thing"); err != nil {
		t.Fatalf("JournalText: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(col.Path, "log.md"))
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} - did the thing\n$`)
	if !pattern.Match(got) {
		t.Errorf("content = %q does not match journal pattern", got)
	}
}

func TestColumn_CopyContent(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, map[string]string{"task": "payload"})
	got, err := col.CopyContent("task")
	if err != nil {
		t.Fatalf("CopyContent: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("content = %q, want %q", got, "payload")
	}
	if _, err := col.CopyContent("ghost"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}

func TestColumn_RenameItem(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		col := newTestColumn(t, map[string]string{"old-name": "x"})
		if err := col.RenameItem("old-name", "new-name"); err != nil {
			t.Fatalf("RenameItem: %v", err)
		}
		if _, err := os.Stat(filepath.Join(col.Path, "old-name.md")); !os.IsNotExist(err) {
			t.Error("old file still exists")
		}
		if _, err := os.Stat(filepath.Join(col.Path, "new-name.md")); err != nil {
			t.Errorf("new file missing: %v", err)
		}
		names := itemNames(col.Items)
		if len(names) != 1 || names[0] != "new-name" {
			t.Errorf("Items = %v, want [new-name]", names)
		}
	})

	t.Run("collision", func(t *testing.T) {
		col := newTestColumn(t, map[string]string{"a": "1", "b": "2"})
		if err := col.RenameItem("a", "b"); !errors.Is(err, os.ErrExist) {
			t.Errorf("err = %v, want os.ErrExist", err)
		}
		// Original still in place.
		if _, err := os.Stat(filepath.Join(col.Path, "a.md")); err != nil {
			t.Errorf("source file removed despite collision: %v", err)
		}
	})

	t.Run("missing item", func(t *testing.T) {
		col := newTestColumn(t, nil)
		if err := col.RenameItem("ghost", "new"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("err = %v, want os.ErrNotExist", err)
		}
	})
}

func TestColumn_Rename(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		col := newTestColumn(t, map[string]string{"task": "x"})
		oldName := col.Name
		newName := oldName + "-renamed"
		if err := col.Rename(newName); err != nil {
			t.Fatalf("Rename: %v", err)
		}
		if col.Name != newName {
			t.Errorf("col.Name = %q, want %q", col.Name, newName)
		}
		if filepath.Base(col.Path) != newName {
			t.Errorf("col.Path = %q, want base %q", col.Path, newName)
		}
		if _, err := os.Stat(col.Path); err != nil {
			t.Errorf("new dir missing: %v", err)
		}
		// Item still loaded after directory move.
		if col.TotalCount() != 1 {
			t.Errorf("TotalCount = %d, want 1", col.TotalCount())
		}
	})

	t.Run("collision", func(t *testing.T) {
		col := newTestColumn(t, nil)
		// Create a sibling directory we'll try to collide with.
		sibling := filepath.Join(filepath.Dir(col.Path), "sibling")
		if err := os.Mkdir(sibling, 0755); err != nil {
			t.Fatal(err)
		}
		if err := col.Rename("sibling"); !errors.Is(err, os.ErrExist) {
			t.Errorf("err = %v, want os.ErrExist", err)
		}
	})
}

func TestColumn_PinItem(t *testing.T) {
	t.Parallel()

	t.Run("pin previously unpinned", func(t *testing.T) {
		col := newTestColumn(t, map[string]string{"task": "x"})
		if err := col.PinItem("task"); err != nil {
			t.Fatalf("PinItem: %v", err)
		}
		// The file name is unchanged; pinning is a frontmatter edit.
		if _, err := os.Stat(filepath.Join(col.Path, "task.md")); err != nil {
			t.Errorf("item file missing: %v", err)
		}
		raw, _ := os.ReadFile(filepath.Join(col.Path, "task.md"))
		if !strings.Contains(string(raw), "pinned: true") {
			t.Errorf("file = %q, want it to contain `pinned: true`", raw)
		}
		if col.TotalCount() != 1 || !col.Items[0].Pinned {
			t.Errorf("Items = %+v, want one pinned", col.Items)
		}
		if sel := col.SelectedItem(); sel == nil || sel.Name != "task" {
			t.Errorf("selected = %v, want the pinned item to stay selected", sel)
		}
	})

	t.Run("unpin previously pinned", func(t *testing.T) {
		col := newTestColumn(t, map[string]string{"urgent": "---\npinned: true\n---\nx"})
		// After LoadItems, the item appears Pinned=true from its frontmatter.
		if !col.Items[0].Pinned {
			t.Fatalf("setup: Items[0].Pinned = false, want true")
		}
		if err := col.PinItem("urgent"); err != nil {
			t.Fatalf("PinItem: %v", err)
		}
		raw, _ := os.ReadFile(filepath.Join(col.Path, "urgent.md"))
		if strings.Contains(string(raw), "pinned") {
			t.Errorf("file = %q, want the pinned key removed", raw)
		}
		if col.Items[0].Pinned {
			t.Error("Items[0].Pinned = true, want false")
		}
	})

	t.Run("pinned item stays selected after re-sort", func(t *testing.T) {
		// zeta sorts last; pinning it should move it to the top and the cursor
		// should follow it there.
		col := newTestColumn(t, map[string]string{"alpha": "x", "mid": "x", "zeta": "x"})
		col.SelectByName("zeta")
		if err := col.PinItem("zeta"); err != nil {
			t.Fatalf("PinItem: %v", err)
		}
		if col.Items[0].Name != "zeta" {
			t.Errorf("Items[0] = %q, want zeta on top", col.Items[0].Name)
		}
		if sel := col.SelectedItem(); sel == nil || sel.Name != "zeta" {
			t.Errorf("selected = %v, want zeta to follow to its new position", sel)
		}
	})

	t.Run("missing item", func(t *testing.T) {
		col := newTestColumn(t, nil)
		if err := col.PinItem("ghost"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("err = %v, want os.ErrNotExist", err)
		}
	})
}

func TestColumn_MoveItemTo(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		src := newTestColumn(t, map[string]string{"task": "payload"})
		dst := newTestColumn(t, nil)
		if err := src.MoveItemTo(dst, "task"); err != nil {
			t.Fatalf("MoveItemTo: %v", err)
		}
		if _, err := os.Stat(filepath.Join(src.Path, "task.md")); !os.IsNotExist(err) {
			t.Error("source file still exists")
		}
		if _, err := os.Stat(filepath.Join(dst.Path, "task.md")); err != nil {
			t.Errorf("dest file missing: %v", err)
		}
		if src.TotalCount() != 0 {
			t.Errorf("src TotalCount = %d, want 0", src.TotalCount())
		}
		if dst.TotalCount() != 1 {
			t.Errorf("dst TotalCount = %d, want 1", dst.TotalCount())
		}
	})

	t.Run("missing item", func(t *testing.T) {
		src := newTestColumn(t, nil)
		dst := newTestColumn(t, nil)
		if err := src.MoveItemTo(dst, "ghost"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("err = %v, want os.ErrNotExist", err)
		}
	})

	t.Run("name collision refuses without overwriting", func(t *testing.T) {
		src := newTestColumn(t, map[string]string{"task": "src payload"})
		dst := newTestColumn(t, map[string]string{"task": "dst payload"})
		if err := src.MoveItemTo(dst, "task"); !errors.Is(err, os.ErrExist) {
			t.Errorf("err = %v, want os.ErrExist", err)
		}
		// Both files must remain untouched.
		if got, err := os.ReadFile(filepath.Join(src.Path, "task.md")); err != nil || string(got) != "src payload" {
			t.Errorf("src task.md = %q, %v; want %q", got, err, "src payload")
		}
		if got, err := os.ReadFile(filepath.Join(dst.Path, "task.md")); err != nil || string(got) != "dst payload" {
			t.Errorf("dst task.md = %q, %v; want %q", got, err, "dst payload")
		}
	})
}

func TestColumn_SelectByName(t *testing.T) {
	t.Parallel()

	col := newTestColumn(t, map[string]string{"alpha": "a", "bravo": "b", "charlie": "c"})

	col.SelectByName("bravo")
	if sel := col.SelectedItem(); sel == nil || sel.Name != "bravo" {
		t.Errorf("SelectedItem = %+v, want bravo", sel)
	}

	// Unknown name leaves the current selection unchanged.
	col.SelectByName("missing")
	if sel := col.SelectedItem(); sel == nil || sel.Name != "bravo" {
		t.Errorf("SelectedItem after missing = %+v, want bravo", sel)
	}
}

func TestColumn_FullPathFor(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, map[string]string{"task": "x"})
	got := col.fullPathFor("task")
	want := filepath.Join(col.Path, "task.md")
	if got != want {
		t.Errorf("fullPathFor = %q, want %q", got, want)
	}
	if got := col.fullPathFor("ghost"); got != "" {
		t.Errorf("fullPathFor(ghost) = %q, want empty", got)
	}
}

func TestColumn_VisibleItems_NoFilter(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, map[string]string{"alpha": "", "bravo": ""})
	visible := col.VisibleItems()
	if len(visible) != 2 {
		t.Fatalf("VisibleItems len = %d, want 2", len(visible))
	}
	names := itemNames(visible)
	want := []string{"alpha", "bravo"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("VisibleItems[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestColumn_HasSelectedItem(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		col := newTestColumn(t, nil)
		if col.HasSelectedItem() {
			t.Error("HasSelectedItem = true on empty column")
		}
		if col.SelectedItem() != nil {
			t.Error("SelectedItem != nil on empty column")
		}
	})

	t.Run("populated", func(t *testing.T) {
		col := newTestColumn(t, map[string]string{"task": "x"})
		if !col.HasSelectedItem() {
			t.Error("HasSelectedItem = false on populated column")
		}
		sel := col.SelectedItem()
		if sel == nil || sel.Name != "task" {
			t.Errorf("SelectedItem = %+v, want task", sel)
		}
	})
}

func TestColumn_NewColumn_Defaults(t *testing.T) {
	t.Parallel()
	col := NewColumn("name", "/path", ItemOptions{PreviewLines: 3})
	if col.Name != "name" || col.Path != "/path" {
		t.Errorf("col = %+v, want Name=name Path=/path", col)
	}
	if col.IsFiltering() {
		t.Error("IsFiltering = true on fresh column")
	}
}

// guard that newTestColumn doesn't accidentally produce a column with stray files.
func TestNewTestColumn_HelperIsClean(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, nil)
	entries, err := os.ReadDir(col.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("temp dir has %d entries: %v", len(entries), strings.Join(names, ", "))
	}
}

// scrollbarLines renders the column's current scrollbar gutter and returns its
// rows, so tests can locate the thumb (┃) relative to the track (│).
func scrollbarLines(col *Column) []string {
	offset, vp, total := col.list.ScrollMetrics()
	height := lipgloss.Height(col.list.View())
	bar := col.renderScrollbar(offset, vp, total, height, col.list.HeaderLines(), true)
	return strings.Split(bar, "\n")
}

func TestColumn_Scrollbar_ThumbMovesTopToBottom(t *testing.T) {
	t.Parallel()
	files := make(map[string]string, 50)
	for i := range 50 {
		files["f"+string(rune('a'+i%26))+string(rune('a'+i/26))] = "x"
	}
	col := newTestColumn(t, files)
	// Cards are 5 rows tall; a 12-row viewport shows ~2, leaving the rest below.
	col.SetHeight(12)
	// Trigger a render so the viewport lays out and computes its scroll bounds.
	_ = col.View(RenderCtx{Active: true, Width: 32, GutterW: 2, MnemonicOf: func(string) string { return "" }})

	lines := scrollbarLines(col)
	if !strings.Contains(strings.Join(lines, ""), "┃") {
		t.Fatalf("expected a thumb (┃) when overflowing, got %q", lines)
	}
	if !strings.Contains(lines[0], "┃") {
		t.Errorf("expected thumb at the top on first page, got %q", lines)
	}
	if strings.Contains(lines[len(lines)-1], "┃") {
		t.Errorf("unexpected thumb at the bottom on first page, got %q", lines)
	}

	// Jump to the end and re-render.
	col.SelectLast()
	_ = col.View(RenderCtx{Active: true, Width: 32, GutterW: 2, MnemonicOf: func(string) string { return "" }})

	lines = scrollbarLines(col)
	if !strings.Contains(lines[len(lines)-1], "┃") {
		t.Errorf("expected thumb at the bottom on last page, got %q", lines)
	}
	if strings.Contains(lines[0], "┃") {
		t.Errorf("unexpected thumb at the top on last page, got %q", lines)
	}
}

func TestColumn_Scrollbar_BlankWhenAllFit(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, map[string]string{"a": "x", "b": "y"})
	col.SetHeight(40)
	_ = col.View(RenderCtx{Active: true, Width: 32, GutterW: 2, MnemonicOf: func(string) string { return "" }})

	bar := strings.Join(scrollbarLines(col), "")
	if strings.Contains(bar, "┃") || strings.Contains(bar, "│") {
		t.Errorf("expected a blank gutter when everything fits, got %q", bar)
	}
}

func TestColumn_View_EmptyColumnHeightMatchesPopulatedColumn(t *testing.T) {
	t.Parallel()

	ctx := RenderCtx{Active: true, Width: 32, GutterW: 2, MnemonicOf: func(string) string { return "" }}
	populated := newTestColumn(t, map[string]string{"task": "body"})
	empty := newTestColumn(t, nil)
	virtualEmpty := NewVirtualColumn("tasks", "Tasks", DarkPalette())
	virtualEmpty.ApplyVirtualSpec(events.VirtualColumnSpec{Name: "Tasks", Empty: "no open tasks"})

	for _, col := range []*Column{populated, empty, virtualEmpty} {
		col.SetHeight(12)
	}

	populatedH := lipgloss.Height(populated.View(ctx))
	if emptyH := lipgloss.Height(empty.View(ctx)); emptyH != populatedH {
		t.Fatalf("empty column height = %d, want populated height %d", emptyH, populatedH)
	}
	virtualView := virtualEmpty.View(ctx)
	if virtualH := lipgloss.Height(virtualView); virtualH != populatedH {
		t.Fatalf("virtual empty column height = %d, want populated height %d", virtualH, populatedH)
	}
	if !strings.Contains(virtualView, "no open tasks") {
		t.Fatalf("virtual empty placeholder missing:\n%s", virtualView)
	}
}

func TestColumn_View_Frontmatter(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, map[string]string{
		"task": "---\nicon: \"♞\"\nmeta: custom meta line\ntags: [urgent, backend]\n---\nbody line",
	})
	col.SetHeight(40)

	view := col.View(RenderCtx{Active: true, Width: 60, GutterW: 2, PreviewLines: 3})
	for _, want := range []string{"♞", "custom meta line", "#urgent", "#backend"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	// The frontmatter meta replaces the mtime/size line.
	if strings.Contains(view, "just now") {
		t.Errorf("view must not show the filesystem meta when frontmatter meta is set:\n%s", view)
	}
	// Frontmatter never leaks into the preview.
	if strings.Contains(view, "icon:") || strings.Contains(view, "tags:") {
		t.Errorf("frontmatter leaked into the preview:\n%s", view)
	}
	if !strings.Contains(view, "body line") {
		t.Errorf("view missing preview body:\n%s", view)
	}
}

func TestItemHeight_RenderLineAddsRow(t *testing.T) {
	t.Parallel()
	plain := Item{Name: "a"}
	withRender := Item{Name: "b", Render: []string{"priority"}, Data: map[string]any{"priority": "high"}}
	sep := Item{Name: "s", Separator: true, Render: []string{"priority"}}

	cfg := renderConfig{previewLines: 3}
	if got, want := itemHeight(plain, cfg), cardRows(3); got != want {
		t.Errorf("itemHeight(plain) = %d, want %d", got, want)
	}
	if got, want := itemHeight(withRender, cfg), cardRows(3)+1; got != want {
		t.Errorf("itemHeight(withRender) = %d, want %d (one taller)", got, want)
	}
	if got, want := itemHeight(sep, cfg), cardRows(3); got != want {
		t.Errorf("itemHeight(separator) = %d, want %d (separators ignore render)", got, want)
	}
}

func TestWrapTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		title    string
		width    int
		maxLines int
		wrap     bool
		want     []string
	}{
		{"disabled truncates", "the quick brown fox", 9, 2, false, []string{"the quic…"}},
		{"maxLines<=1 truncates", "the quick brown fox", 9, 1, true, []string{"the quic…"}},
		{"fits one line", "short", 20, 2, true, []string{"short"}},
		{"wraps on word boundary", "the quick brown fox", 10, 2, true, []string{"the quick", "brown fox"}},
		{"caps and ellipsizes overflow", "alpha beta gamma delta", 6, 2, true, []string{"alpha", "beta …"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := wrapTitle(tc.title, tc.width, tc.maxLines, tc.wrap)
			if len(got) > tc.maxLines && tc.wrap && tc.maxLines > 1 {
				t.Fatalf("wrapTitle returned %d rows, exceeds maxLines %d: %q", len(got), tc.maxLines, got)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("wrapTitle(%q, %d, %d, %v) = %q, want %q", tc.title, tc.width, tc.maxLines, tc.wrap, got, tc.want)
			}
		})
	}
}

func TestItemHeight_WrappedTitleAddsRows(t *testing.T) {
	t.Parallel()
	// colWidth 20: innerW = 18, gutter 2 → titleWidth = 16.
	long := Item{Name: "a", Title: "this title is definitely much longer than one row"}
	wrapped := renderConfig{previewLines: 1, gutterW: 2, colWidth: 20, wrapTitles: true, titleMaxLines: 2}
	off := renderConfig{previewLines: 1, gutterW: 2, colWidth: 20, wrapTitles: false, titleMaxLines: 2}

	if got, want := itemHeight(long, wrapped), cardRows(1)+1; got != want {
		t.Errorf("wrapped itemHeight = %d, want %d (one extra title row, capped at 2)", got, want)
	}
	if got, want := itemHeight(long, off), cardRows(1); got != want {
		t.Errorf("wrap-off itemHeight = %d, want %d (single-line regression guard)", got, want)
	}
	// Separators never grow on a long title.
	sep := Item{Name: "s", Title: "this title is definitely much longer than one row", Separator: true}
	if got, want := itemHeight(sep, wrapped), cardRows(1); got != want {
		t.Errorf("separator itemHeight = %d, want %d", got, want)
	}
}

func TestColumn_View_WrappedCardHeightMatchesItemHeight(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, map[string]string{
		"longone": "body",
	})
	col.SetHeight(40)
	// Force a long title so it must wrap within the column width.
	col.Items[0].Title = "a deliberately long card title that cannot fit on one row"

	ctx := RenderCtx{Active: true, Width: 24, GutterW: 2, PreviewLines: 1, WrapTitles: true, TitleMaxLines: 2}
	_ = col.View(ctx)

	cfg := renderConfig{previewLines: 1, gutterW: 2, colWidth: 24 - scrollGutterW, wrapTitles: true, titleMaxLines: 2}
	declared := itemHeight(col.Items[0], cfg)
	drawn := lipgloss.Height(renderItem(col.Items[0], false, cfg))
	if declared != drawn {
		t.Errorf("declared itemHeight %d != drawn card height %d (sync invariant broken)", declared, drawn)
	}
	if declared <= cardRows(1) {
		t.Errorf("expected wrapped card taller than base %d, got %d", cardRows(1), declared)
	}
}

func TestItemHeight_VirtualWrappedTitleMatchesDraw(t *testing.T) {
	t.Parallel()
	// Virtual (script-supplied) cards wrap their titles too; the declared and
	// drawn heights must stay in sync exactly as they do for regular cards.
	v := Item{Name: "v", Virtual: true, Title: "a long virtual card title that overflows one row", Meta: "x"}
	cfg := renderConfig{previewLines: 1, gutterW: 2, colWidth: 24, wrapTitles: true, titleMaxLines: 2}

	declared := itemHeight(v, cfg)
	drawn := lipgloss.Height(renderItem(v, false, cfg))
	if declared != drawn {
		t.Errorf("virtual declared itemHeight %d != drawn %d", declared, drawn)
	}
	if declared != cardRows(1)+1 {
		t.Errorf("virtual itemHeight = %d, want %d (one extra title row)", declared, cardRows(1)+1)
	}
}

func TestColumn_View_RenderLine(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, map[string]string{
		"task":  "---\npriority: high\nrender: [priority]\n---\nbody",
		"plain": "no frontmatter body",
	})
	col.SetHeight(40)

	view := col.View(RenderCtx{Active: true, Width: 60, GutterW: 2, PreviewLines: 3})
	if !strings.Contains(view, "priority: high") {
		t.Errorf("view missing render line 'priority: high':\n%s", view)
	}
	// The card that declares render: is one row taller (the required variable
	// height). Items are sorted by name, so "plain" precedes "task".
	for _, it := range col.Items {
		switch it.Name {
		case "task":
			if got, want := itemHeight(it, renderConfig{previewLines: 3}), cardRows(3)+1; got != want {
				t.Errorf("task itemHeight = %d, want %d", got, want)
			}
		case "plain":
			if got, want := itemHeight(it, renderConfig{previewLines: 3}), cardRows(3); got != want {
				t.Errorf("plain itemHeight = %d, want %d", got, want)
			}
		}
	}
}

func TestColumn_View_BadFrontmatterBadge(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, map[string]string{
		"broken": "---\ntags: [unclosed\n---\nbody line",
		"fine":   "plain body",
	})
	col.SetHeight(40)

	view := col.View(RenderCtx{Active: true, Width: 60, GutterW: 2, PreviewLines: 3})
	if !strings.Contains(view, "⚠ yaml") {
		t.Errorf("view missing ⚠ yaml badge for malformed frontmatter:\n%s", view)
	}
	if strings.Count(view, "⚠ yaml") != 1 {
		t.Errorf("⚠ yaml badge should appear exactly once:\n%s", view)
	}
}

func TestColumn_View_PreviewDensity(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, map[string]string{
		"task": "first preview line\nsecond preview line\nthird preview line",
	})
	col.SetHeight(40)

	compact := col.View(RenderCtx{Active: true, Width: 60, GutterW: 2, PreviewLines: 1})
	if !strings.Contains(compact, "first preview line") {
		t.Errorf("compact view missing first preview line:\n%s", compact)
	}
	if strings.Contains(compact, "second preview line") {
		t.Errorf("compact view must not show second preview line:\n%s", compact)
	}

	zoomed := col.View(RenderCtx{Active: true, Width: 60, GutterW: 2, PreviewLines: 4})
	for _, want := range []string{"first preview line", "second preview line", "third preview line"} {
		if !strings.Contains(zoomed, want) {
			t.Errorf("zoomed view missing %q:\n%s", want, zoomed)
		}
	}
}

// TestCollapse_AutoExpandWidth locks the auto-expand-on-focus rule: a column
// marked collapsed renders thin only while it is *not* the selection. The
// columns-region width rule
// is the single seam layout reads, so testing it covers packing/geometry too.
func TestCollapse_AutoExpandWidth(t *testing.T) {
	t.Parallel()
	a := NewColumn("A", "", ItemOptions{})
	b := NewColumn("B", "", ItemOptions{})
	b.Collapsed = true
	board := &Board{
		columns:     []*Column{a, b},
		selectedCol: 0,
		cfg:         config.Config{ColumnWidth: 30},
	}

	region := boardColumnsRegion{}
	if got := region.colWidthOf(board, 1); got != collapsedContentWidth {
		t.Fatalf("collapsed + unfocused width = %d, want %d", got, collapsedContentWidth)
	}
	board.selectedCol = 1 // focus the collapsed column
	if got := region.colWidthOf(board, 1); got != 30 {
		t.Fatalf("collapsed + focused width = %d, want 30 (auto-expand)", got)
	}
	if got := region.colWidthOf(board, 0); got != 30 {
		t.Fatalf("ordinary column width = %d, want 30", got)
	}
}

// TestCollapse_VerticalBar checks the collapsed bar stacks the name top-to-bottom
// and shows the item count.
func TestCollapse_VerticalBar(t *testing.T) {
	t.Parallel()
	c := newTestColumn(t, map[string]string{"one": "x", "two": "y", "three": "z"})
	c.Name = "Done"
	c.SetHeight(8)

	out := c.viewCollapsed(RenderCtx{Width: collapsedContentWidth})

	// Name runs one rune per line, so each letter appears on its own row.
	for _, r := range "DONE" {
		if !strings.Contains(out, string(r)) {
			t.Fatalf("collapsed bar missing name rune %q:\n%s", r, out)
		}
	}
	if !strings.Contains(out, "3") {
		t.Fatalf("collapsed bar missing item count:\n%s", out)
	}
	// Every row is the bar's content width plus the two border cells, so no line
	// should exceed that.
	maxW := collapsedContentWidth + 2
	for line := range strings.SplitSeq(out, "\n") {
		if w := lipgloss.Width(line); w > maxW {
			t.Fatalf("collapsed bar line too wide (%d, want <=%d): %q", w, maxW, line)
		}
	}
}

// TestCollapse_Persist round-trips the collapse intent through the column's
// colstore so it survives a rebuild.
func TestCollapse_Persist(t *testing.T) {
	t.Parallel()
	c := newTestColumn(t, map[string]string{"a": "x"})

	c.ToggleCollapse() // -> true, persisted
	if !c.Collapsed {
		t.Fatal("ToggleCollapse did not set Collapsed")
	}

	// A fresh column over the same dir restores the intent.
	reopened := NewColumn(c.Name, c.Path, ItemOptions{})
	reopened.RestoreCollapsed()
	if !reopened.Collapsed {
		t.Fatal("RestoreCollapsed did not load persisted collapse")
	}

	c.ToggleCollapse() // -> false, persisted
	reopened2 := NewColumn(c.Name, c.Path, ItemOptions{})
	reopened2.RestoreCollapsed()
	if reopened2.Collapsed {
		t.Fatal("RestoreCollapsed should have loaded expanded state")
	}
}

// TestCollapse_VirtualSessionOnly confirms a virtual column (no backing dir)
// toggles in-session without attempting persistence.
func TestCollapse_VirtualSessionOnly(t *testing.T) {
	t.Parallel()
	v := NewVirtualColumn("vid", "Virtual", DarkPalette())
	v.ToggleCollapse()
	if !v.Collapsed {
		t.Fatal("virtual column did not toggle collapse")
	}
	v.RestoreCollapsed() // no dir: must be a no-op, leaving the session state
	if !v.Collapsed {
		t.Fatal("RestoreCollapsed clobbered virtual column session state")
	}
}

// TestCollapseFocusShift covers the pure index rule: collapsing shifts focus to
// a neighbour (previous at the right edge, otherwise next), and stays put with a
// lone column.
func TestCollapseFocusShift(t *testing.T) {
	t.Parallel()
	cases := []struct{ selected, n, want int }{
		{0, 3, 1}, // left/middle -> next
		{1, 3, 2}, // middle -> next
		{2, 3, 1}, // right edge -> previous
		{0, 1, 0}, // lone column -> stays
	}
	for _, c := range cases {
		if got := collapseFocusShift(c.selected, c.n); got != c.want {
			t.Errorf("collapseFocusShift(%d,%d) = %d, want %d", c.selected, c.n, got, c.want)
		}
	}
}

// TestCollapse_ToggleKeyShiftsFocus drives the "|" key end-to-end: folding the
// focused column reveals the bar at once by shifting focus to a neighbour, and
// re-expanding keeps focus on the now-open column.
func TestCollapse_ToggleKeyShiftsFocus(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 3, 3)
	pipe := keyPressText("|")

	// Collapse the left column: focus advances off the bar to the next one.
	b.handleKey(pipe)
	if !b.columns[0].Collapsed {
		t.Fatal("| should collapse the focused column")
	}
	if b.selectedCol != 1 {
		t.Fatalf("selectedCol = %d, want 1 (focus shifted off the bar)", b.selectedCol)
	}

	// Re-expand the same column: focus stays on it, now open.
	b.selectedCol = 0 // focusing a collapsed column auto-expands it transiently
	b.handleKey(pipe)
	if b.columns[0].Collapsed {
		t.Fatal("| should expand the collapsed focused column")
	}
	if b.selectedCol != 0 {
		t.Fatalf("selectedCol = %d, want 0 (focus stays on the expanded column)", b.selectedCol)
	}
}

// TestCollapse_ScriptSelectExpands locks that a scripted SelectItem opens a
// collapsed target column (and doesn't overwrite the persisted preference).
func TestCollapse_ScriptSelectExpands(t *testing.T) {
	t.Parallel()
	colA := newTestColumn(t, map[string]string{"a1": "x"})
	colB := newTestColumn(t, map[string]string{"b1": "x", "b2": "y"})
	colB.ToggleCollapse() // collapsed + persisted
	b := &Board{columns: []*Column{colA, colB}, selectedCol: 0}
	api := boardScriptAPI{b: b}

	if err := api.SelectItem(colB.Name, "b2"); err != nil {
		t.Fatalf("SelectItem: %v", err)
	}
	if colB.Collapsed {
		t.Fatal("scripted SelectItem should expand the collapsed column")
	}
	// Expand is session-only: the user's saved preference is untouched.
	reopened := NewColumn(colB.Name, colB.Path, ItemOptions{})
	reopened.RestoreCollapsed()
	if !reopened.Collapsed {
		t.Fatal("scripted expand must not overwrite the persisted collapse preference")
	}
}
