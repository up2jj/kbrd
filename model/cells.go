package model

import (
	"sort"

	"charm.land/lipgloss/v2"
)

// Cell is one entry in the header cells strip. The id is a programmatic handle
// (not shown — chips render content-only) used to set/update/clear the cell;
// appearance is entirely caller-decided. Cells are filled either by internal
// Go code (via SetInternal) or by Lua scripts (kbrd.cell.set).
type Cell struct {
	ID       int
	Text     string
	FG, BG   string // "#rrggbb" or "" => terminal default
	Bold     bool
	internal bool // built-in cell; survives ClearAll (which is script-facing)
}

// CellBar is the registry behind the header strip. It lives on the Board and is
// only ever touched on the Bubble Tea goroutine (script API methods mutate it
// synchronously while the host mutex is held, never from another goroutine), so
// it needs no locking — same contract as the rest of boardScriptAPI.
type CellBar struct {
	cells   map[int]*Cell
	palette *Palette
}

func (cb *CellBar) ensure() {
	if cb.cells == nil {
		cb.cells = make(map[int]*Cell)
	}
}

// Set adds or replaces a script-facing cell.
func (cb *CellBar) Set(c Cell) {
	cb.ensure()
	c.internal = false
	cb.cells[c.ID] = &c
}

// SetInternal adds or replaces a built-in cell. Built-ins survive ClearAll so a
// script clearing its own cells can't wipe the git/count indicators.
func (cb *CellBar) SetInternal(c Cell) {
	cb.ensure()
	c.internal = true
	cb.cells[c.ID] = &c
}

// Clear removes a cell by id (no-op if absent).
func (cb *CellBar) Clear(id int) {
	delete(cb.cells, id)
}

// ClearAll removes every script-set cell, leaving built-ins in place.
func (cb *CellBar) ClearAll() {
	for id, c := range cb.cells {
		if !c.internal {
			delete(cb.cells, id)
		}
	}
}

// Empty reports whether the strip has nothing to render.
func (cb *CellBar) Empty() bool { return len(cb.cells) == 0 }

// sortedIDs returns cell ids in ascending order. Built-ins use negative ids, so
// they sort first (leftmost in the right-aligned strip) and survive truncation.
func (cb *CellBar) sortedIDs() []int {
	ids := make([]int, 0, len(cb.cells))
	for id := range cb.cells {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// render builds the chip strip, dropping trailing (highest-id) chips until it
// fits within maxWidth so built-ins on the left always survive a narrow header.
func (cb *CellBar) render(maxWidth int) string {
	if cb.Empty() || maxWidth <= 0 {
		return ""
	}
	ids := cb.sortedIDs()
	chips := make([]string, 0, len(ids))
	for _, id := range ids {
		chips = append(chips, cb.chip(cb.cells[id]))
	}
	for len(chips) > 0 {
		strip := lipgloss.JoinHorizontal(lipgloss.Top, withGaps(chips)...)
		if lipgloss.Width(strip) <= maxWidth {
			return strip
		}
		chips = chips[:len(chips)-1] // drop the highest-id chip and retry
	}
	return ""
}

func (cb *CellBar) chip(c *Cell) string {
	st := lipgloss.NewStyle().Padding(0, 1)
	if c.FG != "" {
		st = st.Foreground(lipgloss.Color(c.FG))
	}
	if c.BG != "" {
		st = st.Background(lipgloss.Color(c.BG))
	}
	if c.Bold {
		st = st.Bold(true)
	}
	return st.Render(c.Text)
}

// withGaps interleaves a single space between chips for breathing room.
func withGaps(chips []string) []string {
	if len(chips) <= 1 {
		return chips
	}
	out := make([]string, 0, len(chips)*2-1)
	for i, c := range chips {
		if i > 0 {
			out = append(out, " ")
		}
		out = append(out, c)
	}
	return out
}
