package model

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

// boardColumnsRegion renders and hit-tests the board frame's column body for a
// single view of the current board state. Its geometry values are view
// coordinates, never stable item identities for delayed filesystem mutations.
type boardColumnsRegion struct {
	leftIndicatorWidth int
	columnsLeftPad     int
	columnsHeight      int
}

func (p boardColumnsRegion) mouseInColumns(y int) bool {
	return y >= 0 && y < p.columnsHeight
}

// colWidthOf is the content width of column i: its script-set override when one
// is given, else the configured default. This feeds layout's variable-width
// geometry so a script-set column can be wider/narrower than its neighbors.
func (p boardColumnsRegion) colWidthOf(b *Board, i int) int {
	c := b.columns[i]
	// A collapsed column shrinks to a thin bar — except the focused one, which
	// auto-expands so its cards stay readable. This single seam keeps the bar's
	// width and Column.View's collapsed rendering (keyed off width) in lockstep.
	if c.Collapsed && i != b.selectedCol {
		return collapsedContentWidth
	}
	return c.ContentWidth(b.cfg.ColumnWidth)
}

// visibleColRange returns the index of the first column to render and the
// number of columns that fit horizontally. It also adjusts firstVisibleCol so
// the active column is always within the visible window. The math lives in
// layout.go; this wrapper applies it to board state.
func (p boardColumnsRegion) visibleColRange(b *Board) (first, count int) {
	if len(b.columns) == 0 {
		return 0, 0
	}
	widthOf := func(i int) int { return p.colWidthOf(b, i) }
	first, count = packWindow(b.termWidth, b.selectedCol, b.firstVisibleCol, len(b.columns), widthOf)
	b.firstVisibleCol = first
	return first, count
}

func (p boardColumnsRegion) computeSlots(b *Board) ([]Slot, int) {
	widthOf := func(i int) int { return p.colWidthOf(b, i) }
	slots, first := computeSlots(b.zoom.Active(), b.termWidth, b.selectedCol, b.firstVisibleCol, len(b.columns), widthOf)
	b.firstVisibleCol = first
	return slots, first
}

func (p *boardColumnsRegion) measure(b *Board, width int) ([]Slot, int) {
	slots, first := p.computeSlots(b)
	end := first + len(slots)
	indicatorStyle := p.indicatorStyle(b)

	renderedWidth := 0
	p.leftIndicatorWidth = 0
	if !b.zoom.Active() && first > 0 {
		chipW := lipgloss.Width(indicatorStyle.Render(fmt.Sprintf("◀ %d", first)))
		renderedWidth += chipW
		p.leftIndicatorWidth = chipW + 1
	}
	for _, s := range slots {
		renderedWidth += slotWidth(s.Width)
	}
	if !b.zoom.Active() && end < len(b.columns) {
		renderedWidth += lipgloss.Width(indicatorStyle.Render(fmt.Sprintf("%d ▶", len(b.columns)-end)))
	}

	p.columnsLeftPad = 0
	if !b.zoom.Active() {
		if pad := (width - renderedWidth) / 2; pad > 0 {
			p.columnsLeftPad = pad
		}
	}
	p.columnsHeight = p.renderedHeight(b)
	return slots, first
}

func (p boardColumnsRegion) renderedHeight(b *Board) int {
	return max(b.visibleHeight+4, 4)
}

func (p boardColumnsRegion) indicatorStyle(b *Board) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(b.palette.FgSubtle).
		Bold(true).
		PaddingTop(1).
		MarginRight(1)
}

func (p *boardColumnsRegion) renderColumns(b *Board, width int) string {
	b.mnemonics().rebuild()

	gap := lipgloss.NewStyle().MarginRight(1)
	gutterW := gutterWidth(b.mnemonicMaxLen)

	slots, first := p.measure(b, width)
	end := first + len(slots)
	rendered := make([]string, 0, len(slots)+2)

	indicatorStyle := p.indicatorStyle(b)
	if !b.zoom.Active() && first > 0 {
		rendered = append(rendered, indicatorStyle.Render(fmt.Sprintf("◀ %d", first)))
	}
	for _, s := range slots {
		col := b.columns[s.Col]
		rendered = append(rendered, gap.Render(col.View(RenderCtx{
			Active:        s.Col == b.selectedCol,
			Width:         s.Width,
			PreviewLines:  s.PreviewLines,
			WrapTitles:    b.cfg.WrapTitles,
			TitleMaxLines: b.cfg.TitleMaxLines,
			GutterW:       gutterW,
			MnemonicOf:    b.mnemonics().lookup(s.Col),
			StatFor:       b.git.StatFor,
			Indicator:     b.indicators.get(col.Name),
		})))
	}
	if !b.zoom.Active() && end < len(b.columns) {
		rendered = append(rendered, indicatorStyle.Render(fmt.Sprintf("%d ▶", len(b.columns)-end)))
	}

	columnsView := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	if b.zoom.Active() {
		// Zoom renders a single column; center it on the row.
		columnsView = lipgloss.PlaceHorizontal(b.termWidth, lipgloss.Center, columnsView)
	} else if p.columnsLeftPad > 0 {
		// Center the whole strip as a group when it doesn't fill the width.
		// Computed explicitly so columnAtMouse can subtract the same offset.
		columnsView = lipgloss.NewStyle().PaddingLeft(p.columnsLeftPad).Render(columnsView)
	}
	return columnsView
}

func (p boardColumnsRegion) columnAtMouse(b *Board, x int) (int, bool) {
	xc := x - p.leftIndicatorWidth - p.columnsLeftPad
	if xc < 0 {
		return 0, false
	}
	first, count := p.visibleColRange(b)
	widthOf := func(i int) int { return p.colWidthOf(b, i) }
	colIdx := columnAtX(xc, first, count, widthOf)
	if colIdx < 0 {
		return 0, false
	}
	return colIdx, true
}

func (p boardColumnsRegion) itemAtMouse(col *Column, y int) (int, bool) {
	return col.HitTest(y)
}

// selectAtMouse converts rendered mouse coordinates into an immediate UI
// selection. The rendered indexes stay inside this synchronous navigation step;
// delayed mutations must re-resolve through stable item/column refs.
func (p boardColumnsRegion) selectAtMouse(b *Board, x, y int) bool {
	_, _, _, ok := p.selectItemAtMouse(b, x, y)
	return ok
}

func (p boardColumnsRegion) selectItemAtMouse(b *Board, x, y int) (int, *Column, *Item, bool) {
	colIdx, ok := p.columnAtMouse(b, x)
	if !ok {
		return 0, nil, nil, false
	}
	col := b.columns[colIdx]
	b.selectedCol = colIdx
	if itemIdx, ok := p.itemAtMouse(col, y); ok {
		col.SelectIndex(itemIdx)
		if item := col.SelectedItem(); item != nil {
			return colIdx, col, item, true
		}
	}
	return colIdx, col, nil, true
}
