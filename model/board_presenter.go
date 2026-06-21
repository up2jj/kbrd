package model

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// boardPresenter owns render-time layout metadata that input handling consults
// for immediate hit-testing. These values are view coordinates, never stable
// item identities for delayed filesystem mutations.
type boardPresenter struct {
	leftIndicatorWidth int
	columnsLeftPad     int
	columnsHeight      int
	logoHeight         int
}

func (p boardPresenter) mouseInColumns(y int) bool {
	return y >= p.logoHeight && y < p.logoHeight+p.columnsHeight
}

// colWidthOf is the content width of column i: its script-set override when one
// is given, else the configured default. This feeds layout's variable-width
// geometry so a script-set column can be wider/narrower than its neighbors.
func (p boardPresenter) colWidthOf(b *Board, i int) int {
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
func (p boardPresenter) visibleColRange(b *Board) (first, count int) {
	if len(b.columns) == 0 {
		return 0, 0
	}
	widthOf := func(i int) int { return p.colWidthOf(b, i) }
	first, count = packWindow(b.termWidth, b.selectedCol, b.firstVisibleCol, len(b.columns), widthOf)
	b.firstVisibleCol = first
	return first, count
}

func (p boardPresenter) computeSlots(b *Board) ([]Slot, int) {
	widthOf := func(i int) int { return p.colWidthOf(b, i) }
	slots, first := computeSlots(b.zoom.Active(), b.termWidth, b.selectedCol, b.firstVisibleCol, len(b.columns), widthOf)
	b.firstVisibleCol = first
	return slots, first
}

func (p *boardPresenter) renderColumns(b *Board, width int) string {
	b.mnemonics().rebuild()

	gap := lipgloss.NewStyle().MarginRight(1)
	gutterW := gutterWidth(b.mnemonicMaxLen)

	slots, first := p.computeSlots(b)
	end := first + len(slots)
	rendered := make([]string, 0, len(slots)+2)

	indicatorStyle := lipgloss.NewStyle().
		Foreground(b.palette.FgSubtle).
		Bold(true).
		PaddingTop(1).
		MarginRight(1)
	p.leftIndicatorWidth = 0
	if !b.zoom.Active() && first > 0 {
		chip := indicatorStyle.Render(fmt.Sprintf("◀ %d", first))
		rendered = append(rendered, chip)
		p.leftIndicatorWidth = lipgloss.Width(chip) + 1
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
	p.columnsLeftPad = 0
	if b.zoom.Active() {
		// Zoom renders a single column; center it on the row.
		columnsView = lipgloss.PlaceHorizontal(b.termWidth, lipgloss.Center, columnsView)
	} else if pad := (width - lipgloss.Width(columnsView)) / 2; pad > 0 {
		// Center the whole strip as a group when it doesn't fill the width.
		// Computed explicitly so columnAtMouse can subtract the same offset.
		p.columnsLeftPad = pad
		columnsView = lipgloss.NewStyle().PaddingLeft(pad).Render(columnsView)
	}
	p.columnsHeight = lipgloss.Height(columnsView)
	return columnsView
}

func (p boardPresenter) columnAtMouse(b *Board, x int) (int, bool) {
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

func (p boardPresenter) itemY(y int) int {
	return y - p.logoHeight
}

func (p boardPresenter) itemAtMouse(col *Column, y int) (int, bool) {
	return col.HitTest(p.itemY(y))
}

// selectAtMouse converts rendered mouse coordinates into an immediate UI
// selection. The rendered indexes stay inside this synchronous navigation step;
// delayed mutations must re-resolve through stable item/column refs.
func (p boardPresenter) selectAtMouse(b *Board, x, y int) bool {
	colIdx, ok := p.columnAtMouse(b, x)
	if !ok {
		return false
	}
	col := b.columns[colIdx]
	b.selectedCol = colIdx
	if itemIdx, ok := p.itemAtMouse(col, y); ok {
		col.SelectIndex(itemIdx)
	}
	return true
}
