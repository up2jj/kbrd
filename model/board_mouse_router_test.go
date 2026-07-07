package model

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	tea "charm.land/bubbletea/v2"

	"kbrd/config"
)

func newMouseRouterTestBoard(t *testing.T, cfg config.Config) (*Board, int, int) {
	t.Helper()
	b := boardWithNCols(t, 2, 2)
	b.cfg.BoardItemDoubleClick = cfg.BoardItemDoubleClick
	b.notifier = NewNotifier("none")
	writeColItem(t, b.columns[0], "a")
	writeColItem(t, b.columns[1], "b")
	_ = b.View()
	x, y, ok := mousePointForItem(b, 1)
	if !ok {
		t.Fatal("failed to find mouse hit point for second column item")
	}
	return b, x, y
}

func TestBoardMouseRouter_DialogBlocksBoardSelection(t *testing.T) {
	b, x, y := newMouseRouterTestBoard(t, config.Config{})

	b.dialog.OpenConfirm("Pending action", "Mouse should not select behind this dialog.", deleteConfirmMsg{})
	b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if b.selectedCol != 0 {
		t.Fatalf("selectedCol changed behind dialog to %d, want 0", b.selectedCol)
	}

	b.dialog.Close()
	b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if b.selectedCol != 1 {
		t.Fatalf("selectedCol after dialog close = %d, want 1", b.selectedCol)
	}
}

func TestBoardMouseRouter_WheelScrollsHoveredColumnOnly(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "alpha"})
	colBFiles := map[string]string{}
	for i := range 20 {
		name := "item-" + strconv.Itoa(i)
		colBFiles[name] = name
	}
	colB := newTestColumn(t, colBFiles)
	b := &Board{
		cfg:            config.Config{Path: filepath.Dir(colA.Path), ColumnWidth: 20, PreviewLines: 1},
		columns:        []*Column{colA, colB},
		editor:         NewEditor(false),
		selectedCol:    0,
		termWidth:      120,
		termHeight:     16,
		visibleHeight:  6,
		mnemonicByRef:  map[itemRefStable]string{},
		refByMnemonic:  map[string]itemRefStable{},
		mnemonicMaxLen: 1,
	}
	setColumnHeights(b.columns, b.visibleHeight)
	_ = b.View()

	x, y, ok := mousePointForItem(b, 1)
	if !ok {
		t.Fatal("failed to find mouse hit point for second column item")
	}
	beforeOffset, _, _ := b.columns[1].list.ScrollMetrics()

	b.mouseRouter().HandleMouse(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	_ = b.View()
	afterOffset, _, _ := b.columns[1].list.ScrollMetrics()
	if b.selectedCol != 0 {
		t.Fatalf("selectedCol changed on wheel to %d, want 0", b.selectedCol)
	}
	if afterOffset <= beforeOffset {
		t.Fatalf("wheel did not scroll hovered column down; offset before=%d after=%d", beforeOffset, afterOffset)
	}
}

