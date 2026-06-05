package model

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/config"
)

func TestBoard_LoadColumns_SkipsHiddenAndUnderscore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dirs := []string{
		"1. TO DO",
		"2. IN PROGRESS",
		".git",
		"_archive",
	}
	for _, name := range dirs {
		if err := os.Mkdir(filepath.Join(dir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// A stray top-level file shouldn't crash or be considered a column.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	b := NewBoard(config.Config{Path: dir, ColumnWidth: 32, PreviewLines: 3})
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}

	got := make([]string, len(b.columns))
	for i, c := range b.columns {
		got[i] = c.Name
	}
	sort.Strings(got)
	want := []string{"1. TO DO", "2. IN PROGRESS"}
	if len(got) != len(want) {
		t.Fatalf("columns = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("columns[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBoard_LoadColumns_EmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	b := NewBoard(config.Config{Path: dir, ColumnWidth: 32, PreviewLines: 3})
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}
	if len(b.columns) != 0 {
		t.Errorf("columns = %d, want 0", len(b.columns))
	}
}

func TestBoard_CreateDefaultColumns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	b := NewBoard(config.Config{Path: dir, ColumnWidth: 32, PreviewLines: 3})
	if err := b.createDefaultColumns(); err != nil {
		t.Fatalf("createDefaultColumns: %v", err)
	}
	for _, name := range []string{"1. TO DO", "2. IN PROGRESS", "3. DONE"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("missing %q: %v", name, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q is not a directory", name)
		}
	}

	// Re-running is idempotent (MkdirAll is a no-op on existing dirs).
	if err := b.createDefaultColumns(); err != nil {
		t.Errorf("re-run failed: %v", err)
	}

	// After creation, loadColumns should see all three.
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}
	if len(b.columns) != 3 {
		t.Errorf("columns = %d, want 3", len(b.columns))
	}
}

// boardWithNCols builds a board backed by N empty column dirs, sized so that
// only `visibleCols` columns fit on the row.
func boardWithNCols(t *testing.T, n, visibleCols int) *Board {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		name := filepath.Join(dir, "c"+string(rune('0'+i)))
		if err := os.Mkdir(name, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	b := NewBoard(config.Config{Path: dir, ColumnWidth: 20, PreviewLines: 3})
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}
	// slotWidth = ColumnWidth + 3 = 23; indicatorReserve = 6.
	// width = visibleCols * 23 + 6 makes exactly visibleCols fit.
	b.termWidth = visibleCols*b.slotWidth() + 6
	b.termHeight = 40
	b.visibleHeight = 32
	for _, c := range b.columns {
		c.SetHeight(b.visibleHeight)
	}
	return b
}

func TestBoard_VisibleColRange_FitsAndPansOnSelection(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 10, 3)

	first, count := b.visibleColRange()
	if first != 0 || count != 3 {
		t.Fatalf("initial range = (%d,%d), want (0,3)", first, count)
	}

	b.selectedCol = 7
	first, count = b.visibleColRange()
	if first != 5 || count != 3 {
		t.Fatalf("after selecting col 7, range = (%d,%d), want (5,3)", first, count)
	}

	// View() should mention "◀ 5" (5 hidden left) and "2 ▶" (2 hidden right).
	out := b.View()
	if !strings.Contains(out, "◀ 5") {
		t.Errorf("View() missing left indicator '◀ 5':\n%s", out)
	}
	if !strings.Contains(out, "2 ▶") {
		t.Errorf("View() missing right indicator '2 ▶':\n%s", out)
	}
}

func TestBoard_PanKeysMoveWindow(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 10, 3)
	// Force window initialization.
	b.visibleColRange()
	if b.firstVisibleCol != 0 {
		t.Fatalf("firstVisibleCol = %d, want 0", b.firstVisibleCol)
	}
	b.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	if b.firstVisibleCol != 1 {
		t.Errorf("after L, firstVisibleCol = %d, want 1", b.firstVisibleCol)
	}
	b.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")})
	if b.firstVisibleCol != 0 {
		t.Errorf("after H, firstVisibleCol = %d, want 0", b.firstVisibleCol)
	}
}

func TestBoard_View_TinyTerminalShortCircuits(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 3, 1)
	b.termWidth = 20 // < ColumnWidth + 4 = 24
	b.termHeight = 24
	out := b.View()
	if !strings.Contains(out, "terminal too small") {
		t.Errorf("expected too-small placeholder, got:\n%s", out)
	}

	b.termWidth = 200
	b.termHeight = 5
	out = b.View()
	if !strings.Contains(out, "terminal too small") {
		t.Errorf("expected too-small placeholder for short terminal, got:\n%s", out)
	}
}

func TestBoard_WindowSizeMsg_ClampsVisibleHeight(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 2, 2)
	b.Update(tea.WindowSizeMsg{Width: 200, Height: 3})
	if b.visibleHeight < 1 {
		t.Errorf("visibleHeight = %d, want >= 1", b.visibleHeight)
	}
}

