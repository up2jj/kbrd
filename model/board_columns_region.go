package model

import (
	"fmt"

	"charm.land/lipgloss/v2"

	kbrdfs "kbrd/fs"
)

type boardColumnsRegionContext struct {
	columns       []*Column
	termWidth     int
	visibleHeight int
	// The columns region updates both values during layout and hit-testing:
	// panning keeps the selected column visible, and mouse selection changes focus.
	selectedCol     *int
	firstVisibleCol *int
	zoomed          bool
	columnWidth     int
	wrapTitles      bool
	titleMaxLines   int
	mnemonicMaxLen  int
	palette         Palette
	mnemonicLookup  func(colIdx int) func(name string) string
	statFor         func(absPath string) (kbrdfs.DiffStat, bool)
	indicatorFor    func(columnName string) colIndicator
}

func (b *Board) columnsRegionContext() boardColumnsRegionContext {
	termWidth := b.termWidth
	if termWidth == 0 {
		termWidth = 80
	}
	return boardColumnsRegionContext{
		columns:         b.columns,
		termWidth:       termWidth,
		visibleHeight:   b.visibleHeight,
		selectedCol:     &b.selectedCol,
		firstVisibleCol: &b.firstVisibleCol,
		zoomed:          b.zoom.Active(),
		columnWidth:     b.cfg.ColumnWidth,
		wrapTitles:      b.cfg.WrapTitles,
		titleMaxLines:   b.cfg.TitleMaxLines,
		mnemonicMaxLen:  b.mnemonicMaxLen,
		palette:         b.palette,
		mnemonicLookup:  b.mnemonicLookup,
		statFor:         b.git.StatFor,
		indicatorFor:    b.indicators.get,
	}
}

func (b *Board) renderColumnsRegionContext() boardColumnsRegionContext {
	b.mnemonics().rebuild()
	return b.columnsRegionContext()
}

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
func (p boardColumnsRegion) colWidthOf(ctx boardColumnsRegionContext, i int) int {
	c := ctx.columns[i]
	// A collapsed column shrinks to a thin bar — except the focused one, which
	// auto-expands so its cards stay readable. This single seam keeps the bar's
	// width and Column.View's collapsed rendering (keyed off width) in lockstep.
	if c.Collapsed && i != *ctx.selectedCol {
		return collapsedContentWidth
	}
	return c.ContentWidth(ctx.columnWidth)
}

// visibleColRange returns the index of the first column to render and the
// number of columns that fit horizontally. It also adjusts firstVisibleCol so
// the active column is always within the visible window. The math lives in
// layout.go; this wrapper applies it to board state.
func (p boardColumnsRegion) visibleColRange(ctx boardColumnsRegionContext) (first, count int) {
	if len(ctx.columns) == 0 {
		return 0, 0
	}
	widthOf := func(i int) int { return p.colWidthOf(ctx, i) }
	first, count = packWindow(ctx.termWidth, *ctx.selectedCol, *ctx.firstVisibleCol, len(ctx.columns), widthOf)
	*ctx.firstVisibleCol = first
	return first, count
}

func (p boardColumnsRegion) computeSlots(ctx boardColumnsRegionContext) ([]Slot, int) {
	widthOf := func(i int) int { return p.colWidthOf(ctx, i) }
	slots, first := computeSlots(ctx.zoomed, ctx.termWidth, *ctx.selectedCol, *ctx.firstVisibleCol, len(ctx.columns), widthOf)
	*ctx.firstVisibleCol = first
	return slots, first
}

