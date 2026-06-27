package model

import (
	"image/color"

	tea "charm.land/bubbletea/v2"

	"kbrd/colstore"
	"kbrd/vlist"
)

// Column represents one kanban column backed by a directory.
type Column struct {
	Name        string
	Path        string
	Items       []Item // unfiltered master list (used by file operations)
	list        vlist.Model
	itemOpts    ItemOptions
	listYOffset int
	palette     Palette

	// width/height are the last geometry the layout handed this column; renderCfg
	// is the last per-frame render context. They are kept so the list's delegate
	// (and so its item heights) stay valid for calls made between frames — e.g.
	// HitTest during a mouse event, which runs before the next View.
	width     int
	height    int
	renderCfg renderConfig

	// transformed marks a filesystem column whose item order is currently
	// script-defined (a column_items hook returned a table for it). Rendered
	// as a ƒ glyph in the header so a hidden/reordered card is explainable.
	// Maintained by Board.applyColumnTransform.
	transformed bool

	// Virtual-column state. A virtual column has no filesystem backing: its
	// Items are pushed by a script via kbrd.column.set, file moves into/out of
	// it are rejected, and its actions come from colCmds rather than the
	// built-in mutation keys. Zero for ordinary filesystem columns.
	Virtual    bool
	VID        string       // Lua-facing id (stable key for set/clear)
	colCmds    []VirtualCmd // column-scoped commands (B)
	defaultCmd string       // id of the Enter/default command (optional)
	emptyText  string       // placeholder shown when there are no items

	// Script-set appearance overrides (virtual columns). Zero values mean
	// "use the cfg/palette default". Width participates in layout geometry;
	// headerFG/headerBG paint the header bar in renderHeader.
	Width    int
	headerFG color.Color
	headerBG color.Color

	// Collapsed is the user's persisted intent to shrink this column to a thin
	// vertical bar (toggled with "|", restored from colstore on load). It is
	// intent, not the rendered state: the focused column auto-expands, so the
	// live decision is made in Board.colWidthOf (Collapsed && not selected),
	// which drives both the bar width and Column.View's collapsed path.
	Collapsed bool
}

// collapsedContentWidth is the content width of a collapsed column's bar: wide
// enough to set the vertical name/count off the border with a cell of padding
// each side. Its on-screen width is this plus columnSlotPadding (border +
// margin).
const collapsedContentWidth = 3

// collapsedStoreKey is the colstore key that persists a filesystem column's
// collapse intent across restarts.
const collapsedStoreKey = "collapsed"

// ContentWidth is the column's content width: its script-set override when one
// is given, else the supplied default (cfg.ColumnWidth). Layout owns geometry,
// so this is the single place an override feeds the slot math.
func (c *Column) ContentWidth(def int) int {
	if c.Width > 0 {
		return c.Width
	}
	return def
}

// ToggleCollapse flips this column's collapse intent and persists it. The board
// only dispatches the key; the flag, its persistence, and how a collapsed column
// renders all live here in the column component.
func (c *Column) ToggleCollapse() {
	c.Collapsed = !c.Collapsed
	c.persistCollapsed()
}

// collapseFocusShift returns the column index to focus after collapsing the one
// at `selected` (of `n` columns). The focused column always auto-expands, so the
// just-folded bar only shows once focus leaves it; this keeps focus adjacent —
// the previous column at the right edge, otherwise the next. With a single
// column there is nowhere to go, so focus stays put.
func collapseFocusShift(selected, n int) int {
	switch {
	case n <= 1:
		return selected
	case selected == n-1:
		return selected - 1
	default:
		return selected + 1
	}
}

// Expand clears the column's collapse intent for this session when something
// explicitly surfaces its content — e.g. a script selecting one of its items —
// so the column opens and stays open instead of re-collapsing on the next
// keypress (unlike the transient auto-expand a focused column gets). It does not
// persist: a script revealing an item must not overwrite the user's saved
// collapse preference, which returns on the next launch.
func (c *Column) Expand() { c.Collapsed = false }

// persistCollapsed best-effort writes the collapse intent to the column's
// colstore. Virtual columns have no backing dir, so their collapse is
// session-only. A failed write only means the state won't survive a restart, so
// it is swallowed rather than surfaced.
func (c *Column) persistCollapsed() {
	if c.Virtual || c.Path == "" {
		return
	}
	_ = colstore.Update(c.Path, func(s *colstore.Store) error {
		s.Set(collapsedStoreKey, c.Collapsed)
		return nil
	})
}

// RestoreCollapsed loads the persisted collapse intent for a filesystem column.
// Missing/invalid state leaves the column expanded. Called once at build time.
func (c *Column) RestoreCollapsed() {
	if c.Virtual || c.Path == "" {
		return
	}
	s, err := colstore.Read(c.Path)
	if err != nil {
		return
	}
	if v, ok := s.Get(collapsedStoreKey); ok {
		if b, ok := v.(bool); ok {
			c.Collapsed = b
		}
	}
}

// vlistKeys maps the board's cursor bindings into the list engine so j/k and the
// arrows are defined in exactly one place (model/keys.go).
func vlistKeys() vlist.KeyMap {
	return vlist.KeyMap{
		Up: Keys.CursorUp, Down: Keys.CursorDown,
		PageUp: Keys.ColPageUp, PageDown: Keys.ColPageDown,
	}
}

// NewColumn builds a column over a directory. Widths are not stored: layout
// owns geometry, and every render passes the column its width via RenderCtx.
func NewColumn(name, path string, itemOpts ItemOptions) *Column {
	palette := DarkPalette()
	c := &Column{
		Name:      name,
		Path:      path,
		list:      vlist.New(vlistKeys()),
		itemOpts:  itemOpts,
		palette:   palette,
		renderCfg: renderConfig{palette: palette, gutterW: 2, previewLines: 1},
	}
	c.syncDelegate()
	return c
}

// syncDelegate hands the list a fresh cardDelegate over the current items and
// render context. Called whenever items or the render context change.
func (c *Column) syncDelegate() {
	c.list.SetDelegate(cardDelegate{items: c.Items, cfg: c.renderCfg})
}

func (c *Column) SetHeight(h int) {
	c.height = h
	c.list.SetSize(c.width, c.height)
}

func setColumnHeights(cols []*Column, h int) {
	for _, col := range cols {
		col.SetHeight(h)
	}
}

func (c *Column) UpdateList(msg tea.Msg) tea.Cmd {
	return c.list.Update(msg)
}

// BeginFilter focuses the filter input; the returned command drives its cursor
// blink.
func (c *Column) BeginFilter() tea.Cmd {
	return c.list.BeginFilter()
}

// ScrollBy scrolls the list content by n rows (positive = down) without moving
// the cursor — used by mouse-wheel handling.
func (c *Column) ScrollBy(n int) {
	c.list.ScrollBy(n)
}
