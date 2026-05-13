package model

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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
	col := NewColumn(filepath.Base(dir), dir, 32, 3)
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

	col := NewColumn("c", dir, 32, 3)
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

func TestColumn_LoadItems_MissingDir(t *testing.T) {
	t.Parallel()
	col := NewColumn("c", filepath.Join(t.TempDir(), "nope"), 32, 3)
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

func TestColumn_JournalText(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, map[string]string{"log": ""})
	if err := col.JournalText("log", "did the thing"); err != nil {
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
		if _, err := os.Stat(filepath.Join(col.Path, "p_task.md")); err != nil {
			t.Errorf("pinned file missing: %v", err)
		}
		if _, err := os.Stat(filepath.Join(col.Path, "task.md")); !os.IsNotExist(err) {
			t.Error("unpinned file still exists")
		}
		if col.TotalCount() != 1 || !col.Items[0].Pinned {
			t.Errorf("Items = %+v, want one pinned", col.Items)
		}
	})

	t.Run("unpin previously pinned", func(t *testing.T) {
		col := newTestColumn(t, map[string]string{"p_urgent": "x"})
		// After LoadItems, the item appears with Name "urgent", Pinned=true.
		if err := col.PinItem("urgent"); err != nil {
			t.Fatalf("PinItem: %v", err)
		}
		if _, err := os.Stat(filepath.Join(col.Path, "urgent.md")); err != nil {
			t.Errorf("unpinned file missing: %v", err)
		}
		if _, err := os.Stat(filepath.Join(col.Path, "p_urgent.md")); !os.IsNotExist(err) {
			t.Error("pinned file still exists")
		}
		if col.Items[0].Pinned {
			t.Error("Items[0].Pinned = true, want false")
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
	col := NewColumn("name", "/path", 32, 3)
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
