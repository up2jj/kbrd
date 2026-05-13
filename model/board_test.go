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
