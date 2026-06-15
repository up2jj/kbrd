// Package vlist is a variable-height, filterable, scrollable single-column
// list widget for Bubble Tea. Unlike bubbles/list it does not assume a uniform
// item height: it stacks each item at the height its Delegate reports, so cards
// of different sizes coexist in one column. It owns the cursor, the fuzzy
// filter, and the scroll viewport; the host owns the data and supplies a
// Delegate that renders items by index.
//
// The package is deliberately generic — it imports no application types — so it
// is unit-testable in isolation and cannot create an import cycle with its host.
package vlist

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"
)

// Delegate is the host's data + rendering source. vlist addresses items by
// index into the host's backing collection; the host owns that collection and
// passes a fresh (cheap) Delegate each frame to carry render context.
type Delegate interface {
	// Len reports the number of items.
	Len() int
	// Height is the number of terminal rows item i renders to. It may differ
	// per item — that is the whole point — but must match what Render produces.
	Height(i int) int
	// Render draws item i; selected is true for the cursor row.
	Render(i int, selected bool) string
	// FilterValue is the string item i is fuzzy-matched against. Returning ""
	// excludes the item from results while a filter is active (e.g. separators).
	FilterValue(i int) string
	// Selectable reports whether the cursor may land on item i. A single
	// up/down press skips non-selectable rows (e.g. separators).
	Selectable(i int) bool
}

// KeyMap are the bindings vlist consumes for cursor movement. The host injects
// them so key choices live in one place (the host's own key registry).
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
}

type filterState int

const (
	unfiltered filterState = iota
	filtering              // the filter input is focused and capturing keys
	applied                // a query is set and narrowing, but not capturing keys
)

// Model is the widget state. Construct it with New.
type Model struct {
	vp     viewport.Model
	filter textinput.Model
	keys   KeyMap

	del     Delegate
	visible []int // underlying indices currently shown, in display order
	cursor  int   // index into visible

	state  filterState
	width  int
	height int

	// scrollToCursor requests that the next View scroll the cursor into view.
	// Set on cursor moves; cleared by View. Wheel scrolling deliberately does
	// not set it, so the user can scroll away from the cursor.
	scrollToCursor bool
}

// New builds a Model. keys supplies the cursor up/down bindings.
func New(keys KeyMap) Model {
	ti := textinput.New()
	ti.Prompt = "Filter: "
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = false // the host routes wheel events via ScrollBy
	return Model{vp: vp, filter: ti, keys: keys}
}

// SetSize sets the widget's outer width and height (height includes the filter
// bar row when shown).
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.syncViewport()
}

// SetDelegate swaps the data/render source. It is cheap and called every frame;
// it does not recompute visibility (call Reload after the underlying items
// change).
func (m *Model) SetDelegate(d Delegate) {
	first := m.del == nil
	m.del = d
	if first {
		m.rebuildVisible()
	}
}

// Reload recomputes the visible set from the current Delegate, re-applying any
// active filter, and clamps the cursor. The host calls it after items change.
func (m *Model) Reload() { m.rebuildVisible() }

// Update handles cursor movement and the filter text-input flow. While the
// filter is focused every key is consumed; otherwise only the up/down bindings.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	if m.state == filtering {
		switch km.Type {
		case tea.KeyEsc:
			m.ClearFilter()
			return nil
		case tea.KeyEnter:
			m.applyFilter()
			return nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.rebuildVisible()
		m.scrollToCursor = true
		return cmd
	}
	switch {
	case key.Matches(km, m.keys.Up):
		m.moveCursor(-1)
	case key.Matches(km, m.keys.Down):
		m.moveCursor(+1)
	case key.Matches(km, m.keys.PageUp):
		m.pageCursor(-1)
	case key.Matches(km, m.keys.PageDown):
		m.pageCursor(+1)
	}
	return nil
}

// View renders the filter bar (when shown) above the scrolling content.
func (m *Model) View() string {
	m.syncViewport()
	parts := make([]string, len(m.visible))
	for vi, ui := range m.visible {
		parts[vi] = m.del.Render(ui, vi == m.cursor)
	}
	m.vp.SetContent(strings.Join(parts, "\n"))
	if m.scrollToCursor {
		m.ensureVisible()
		m.scrollToCursor = false
	}
	if m.filterShown() {
		return m.filter.View() + "\n" + m.vp.View()
	}
	return m.vp.View()
}

// --- cursor & selection ------------------------------------------------------

// Index returns the cursor position in visible space.
func (m *Model) Index() int { return m.cursor }

