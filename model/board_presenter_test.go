package model

import (
	"path/filepath"
	"strings"
	"testing"

	"kbrd/config"
)

func TestBoardPresenter_ColWidthCollapsedFocusRule(t *testing.T) {
	a := NewColumn("A", "", ItemOptions{})
	bcol := NewColumn("B", "", ItemOptions{})
	bcol.Collapsed = true
	b := &Board{
		columns:     []*Column{a, bcol},
		selectedCol: 0,
		cfg:         config.Config{ColumnWidth: 30},
	}

	if got := b.presenter.colWidthOf(b, 1); got != collapsedContentWidth {
		t.Fatalf("collapsed + unfocused width = %d, want %d", got, collapsedContentWidth)
	}
	b.selectedCol = 1
	if got := b.presenter.colWidthOf(b, 1); got != 30 {
		t.Fatalf("collapsed + focused width = %d, want 30", got)
	}
	if got := b.presenter.colWidthOf(b, 0); got != 30 {
		t.Fatalf("ordinary column width = %d, want 30", got)
	}
}

func TestBoardPresenter_VisibleRangeKeepsSelectionVisible(t *testing.T) {
	b := boardWithNCols(t, 10, 3)

	first, count := b.presenter.visibleColRange(b)
	if first != 0 || count != 3 {
		t.Fatalf("initial range = (%d,%d), want (0,3)", first, count)
	}

	b.selectedCol = 7
	first, count = b.presenter.visibleColRange(b)
	if first != 5 || count != 3 {
		t.Fatalf("after selecting col 7, range = (%d,%d), want (5,3)", first, count)
	}
	if b.firstVisibleCol != 5 {
		t.Fatalf("firstVisibleCol = %d, want 5", b.firstVisibleCol)
	}
}

func TestBoardPresenter_RenderColumnsUpdatesIndicatorsAndMetadata(t *testing.T) {
	b := boardWithNCols(t, 10, 3)
	b.selectedCol = 7

	out := b.presenter.renderColumns(b, b.termWidth)
	for _, want := range []string{"◀ 5", "2 ▶"} {
		if !strings.Contains(out, want) {
			t.Fatalf("columns view missing %q:\n%s", want, out)
		}
	}
	if b.presenter.leftIndicatorWidth <= 0 {
		t.Fatalf("leftIndicatorWidth = %d, want positive", b.presenter.leftIndicatorWidth)
	}
	if b.presenter.columnsHeight <= 0 {
		t.Fatalf("columnsHeight = %d, want positive", b.presenter.columnsHeight)
	}
	if b.presenter.columnsLeftPad < 0 {
		t.Fatalf("columnsLeftPad = %d, want non-negative", b.presenter.columnsLeftPad)
	}
}

func TestBoardPresenter_SelectAtMouseIsImmediateNavigationOnly(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "alpha"})
	colB := newTestColumn(t, map[string]string{"b": "bravo"})
	b := &Board{
		cfg:            config.Config{Path: filepath.Dir(colA.Path), ColumnWidth: 20, PreviewLines: 3},
		columns:        []*Column{colA, colB},
		selectedCol:    0,
		termWidth:      200,
		visibleHeight:  20,
		presenter:      boardPresenter{logoHeight: 0, columnsHeight: 20},
		mnemonicByRef:  map[itemRefStable]string{},
		refByMnemonic:  map[string]itemRefStable{},
		mnemonicMaxLen: 1,
	}
	for _, col := range b.columns {
		col.SetHeight(b.visibleHeight)
	}

	x := slotWidth(b.presenter.colWidthOf(b, 0)) + 1
	if !b.presenter.selectAtMouse(b, x, 1) {
		t.Fatal("selectAtMouse returned false")
	}
	if b.selectedCol != 1 {
		t.Fatalf("selectedCol = %d, want 1", b.selectedCol)
	}
	if got := b.columns[1].SelectedItem().Name; got != "b" {
		t.Fatalf("selected item = %q, want b", got)
	}
}