func TestBoardMouseRouter_WheelScrollsHelpMenu(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "alpha"})
	colBFiles := map[string]string{}
	for i := range 20 {
		name := "item-" + strconv.Itoa(i)
		colBFiles[name] = name
	}
	colB := newTestColumn(t, colBFiles)
	b := &Board{
		cfg:            config.Config{Path: filepath.Dir(colA.Path), ColumnWidth: 20, PreviewLines: 1},
		columns:        []*Column{colA, colB},
		editor:         NewEditor(false),
		selectedCol:    0,
		termWidth:      120,
		termHeight:     16,
		visibleHeight:  6,
		mnemonicByRef:  map[itemRefStable]string{},
		refByMnemonic:  map[string]itemRefStable{},
		mnemonicMaxLen: 1,
	}
	setColumnHeights(b.columns, b.visibleHeight)
	_ = b.View()
	x, y, ok := mousePointForItem(b, 1)
	if !ok {
		t.Fatal("failed to find mouse hit point for second column item")
	}
	beforeOffset, _, _ := b.columns[1].list.ScrollMetrics()

	b.helpMenu.SetPalette(DarkPalette())
	b.helpMenu.Open(helpGroupsWithEntries(8))
	b.mouseRouter().HandleMouse(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	afterOffset, _, _ := b.columns[1].list.ScrollMetrics()

	if got := b.helpMenu.SelectedRunKey(); got != "0" {
		t.Fatalf("help selection after wheel down = %q, want 0", got)
	}
	if b.helpMenu.scroll != 3 {
		t.Fatalf("help scroll after wheel down = %d, want 3", b.helpMenu.scroll)
	}
	if afterOffset != beforeOffset {
		t.Fatalf("column scrolled behind help menu: before=%d after=%d", beforeOffset, afterOffset)
	}

	b.mouseRouter().HandleMouse(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	if got := b.helpMenu.SelectedRunKey(); got != "0" {
		t.Fatalf("help selection after wheel up = %q, want 0", got)
	}
	if b.helpMenu.scroll != 0 {
		t.Fatalf("help scroll after wheel up = %d, want 0", b.helpMenu.scroll)
	}
}

func TestBoardMouseRouter_WheelOverGitPanelDoesNotScrollColumn(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	colA := newTestColumn(t, map[string]string{"a": "alpha"})
	colBFiles := map[string]string{}
	for i := range 20 {
		name := "item-" + strconv.Itoa(i)
		colBFiles[name] = name
	}
	colB := newTestColumn(t, colBFiles)
	b := &Board{
		cfg:            config.Config{Path: filepath.Dir(colA.Path), ColumnWidth: 20, PreviewLines: 1},
		columns:        []*Column{colA, colB},
		editor:         NewEditor(false),
		notifier:       NewNotifier("none"),
		selectedCol:    0,
		termWidth:      120,
		termHeight:     16,
		visibleHeight:  6,
		mnemonicByRef:  map[itemRefStable]string{},
		refByMnemonic:  map[string]itemRefStable{},
		mnemonicMaxLen: 1,
	}
	setColumnHeights(b.columns, b.visibleHeight)
	b.initGit()
	b.git.SetSize(b.termWidth, b.termHeight)
	_ = b.git.Open()
	if !b.git.Active() {
		t.Fatal("expected git panel to be active")
	}
	_ = b.View()

	x, y, ok := mousePointForItem(b, 1)
	if !ok {
		t.Fatal("failed to find mouse hit point for second column item")
	}
	beforeOffset, _, _ := b.columns[1].list.ScrollMetrics()

	b.mouseRouter().HandleMouse(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	_ = b.View()
	afterOffset, _, _ := b.columns[1].list.ScrollMetrics()

	if b.selectedCol != 0 {
		t.Fatalf("selectedCol changed behind git panel to %d, want 0", b.selectedCol)
	}
	if afterOffset != beforeOffset {
		t.Fatalf("column scrolled behind git panel: before=%d after=%d", beforeOffset, afterOffset)
	}
}

func TestBoardMouseRouter_SingleClickSelectsWithoutOpening(t *testing.T) {
	b, x, y := newMouseRouterTestBoard(t, config.Config{})

	_, cmd := b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if cmd != nil {
		cmd()
	}

	if b.selectedCol != 1 {
		t.Fatalf("single click selected col %d, want 1", b.selectedCol)
	}
	if item := b.columns[1].SelectedItem(); item == nil || item.Name != "b" {
		t.Fatalf("single click selected item %+v, want b", item)
	}
	if b.peek.Active() {
		t.Fatal("single click opened peek")
	}
	if b.editor.state != editorNone {
		t.Fatalf("single click editor state = %v, want none", b.editor.state)
	}
}

func TestBoardMouseRouter_DoubleClickDefaultsToPeek(t *testing.T) {
	b, x, y := newMouseRouterTestBoard(t, config.Config{})

	b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	_, cmd := b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if cmd != nil {
		cmd()
	}

	if !b.peek.Active() {
		t.Fatal("double click did not open peek")
	}
	if b.peek.title != "b" {
		t.Fatalf("peek title = %q, want b", b.peek.title)
	}
	if b.editor.state != editorNone {
		t.Fatalf("double click default editor state = %v, want none", b.editor.state)
	}
}

func TestBoardMouseRouter_DoubleClickSurvivesReleaseBetweenClicks(t *testing.T) {
	b, x, y := newMouseRouterTestBoard(t, config.Config{})

	b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	b.mouseRouter().HandleMouse(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	_, cmd := b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if cmd != nil {
		cmd()
	}

	if !b.peek.Active() {
		t.Fatal("double click with release between clicks did not open peek")
	}
	if b.peek.title != "b" {
		t.Fatalf("peek title = %q, want b", b.peek.title)
	}
}

func TestBoardMouseRouter_DoubleClickCanEdit(t *testing.T) {
	b, x, y := newMouseRouterTestBoard(t, config.Config{BoardItemDoubleClick: "edit"})

	b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	_, cmd := b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if cmd != nil {
		cmd()
	}

	if b.peek.Active() {
		t.Fatal("edit double click opened peek")
	}
	if b.editor.state != editorEdit {
		t.Fatalf("editor state = %v, want edit", b.editor.state)
	}
	if b.editor.FileName != "b" {
		t.Fatalf("editor FileName = %q, want b", b.editor.FileName)
	}
}

func TestBoardMouseRouter_DoubleClickRequiresSameItem(t *testing.T) {
	b, xB, yB := newMouseRouterTestBoard(t, config.Config{})
	xA, yA, ok := mousePointForItem(b, 0)
	if !ok {
		t.Fatal("failed to find mouse hit point for first column item")
	}

	b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: xA, Y: yA, Button: tea.MouseLeft})
	_, cmd := b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: xB, Y: yB, Button: tea.MouseLeft})
	if cmd != nil {
		cmd()
	}

	if b.peek.Active() {
		t.Fatal("clicking a different item opened peek")
	}
	if b.editor.state != editorNone {
		t.Fatalf("clicking a different item editor state = %v, want none", b.editor.state)
	}
	if b.selectedCol != 1 {
		t.Fatalf("second click selected col %d, want 1", b.selectedCol)
	}
}

func TestBoardMouseRouter_NonItemClickResetsDoubleClick(t *testing.T) {
	b, x, y := newMouseRouterTestBoard(t, config.Config{})

	b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: x, Y: 0, Button: tea.MouseLeft})
	_, cmd := b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if cmd != nil {
		cmd()
	}

	if b.peek.Active() {
		t.Fatal("click after non-item reset opened peek")
	}
	if b.editor.state != editorNone {
		t.Fatalf("click after non-item reset editor state = %v, want none", b.editor.state)
	}
}