// Select moves the cursor to visible position i (snapping to the nearest
// selectable row).
func (m *Model) Select(i int) {
	m.cursor = i
	m.clampCursor()
	m.scrollToCursor = true
}

// SelectUnderlying selects the visible row backed by underlying index ui, if it
// is currently visible.
func (m *Model) SelectUnderlying(ui int) {
	for vi, u := range m.visible {
		if u == ui {
			m.Select(vi)
			return
		}
	}
}

// SelectFirst / SelectLast land on the first / last selectable visible row.
func (m *Model) SelectFirst() {
	for vi := range m.visible {
		if m.selectableAt(vi) {
			m.Select(vi)
			return
		}
	}
}

func (m *Model) SelectLast() {
	for vi := len(m.visible) - 1; vi >= 0; vi-- {
		if m.selectableAt(vi) {
			m.Select(vi)
			return
		}
	}
}

// Selected returns the underlying index of the cursor row. ok is false when the
// list is empty.
func (m *Model) Selected() (int, bool) {
	if len(m.visible) == 0 {
		return 0, false
	}
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return 0, false
	}
	return m.visible[m.cursor], true
}

// Visible returns the underlying indices currently shown, in display order.
func (m *Model) Visible() []int {
	out := make([]int, len(m.visible))
	copy(out, m.visible)
	return out
}

// AtTop / AtBottom report whether there is no selectable row above / below the
// cursor, so the host's wrap logic composes when a separator sits at an edge.
func (m *Model) AtTop() bool    { _, ok := m.nextSelectable(m.cursor, -1); return !ok }
func (m *Model) AtBottom() bool { _, ok := m.nextSelectable(m.cursor, +1); return !ok }

// --- filtering ---------------------------------------------------------------

// BeginFilter focuses the filter input. The returned command drives the cursor
// blink.
func (m *Model) BeginFilter() tea.Cmd {
	m.state = filtering
	return m.filter.Focus()
}

// ClearFilter removes any filter and restores the full list.
func (m *Model) ClearFilter() {
	m.state = unfiltered
	m.filter.Blur()
	m.filter.Reset()
	m.rebuildVisible()
}

// Filtering reports whether the filter input is focused (capturing keys).
func (m *Model) Filtering() bool { return m.state == filtering }

// Filtered reports whether a query is currently narrowing the list.
func (m *Model) Filtered() bool {
	return m.state == applied || (m.state == filtering && strings.TrimSpace(m.filter.Value()) != "")
}

// FilterShown reports whether the filter bar is visible.
func (m *Model) filterShown() bool { return m.state != unfiltered }

// FilterShown is the exported form for hosts.
func (m *Model) FilterShown() bool { return m.filterShown() }

// Query returns the current filter text.
func (m *Model) Query() string { return m.filter.Value() }

func (m *Model) applyFilter() {
	if strings.TrimSpace(m.filter.Value()) == "" {
		m.ClearFilter()
		return
	}
	m.state = applied
	m.filter.Blur()
}

// --- geometry ----------------------------------------------------------------

// HitTest maps a y offset (rows below the first content row, before scrolling)
// to a visible index. ok is false when y lands past the last item.
func (m *Model) HitTest(y int) (int, bool) {
	if y < 0 {
		return 0, false
	}
	abs := m.vp.YOffset + y
	acc := 0
	for vi := range m.visible {
		h := m.heightOf(vi)
		if abs < acc+h {
			return vi, true
		}
		acc += h
	}
	return 0, false
}

// AboveBelow counts visible items scrolled entirely above / below the viewport.
func (m *Model) AboveBelow() (above, below int) {
	top := m.vp.YOffset
	bot := m.vp.YOffset + m.vp.Height
	acc := 0
	for vi := range m.visible {
		h := m.heightOf(vi)
		switch {
		case acc+h <= top:
			above++
		case acc >= bot:
			below++
		}
		acc += h
	}
	return above, below
}

// ScrollMetrics reports vertical scroll geometry for drawing a scrollbar:
// offset rows scrolled from the top, viewport height in rows, and total content
// height in rows. content <= viewport means everything fits in the viewport.
func (m *Model) ScrollMetrics() (offset, viewport, content int) {
	m.syncViewport()
	total := 0
	for vi := range m.visible {
		total += m.heightOf(vi)
	}
	return m.vp.YOffset, m.vp.Height, total
}

// HeaderLines is the number of rows the filter bar occupies (0 or 1).
func (m *Model) HeaderLines() int {
	if m.filterShown() {
		return 1
	}
	return 0
}