func writeColItem(t *testing.T, col *Column, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(col.Path, name+".md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := col.LoadItems(); err != nil {
		t.Fatal(err)
	}
}

func columnHasItem(col *Column, name string) bool {
	for _, it := range col.Items {
		if it.Name == name {
			return true
		}
	}
	return false
}

func TestBoard_MMovesItemToFirstColumn(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 3, 3)
	writeColItem(t, b.columns[2], "task")
	b.selectedCol = 2

	b.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("M")})

	if b.selectedCol != 0 {
		t.Errorf("selectedCol = %d, want 0", b.selectedCol)
	}
	if !columnHasItem(b.columns[0], "task") {
		t.Errorf("first column missing item 'task'; items=%v", b.columns[0].Items)
	}
	if len(b.columns[2].Items) != 0 {
		t.Errorf("source column not empty: %v", b.columns[2].Items)
	}
	if _, err := os.Stat(filepath.Join(b.columns[0].Path, "task.md")); err != nil {
		t.Errorf("file not at destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(b.columns[2].Path, "task.md")); !os.IsNotExist(err) {
		t.Errorf("file still at source (err=%v)", err)
	}
}

func TestBoard_MOnFirstColumnIsNoop(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 3, 3)
	writeColItem(t, b.columns[0], "task")
	b.selectedCol = 0

	b.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("M")})

	if b.selectedCol != 0 {
		t.Errorf("selectedCol = %d, want 0", b.selectedCol)
	}
	if !columnHasItem(b.columns[0], "task") {
		t.Errorf("item missing from first column after no-op M")
	}
	if _, err := os.Stat(filepath.Join(b.columns[0].Path, "task.md")); err != nil {
		t.Errorf("file missing at first column: %v", err)
	}
}

func TestBoard_MWithoutSelectionDoesNothing(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 3, 3)
	b.selectedCol = 1

	b.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("M")})

	if b.selectedCol != 1 {
		t.Errorf("selectedCol = %d, want 1 (unchanged)", b.selectedCol)
	}
}

func TestBoard_ReloadPreservesSelection(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 3, 3)
	writeColItem(t, b.columns[0], "alpha")
	writeColItem(t, b.columns[0], "beta")
	writeColItem(t, b.columns[0], "gamma")
	b.columns[0].SelectByName("beta")

	fresh, err := buildColumns(b.cfg, b.palette, b.itemsByPath())
	if err != nil {
		t.Fatalf("buildColumns: %v", err)
	}
	b.applyReloadedColumns(fresh)

	if got := b.columns[0].SelectedItem(); got == nil || got.Name != "beta" {
		t.Fatalf("selection after reload = %v, want beta", got)
	}
}

func TestBoard_ColumnReloadPreservesSelection(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 3, 3)
	writeColItem(t, b.columns[1], "one")
	writeColItem(t, b.columns[1], "two")
	b.columns[1].SelectByName("two")

	fresh := buildColumn(b.columns[1].Path, b.columns[1].Name, b.cfg, b.palette, b.itemsByPath())
	b.Update(columnReloadedMsg{Seq: b.watchSeq, path: b.columns[1].Path, col: fresh})

	if got := b.columns[1].SelectedItem(); got == nil || got.Name != "two" {
		t.Fatalf("selection after column reload = %v, want two", got)
	}
}

func TestBoard_ZoomToggleAndFollowSelection(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 4, 2)

	plus := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("+")}
	b.handleKey(plus)
	if !b.zoom.Active() {
		t.Fatal("+ should activate zoom")
	}

	// Zoom renders only the selected column, with no pan indicators.
	out := b.View()
	if strings.Contains(out, "◀") || strings.Contains(out, "▶") {
		t.Errorf("zoomed view must not show pan indicators:\n%s", out)
	}
	if !strings.Contains(out, "C0") || strings.Contains(out, "C1") {
		t.Errorf("zoomed view should show only the selected column header:\n%s", out)
	}

	// Selection changes keep zoom on; the zoomed column follows.
	b.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	if !b.zoom.Active() {
		t.Error("changing columns must not exit zoom")
	}
	if b.selectedCol != 1 {
		t.Errorf("selectedCol = %d, want 1", b.selectedCol)
	}
	out = b.View()
	if !strings.Contains(out, "C1") || strings.Contains(out, "C0") {
		t.Errorf("zoom should follow selection to C1:\n%s", out)
	}

	// esc exits zoom; pressing esc again is passed through, not consumed.
	b.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if b.zoom.Active() {
		t.Error("esc should exit zoom")
	}

	// + then - also exits.
	b.handleKey(plus)
	b.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-")})
	if b.zoom.Active() {
		t.Error("- should exit zoom")
	}

	// + twice toggles off.
	b.handleKey(plus)
	b.handleKey(plus)
	if b.zoom.Active() {
		t.Error("+ should toggle zoom off")
	}
}
