package model

// groupedMenuNav owns the common navigation and viewport state for grouped
// overlays. Callers build and render their own rows, adding selectable row
// indexes to nav as they do so.
type groupedMenuNav struct {
	nav      []int
	selected int // index into nav
	scroll   int // top visible row
	follow   bool
}

func (n *groupedMenuNav) Reset() {
	n.nav = n.nav[:0]
	n.ResetSelection()
}

func (n *groupedMenuNav) ResetSelection() {
	n.selected = 0
	n.scroll = 0
	n.follow = true
}

func (n *groupedMenuNav) BeginRebuild() {
	n.nav = n.nav[:0]
}

// Clamp keeps selection and scrolling valid after the caller rebuilds rows.
func (n *groupedMenuNav) Clamp(rowCount int) {
	n.selected = min(max(n.selected, 0), max(len(n.nav)-1, 0))
	n.scroll = min(max(n.scroll, 0), max(rowCount-1, 0))
}

// UpdateKey applies the shared list-navigation bindings.
func (n *groupedMenuNav) UpdateKey(key string) bool {
	if len(n.nav) == 0 {
		return false
	}
	switch key {
	case "down", "j", "ctrl+n", "tab":
		n.selected = min(n.selected+1, len(n.nav)-1)
	case "up", "k", "ctrl+p", "shift+tab":
		n.selected = max(n.selected-1, 0)
	case "g", "home":
		n.selected = 0
	case "G", "end":
		n.selected = len(n.nav) - 1
	case "pgdown", "ctrl+d":
		n.selected = min(n.selected+10, len(n.nav)-1)
	case "pgup", "ctrl+u":
		n.selected = max(n.selected-10, 0)
	default:
		return false
	}
	n.follow = true
	return true
}

// ScrollBy moves the viewport without changing selection. Keyboard navigation
// restores follow mode and makes the selected row visible again.
func (n *groupedMenuNav) ScrollBy(rowCount, delta int) {
	if rowCount == 0 {
		return
	}
	n.scroll = min(max(n.scroll+delta, 0), max(rowCount-1, 0))
	n.follow = false
}

func (n *groupedMenuNav) SelectedRow() (int, bool) {
	if n.selected < 0 || n.selected >= len(n.nav) {
		return 0, false
	}
	return n.nav[n.selected], true
}

func (n *groupedMenuNav) Position() (current, total int) {
	if _, ok := n.SelectedRow(); ok {
		return n.selected + 1, len(n.nav)
	}
	return 0, len(n.nav)
}

func (n *groupedMenuNav) EnsureSelectedVisible(size int) {
	if size <= 0 {
		return
	}
	selectedRow, ok := n.SelectedRow()
	if !ok {
		return
	}
	if selectedRow < n.scroll {
		n.scroll = selectedRow
		return
	}
	if selectedRow >= n.scroll+size {
		n.scroll = selectedRow - size + 1
	}
}

// Viewport returns a clamped [start, end) interval of at most size rows.
func (n *groupedMenuNav) Viewport(rowCount, size int) (start, end int) {
	if rowCount <= size {
		n.scroll = 0
		return 0, rowCount
	}
	start = min(max(n.scroll, 0), rowCount-size)
	n.scroll = start
	return start, start + size
}

// groupedMenuScrollbar returns a scrollbar sized to the visible row count.
// Callers supply palette-specific track and thumb glyphs.
func groupedMenuScrollbar(height, total, start int, track, thumb string) []string {
	if height <= 0 || total <= height {
		return nil
	}
	thumbHeight := max(height*height/total, 1)
	maxStart := total - height
	thumbStart := min(max((height-thumbHeight)*start/maxStart, 0), height-thumbHeight)
	rows := make([]string, height)
	for i := range rows {
		if i >= thumbStart && i < thumbStart+thumbHeight {
			rows[i] = thumb
		} else {
			rows[i] = track
		}
	}
	return rows
}
