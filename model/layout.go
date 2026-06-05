package model

// layout.go owns the column-strip geometry: how wide a column slot is, how
// many fit, which window of columns is visible, and the zoom mode that swaps
// the multi-column strip for one wide, centered column. Board.View() asks
// computeSlots for the frame's geometry and renders whatever it returns —
// there is a single render path for both normal and zoomed mode.

const (
	// zoomMaxWidth caps the zoomed column so very wide terminals don't produce
	// unreadably long preview lines.
	zoomMaxWidth = 100
	// zoomPreviewLines is how many preview lines a zoomed card shows. It is
	// also the ceiling item loading must satisfy — see maxPreviewLines.
	zoomPreviewLines = 8
	// indicatorReserve leaves room for the "◀ N" / "N ▶" overflow chips.
	indicatorReserve = 6
	// columnSlotPadding is what rendering adds around a column's content
	// width: 2 for the rounded border, 1 for the right margin.
	columnSlotPadding = 3
	// zoomEdgeReserve is breathing room kept around the zoomed column.
	zoomEdgeReserve = 8
)

// Zoom is the column-zoom toggle. Zero value = off.
type Zoom struct {
	active bool
}

func (z *Zoom) Active() bool { return z.active }
func (z *Zoom) Toggle()      { z.active = !z.active }
func (z *Zoom) Off()         { z.active = false }

// Slot is one column's allotment in the rendered strip: which column, how
// wide its content is, and how dense its cards are.
type Slot struct {
	Col          int // column index into Board.columns
	Width        int // content width (excludes border/margin)
	PreviewLines int // preview lines per card; 1 = compact default
}

// computeSlots returns the slots to render this frame and the adjusted
// firstVisible. Normal mode yields the visible window of columns at colWidth;
// zoom mode yields a single wide slot for the selected column (firstVisible
// passes through untouched so leaving zoom restores the previous window).
func computeSlots(zoomed bool, termWidth, selected, firstVisible, numCols, colWidth int) ([]Slot, int) {
	if numCols == 0 {
		return nil, firstVisible
	}
	if zoomed {
		return []Slot{{
			Col:          selected,
			Width:        zoomedColumnWidth(termWidth, colWidth),
			PreviewLines: zoomPreviewLines,
		}}, firstVisible
	}
	count := visibleCount(termWidth, slotWidth(colWidth), numCols)
	first := clampFirstVisible(firstVisible, selected, count, numCols)
	slots := make([]Slot, 0, count)
	for i := first; i < first+count; i++ {
		slots = append(slots, Slot{Col: i, Width: colWidth, PreviewLines: 1})
	}
	return slots, first
}

// slotWidth is the rendered width of one column cell on the row:
// content colWidth + 2 for the rounded border + 1 for the right margin.
func slotWidth(colWidth int) int { return colWidth + columnSlotPadding }

// visibleCount is how many slots of slotW fit in termWidth, at least 1 and at
// most numCols. termWidth 0 (no WindowSizeMsg yet) falls back to 80.
func visibleCount(termWidth, slotW, numCols int) int {
	if termWidth == 0 {
		termWidth = 80
	}
	count := max((termWidth-indicatorReserve)/slotW, 1)
	return min(count, numCols)
}

// clampFirstVisible adjusts the window start so the selected column stays
// within [first, first+count) and the window stays within the column range.
func clampFirstVisible(firstVisible, selected, count, numCols int) int {
	if selected < firstVisible {
		firstVisible = selected
	}
	if selected >= firstVisible+count {
		firstVisible = selected - count + 1
	}
	maxFirst := max(numCols-count, 0)
	return min(max(firstVisible, 0), maxFirst)
}

// gutterWidth is the mnemonic gutter inside a card: the widest mnemonic plus
// one space, never narrower than 2.
func gutterWidth(mnemonicMaxLen int) int {
	return max(mnemonicMaxLen+1, 2)
}

// zoomedColumnWidth is the content width of the single zoomed column:
// the terminal minus breathing room, capped at zoomMaxWidth, and never
// narrower than the normal column width.
func zoomedColumnWidth(termWidth, normalColWidth int) int {
	if termWidth == 0 {
		termWidth = 80
	}
	return max(min(termWidth-zoomEdgeReserve, zoomMaxWidth), normalColWidth)
}

// minBoardWidth is the narrowest terminal the board can render in: one
// column's content plus its border and one cell of slack. Below this the
// board shows the too-small placeholder instead of a broken layout.
func minBoardWidth(colWidth int) int { return colWidth + 4 }

// maxPreviewLines is the most preview lines any layout will ask a card to
// show. Item loading uses it as the read ceiling so every layout has its
// lines in memory without re-reading files on mode changes.
func maxPreviewLines() int { return zoomPreviewLines }
