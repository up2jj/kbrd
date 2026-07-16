package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/harpoon"
)

func TestHarpoonViewListsFiveSlots(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	b := boardWithNCols(t, 1, 1)
	b.handleKey(keyPressText("h"))

	view := b.View().Content
	if !strings.Contains(view, "Harpoon") {
		t.Fatalf("harpoon title missing:\n%s", view)
	}
	if got := strings.Count(view, "empty"); got != 5 {
		t.Fatalf("empty slots rendered = %d, want 5:\n%s", got, view)
	}
}

func TestHarpoonAssignAndJumpSelectsSavedFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	b := boardWithNCols(t, 2, 2)
	writeColItem(t, b.columns[0], "first")
	writeColItem(t, b.columns[1], "target")
	b.selectedCol = 1
	b.columns[1].SelectByName("target")

	b.handleKey(keyPressText("h"))
	if !b.harpoon.Active() {
		t.Fatal("h did not open harpoon")
	}
	b.handleKey(keyPressText("a"))
	if got := b.harpoon.SelectedPath(); !samePath(got, filepath.Join(b.columns[1].Path, "target.md")) {
		t.Fatalf("assigned path = %q, want target file", got)
	}

	b.selectedCol = 0
	b.columns[0].SelectByName("first")
	b.handleKey(keyPressText("1"))
	if b.harpoon.Active() {
		t.Fatal("jump did not close harpoon")
	}
	if b.selectedCol != 1 {
		t.Fatalf("selected column = %d, want 1", b.selectedCol)
	}
	if got := b.columns[1].SelectedItem(); got == nil || got.Name != "target" {
		t.Fatalf("selected item = %#v, want target", got)
	}
}

func TestHarpoonJumpExpandsCollapsedTargetWithoutPersisting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	b := boardWithNCols(t, 2, 2)
	writeColItem(t, b.columns[1], "target")
	b.selectedCol = 1
	b.columns[1].SelectByName("target")
	b.handleKey(keyPressText("h"))
	b.handleKey(keyPressText("a"))
	b.columns[1].ToggleCollapse()
	b.selectedCol = 0

	b.handleKey(keyPressText("1"))
	if b.columns[1].Collapsed {
		t.Fatal("harpoon jump left target column collapsed")
	}

	reopened := NewColumn(b.columns[1].Name, b.columns[1].Path, ItemOptions{})
	reopened.RestoreCollapsed()
	if !reopened.Collapsed {
		t.Fatal("harpoon jump overwrote the target column's persisted collapse preference")
	}
}

func TestHarpoonJumpMissingTargetKeepsMenuOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	b := boardWithNCols(t, 1, 1)
	b.handleKey(keyPressText("h"))
	if err := b.harpoon.SetSelected(filepath.Join(b.cfg.Path, "c0", "missing.md")); err != nil {
		t.Fatal(err)
	}

	_, cmd := b.handleKey(keyPressText("1"))
	if cmd == nil {
		t.Fatal("missing target did not report an error")
	}
	if !b.harpoon.Active() {
		t.Fatal("missing target unexpectedly closed harpoon")
	}
}

func TestHarpoonCardIndicatorLoadsAtBoardStartup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	first := boardWithNCols(t, 1, 1)
	writeColItem(t, first.columns[0], "target")
	target := filepath.Join(first.columns[0].Path, "target.md")

	first.handleKey(keyPressText("h"))
	first.handleKey(keyPressText("a"))

	reopened := NewBoard(first.cfg)
	if err := reopened.loadColumns(); err != nil {
		t.Fatal(err)
	}
	setColumnHeights(reopened.columns, 20)
	view := reopened.columns[0].View(RenderCtx{
		Active:       true,
		Width:        reopened.cfg.ColumnWidth,
		PreviewLines: reopened.cfg.PreviewLines,
		GutterW:      2,
		IsHarpooned:  reopened.harpoon.Contains,
	})
	if !reopened.harpoon.Contains(target) {
		t.Fatal("reopened board did not load persisted Harpoon slot")
	}
	if !strings.Contains(view, "[H]") {
		t.Fatalf("reopened board card missing Harpoon marker:\n%s", view)
	}
}

func TestHarpoonWatcherUpdatesMovedPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	b := boardWithNCols(t, 2, 2)
	writeColItem(t, b.columns[0], "tracked")
	oldPath := b.columns[0].Items[0].FullPath
	newPath := filepath.Join(b.columns[1].Path, filepath.Base(oldPath))

	if err := b.harpoon.Open(b.cfg.Path); err != nil {
		t.Fatal(err)
	}
	if err := b.harpoon.SetSelected(oldPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}

	b.updateInner(watchEventMsg{Path: oldPath})
	b.updateInner(watchEventMsg{Path: newPath})
	reload := b.debouncedReload(b.watchSeq)
	if reload == nil {
		t.Fatal("move did not schedule a reload")
	}
	b.updateInner(reload())

	store, err := harpoon.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := store.ForBoard(b.cfg.Path)[0]; !samePath(got, newPath) {
		t.Fatalf("persisted slot = %q, want %q", got, newPath)
	}
	if got := b.harpoon.SelectedPath(); !samePath(got, newPath) {
		t.Fatalf("open menu slot = %q, want %q", got, newPath)
	}
}

