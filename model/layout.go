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
// firstVisible. Normal mode yields the visible window of columns, each at its
// own content width (widthOf); zoom mode yields a single wide slot for the
// selected column (firstVisible passes through untouched so leaving zoom
// restores the previous window). widthOf maps a column index to its content
// width, letting a script-set column be wider/narrower than its neighbors.
func computeSlots(zoomed bool, termWidth, selected, firstVisible, numCols int, widthOf func(i int) int) ([]Slot, int) {
	if numCols == 0 {
		return nil, firstVisible
	}
	if zoomed {
		return []Slot{{
			Col:          selected,
			Width:        zoomedColumnWidth(termWidth, widthOf(selected)),
			PreviewLines: zoomPreviewLines,
		}}, firstVisible
	}
	first, count := packWindow(termWidth, selected, firstVisible, numCols, widthOf)
	slots := make([]Slot, 0, count)
	for i := first; i < first+count; i++ {
		slots = append(slots, Slot{Col: i, Width: widthOf(i), PreviewLines: 1})
	}
	return slots, first
}

// slotWidth is the rendered width of one column cell on the row:
// content colWidth + 2 for the rounded border + 1 for the right margin.
func slotWidth(colWidth int) int { return colWidth + columnSlotPadding }

// columnAtX maps an x offset (measured from the left edge of the first visible
// column) to a column index within the window [first, first+count), walking
// each column's own slot width so variable widths map correctly. Returns -1
// when x lands past the last visible column.
func columnAtX(x, first, count int, widthOf func(i int) int) int {
	for i := first; i < first+count; i++ {
		w := slotWidth(widthOf(i))
		if x < w {
			return i
		}
		x -= w
	}
	return -1
}

// packBudget is the horizontal space available to column slots: the terminal
// minus the overflow-chip reserve. termWidth 0 (no WindowSizeMsg yet) is 80.
func packBudget(termWidth int) int {
	if termWidth == 0 {
		termWidth = 80
	}
	return termWidth - indicatorReserve
}

// packWindow returns the visible window [first, first+count) for variable-width
// columns. It keeps the firstVisible hint as the left edge when it can, grows
// right by budget, slides the window right if the selected column fell off the
// right edge, then pulls in any leftover budget on the left so the window stays
// full near the right end. The first column is always admitted (count >= 1).
// On uniform widths it reduces to the old visibleCount/clampFirstVisible math.
func packWindow(termWidth, selected, firstVisible, numCols int, widthOf func(i int) int) (first, count int) {
	avail := packBudget(termWidth)

	first = min(max(firstVisible, 0), numCols-1)
	if selected < first {
		first = selected // follow the selection leftward past the hint
	}

	// Grow right from first within budget; the left-most column always shows.
	used, last := 0, first
	for i := first; i < numCols; i++ {
		w := slotWidth(widthOf(i))
		if i > first && used+w > avail {
			break
		}
		used += w
		last = i
	}

	// Selection fell off the right edge: re-anchor on it and fill leftward.
	if selected > last {
		used, first, last = slotWidth(widthOf(selected)), selected, selected
		for i := selected - 1; i >= 0; i-- {
			w := slotWidth(widthOf(i))
			if used+w > avail {
				break
			}
			used += w
			first = i
		}
	}

	// Pull leftover budget in from the left so the window stays as full as it
	// can near the right end (mirrors the old maxFirst clamp).
	for i := first - 1; i >= 0; i-- {
		w := slotWidth(widthOf(i))
		if used+w > avail {
			break
		}
		used += w
		first = i
	}

	return first, last - first + 1
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