func (p *boardColumnsRegion) measure(ctx boardColumnsRegionContext, width int) ([]Slot, int) {
	slots, first := p.computeSlots(ctx)
	end := first + len(slots)
	indicatorStyle := p.indicatorStyle(ctx)

	renderedWidth := 0
	p.leftIndicatorWidth = 0
	if !ctx.zoomed && first > 0 {
		chipW := lipgloss.Width(indicatorStyle.Render(fmt.Sprintf("◀ %d", first)))
		renderedWidth += chipW
		p.leftIndicatorWidth = chipW + 1
	}
	for _, s := range slots {
		renderedWidth += slotWidth(s.Width)
	}
	if !ctx.zoomed && end < len(ctx.columns) {
		renderedWidth += lipgloss.Width(indicatorStyle.Render(fmt.Sprintf("%d ▶", len(ctx.columns)-end)))
	}

	p.columnsLeftPad = 0
	if !ctx.zoomed {
		if pad := (width - renderedWidth) / 2; pad > 0 {
			p.columnsLeftPad = pad
		}
	}
	p.columnsHeight = p.renderedHeight(ctx)
	return slots, first
}

func (p boardColumnsRegion) renderedHeight(ctx boardColumnsRegionContext) int {
	return max(ctx.visibleHeight+4, 4)
}

func (p boardColumnsRegion) indicatorStyle(ctx boardColumnsRegionContext) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(ctx.palette.FgSubtle).
		Bold(true).
		PaddingTop(1).
		MarginRight(1)
}

func (p *boardColumnsRegion) renderColumns(ctx boardColumnsRegionContext, width int) string {
	gap := lipgloss.NewStyle().MarginRight(1)
	gutterW := gutterWidth(ctx.mnemonicMaxLen)

	slots, first := p.measure(ctx, width)
	end := first + len(slots)
	rendered := make([]string, 0, len(slots)+2)

	indicatorStyle := p.indicatorStyle(ctx)
	if !ctx.zoomed && first > 0 {
		rendered = append(rendered, indicatorStyle.Render(fmt.Sprintf("◀ %d", first)))
	}
	for _, s := range slots {
		col := ctx.columns[s.Col]
		rendered = append(rendered, gap.Render(col.View(RenderCtx{
			Active:        s.Col == *ctx.selectedCol,
			Width:         s.Width,
			PreviewLines:  s.PreviewLines,
			WrapTitles:    ctx.wrapTitles,
			TitleMaxLines: ctx.titleMaxLines,
			GutterW:       gutterW,
			MnemonicOf:    ctx.mnemonicLookup(s.Col),
			StatFor:       ctx.statFor,
			Indicator:     ctx.indicatorFor(col.Name),
		})))
	}
	if !ctx.zoomed && end < len(ctx.columns) {
		rendered = append(rendered, indicatorStyle.Render(fmt.Sprintf("%d ▶", len(ctx.columns)-end)))
	}

	columnsView := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	if ctx.zoomed {
		// Zoom renders a single column; center it on the row.
		columnsView = lipgloss.PlaceHorizontal(ctx.termWidth, lipgloss.Center, columnsView)
	} else if p.columnsLeftPad > 0 {
		// Center the whole strip as a group when it doesn't fill the width.
		// Computed explicitly so columnAtMouse can subtract the same offset.
		columnsView = lipgloss.NewStyle().PaddingLeft(p.columnsLeftPad).Render(columnsView)
	}
	return columnsView
}

func (p boardColumnsRegion) columnAtMouse(ctx boardColumnsRegionContext, x int) (int, bool) {
	xc := x - p.leftIndicatorWidth - p.columnsLeftPad
	if xc < 0 {
		return 0, false
	}
	first, count := p.visibleColRange(ctx)
	widthOf := func(i int) int { return p.colWidthOf(ctx, i) }
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
func (p boardColumnsRegion) selectAtMouse(ctx boardColumnsRegionContext, x, y int) bool {
	_, _, _, ok := p.selectItemAtMouse(ctx, x, y)
	return ok
}

func (p boardColumnsRegion) selectItemAtMouse(ctx boardColumnsRegionContext, x, y int) (int, *Column, *Item, bool) {
	colIdx, ok := p.columnAtMouse(ctx, x)
	if !ok {
		return 0, nil, nil, false
	}
	col := ctx.columns[colIdx]
	*ctx.selectedCol = colIdx
	if itemIdx, ok := p.itemAtMouse(col, y); ok {
		col.SelectIndex(itemIdx)
		if item := col.SelectedItem(); item != nil {
			return colIdx, col, item, true
		}
	}
	return colIdx, col, nil, true
}