func TestHarpoonWatcherLeavesAmbiguousMoveStale(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	b := boardWithNCols(t, 2, 2)
	writeColItem(t, b.columns[0], "tracked")
	oldPath := b.columns[0].Items[0].FullPath
	body, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := b.harpoon.Open(b.cfg.Path); err != nil {
		t.Fatal(err)
	}
	if err := b.harpoon.SetSelected(oldPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(oldPath); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(b.columns[0].Path, "first-copy.md")
	second := filepath.Join(b.columns[1].Path, "second-copy.md")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, path := range []string{oldPath, first, second} {
		b.updateInner(watchEventMsg{Path: path})
	}
	reload := b.debouncedReload(b.watchSeq)
	if reload == nil {
		t.Fatal("changes did not schedule a reload")
	}
	b.updateInner(reload())

	store, err := harpoon.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := store.ForBoard(b.cfg.Path)[0]; !samePath(got, oldPath) {
		t.Fatalf("ambiguous slot = %q, want stale path %q", got, oldPath)
	}
}

func TestHarpoonWatcherUpdatesPathAfterModelMove(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	b := boardWithNCols(t, 2, 2)
	writeColItem(t, b.columns[0], "tracked")
	oldPath := b.columns[0].Items[0].FullPath
	newPath := filepath.Join(b.columns[1].Path, filepath.Base(oldPath))

	if err := b.harpoon.Open(b.cfg.Path); err != nil {
		t.Fatal(err)
	}
	if err := b.harpoon.SetSelected(oldPath); err != nil {
		t.Fatal(err)
	}
	if err := b.moveItem(b.columns[0], b.columns[1], "tracked"); err != nil {
		t.Fatal(err)
	}

	// moveItem has already reloaded both columns before fsnotify is handled.
	for _, path := range []string{oldPath, newPath} {
		b.updateInner(watchEventMsg{Path: path})
	}
	reload := b.debouncedReload(b.watchSeq)
	if reload == nil {
		t.Fatal("move did not schedule a reload")
	}
	b.updateInner(reload())

	store, err := harpoon.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := store.ForBoard(b.cfg.Path)[0]; !samePath(got, newPath) {
		t.Fatalf("persisted slot = %q, want %q", got, newPath)
	}
}

func TestHarpoonWatcherKeepsIdentityAcrossSplitMoveEvents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	b := boardWithNCols(t, 2, 2)
	writeColItem(t, b.columns[0], "tracked")
	oldPath := b.columns[0].Items[0].FullPath
	newPath := filepath.Join(b.columns[1].Path, filepath.Base(oldPath))

	if err := b.harpoon.Open(b.cfg.Path); err != nil {
		t.Fatal(err)
	}
	if err := b.harpoon.SetSelected(oldPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}

	// The source event reloads first, before the destination event arrives.
	b.updateInner(watchEventMsg{Path: oldPath})
	firstReload := b.debouncedReload(b.watchSeq)
	if firstReload == nil {
		t.Fatal("source event did not schedule a reload")
	}
	b.updateInner(firstReload())
	store, err := harpoon.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := store.ForBoard(b.cfg.Path)[0]; !samePath(got, oldPath) {
		t.Fatalf("source-only reload changed slot to %q", got)
	}

	b.updateInner(watchEventMsg{Path: newPath})
	secondReload := b.debouncedReload(b.watchSeq)
	if secondReload == nil {
		t.Fatal("destination event did not schedule a reload")
	}
	b.updateInner(secondReload())
	store, err = harpoon.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := store.ForBoard(b.cfg.Path)[0]; !samePath(got, newPath) {
		t.Fatalf("persisted slot = %q, want %q", got, newPath)
	}
}

func TestHarpoonOpenReconcilesMoveWhileClosed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	b := boardWithNCols(t, 2, 2)
	writeColItem(t, b.columns[0], "tracked")
	oldPath := b.columns[0].Items[0].FullPath
	newPath := filepath.Join(b.columns[1].Path, filepath.Base(oldPath))

	if err := b.harpoon.Open(b.cfg.Path); err != nil {
		t.Fatal(err)
	}
	if err := b.harpoon.SetSelected(oldPath); err != nil {
		t.Fatal(err)
	}
	b.harpoon.Close()
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	for _, col := range b.columns {
		if err := col.LoadItems(); err != nil {
			t.Fatal(err)
		}
	}

	b.harpoonActions().open()
	b.harpoon.Select(0)
	if got := b.harpoon.SelectedPath(); !samePath(got, newPath) {
		t.Fatalf("reopened slot = %q, want %q", got, newPath)
	}
}
