package model

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/config"
)

func TestBoardMouseRouter_DialogBlocksBoardSelection(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "alpha"})
	colB := newTestColumn(t, map[string]string{"b": "bravo"})
	b := &Board{
		cfg:            config.Config{Path: filepath.Dir(colA.Path), ColumnWidth: 20, PreviewLines: 3},
		columns:        []*Column{colA, colB},
		editor:         NewEditor(false),
		selectedCol:    0,
		termWidth:      120,
		termHeight:     30,
		visibleHeight:  20,
		mnemonicByRef:  map[itemRefStable]string{},
		refByMnemonic:  map[string]itemRefStable{},
		mnemonicMaxLen: 1,
	}
	for _, col := range b.columns {
		col.SetHeight(b.visibleHeight)
	}
	_ = b.View()
	x, y, ok := mousePointForItem(b, 1)
	if !ok {
		t.Fatal("failed to find mouse hit point for second column item")
	}

	b.dialog.OpenConfirm("Pending action", "Mouse should not select behind this dialog.", deleteConfirmMsg{})
	b.mouseRouter().HandleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if b.selectedCol != 0 {
		t.Fatalf("selectedCol changed behind dialog to %d, want 0", b.selectedCol)
	}

	b.dialog.Close()
	b.mouseRouter().HandleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if b.selectedCol != 1 {
		t.Fatalf("selectedCol after dialog close = %d, want 1", b.selectedCol)
	}
}

func TestBoardMouseRouter_WheelScrollsHoveredColumnOnly(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "alpha"})
	colBFiles := map[string]string{}
	for i := 0; i < 20; i++ {
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
	for _, col := range b.columns {
		col.SetHeight(b.visibleHeight)
	}
	_ = b.View()

	x, y, ok := mousePointForItem(b, 1)
	if !ok {
		t.Fatal("failed to find mouse hit point for second column item")
	}
	beforeOffset, _, _ := b.columns[1].list.ScrollMetrics()

	b.mouseRouter().HandleMouse(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelDown})
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
	for i := 0; i < 20; i++ {
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
	for _, col := range b.columns {
		col.SetHeight(b.visibleHeight)
	}
	_ = b.View()
	x, y, ok := mousePointForItem(b, 1)
	if !ok {
		t.Fatal("failed to find mouse hit point for second column item")
	}
	beforeOffset, _, _ := b.columns[1].list.ScrollMetrics()

	b.helpMenu.SetPalette(DarkPalette())
	b.helpMenu.Open(helpGroupsWithEntries(8))
	b.mouseRouter().HandleMouse(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelDown})
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

	b.mouseRouter().HandleMouse(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelUp})
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
	for i := 0; i < 20; i++ {
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
	for _, col := range b.columns {
		col.SetHeight(b.visibleHeight)
	}
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

	b.mouseRouter().HandleMouse(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelDown})
	_ = b.View()
	afterOffset, _, _ := b.columns[1].list.ScrollMetrics()

	if b.selectedCol != 0 {
		t.Fatalf("selectedCol changed behind git panel to %d, want 0", b.selectedCol)
	}
	if afterOffset != beforeOffset {
		t.Fatalf("column scrolled behind git panel: before=%d after=%d", beforeOffset, afterOffset)
	}
}