// ScrollBy scrolls the content by n rows (positive = down). The cursor stays
// put; it re-centres on the next keyboard move.
func (m *Model) ScrollBy(n int) {
	if n > 0 {
		m.vp.ScrollDown(n)
	} else if n < 0 {
		m.vp.ScrollUp(-n)
	}
}

// --- internals ---------------------------------------------------------------

func (m *Model) syncViewport() {
	h := max(m.height-m.HeaderLines(), 0)
	m.vp.Width = m.width
	m.vp.Height = h
	// Size the text area to the column, leaving room for the "Filter: " prompt.
	m.filter.Width = max(m.width-len(m.filter.Prompt)-1, 1)
}

func (m *Model) rebuildVisible() {
	if m.del == nil {
		m.visible = nil
		m.cursor = 0
		return
	}
	n := m.del.Len()
	q := ""
	if m.state != unfiltered {
		q = strings.TrimSpace(m.filter.Value())
	}
	if q == "" {
		m.visible = make([]int, n)
		for i := range m.visible {
			m.visible[i] = i
		}
		m.clampCursor()
		return
	}
	// Candidates are items with a non-empty FilterValue; separators (which
	// return "") drop out while filtering, matching bubbles/list.
	idx := make([]int, 0, n)
	cand := make([]string, 0, n)
	for i := range n {
		fv := m.del.FilterValue(i)
		if fv == "" {
			continue
		}
		idx = append(idx, i)
		cand = append(cand, fv)
	}
	ranks := fuzzy.Find(q, cand)
	sort.Stable(ranks)
	m.visible = make([]int, 0, len(ranks))
	for _, r := range ranks {
		m.visible = append(m.visible, idx[r.Index])
	}
	m.clampCursor()
}

func (m *Model) clampCursor() {
	if len(m.visible) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.selectableAt(m.cursor) {
		return
	}
	if j, ok := m.nextSelectable(m.cursor, +1); ok {
		m.cursor = j
		return
	}
	if j, ok := m.nextSelectable(m.cursor, -1); ok {
		m.cursor = j
	}
}

func (m *Model) moveCursor(dir int) {
	if j, ok := m.nextSelectable(m.cursor, dir); ok {
		m.cursor = j
		m.scrollToCursor = true
	}
}

// pageCursor moves the cursor by roughly one viewport height in direction dir
// (+1 down, -1 up), then snaps to the nearest selectable row. Walking only as
// far as the ends clamps at the first/last row — it does not wrap.
func (m *Model) pageCursor(dir int) {
	if len(m.visible) == 0 {
		return
	}
	page := max(m.vp.Height, 1)
	target := m.cursor
	acc := 0
	for i := m.cursor + dir; i >= 0 && i < len(m.visible); i += dir {
		target = i
		acc += max(m.heightOf(i), 1)
		if acc >= page {
			break
		}
	}
	m.cursor = target
	if !m.selectableAt(m.cursor) {
		if j, ok := m.nextSelectable(m.cursor, dir); ok {
			m.cursor = j
		} else if j, ok := m.nextSelectable(m.cursor, -dir); ok {
			m.cursor = j
		}
	}
	m.scrollToCursor = true
}

// nextSelectable returns the first selectable visible index strictly past from
// in direction dir (+1/-1).
func (m *Model) nextSelectable(from, dir int) (int, bool) {
	for i := from + dir; i >= 0 && i < len(m.visible); i += dir {
		if m.selectableAt(i) {
			return i, true
		}
	}
	return 0, false
}

func (m *Model) selectableAt(vi int) bool {
	if vi < 0 || vi >= len(m.visible) {
		return false
	}
	return m.del.Selectable(m.visible[vi])
}

func (m *Model) heightOf(vi int) int { return m.del.Height(m.visible[vi]) }

func (m *Model) offsetOf(vi int) int {
	acc := 0
	for i := 0; i < vi && i < len(m.visible); i++ {
		acc += m.heightOf(i)
	}
	return acc
}

// ensureVisible scrolls so the cursor row sits fully inside the viewport.
func (m *Model) ensureVisible() {
	if len(m.visible) == 0 {
		return
	}
	top := m.offsetOf(m.cursor)
	h := m.heightOf(m.cursor)
	switch {
	case top < m.vp.YOffset:
		m.vp.SetYOffset(top)
	case top+h > m.vp.YOffset+m.vp.Height:
		m.vp.SetYOffset(top + h - m.vp.Height)
	}
}
