package model

import (
	"charm.land/lipgloss/v2"

	"kbrd/events"
)

// VirtualCmd is a column-scoped command surfaced in the X menu / status hints
// for a virtual column. Ref is the host dispatch handle.
type VirtualCmd struct {
	ID           string
	Name         string
	Key          string
	Default      bool
	RequiresItem bool // false lets the command run on an empty column
	Ref          string
}

// NewVirtualColumn builds an empty script-backed column, then flips the virtual
// flag and clears the filesystem Path.
func NewVirtualColumn(vid, name string, palette Palette) *Column {
	c := NewColumn(name, "", ItemOptions{})
	c.Virtual = true
	c.VID = vid
	c.palette = palette
	c.renderCfg.palette = palette
	c.syncDelegate()
	return c
}

// ApplyVirtualSpec replaces the column's items and column-scoped commands from a
// script push (kbrd.column.set). Items are shown in the order given (no
// SortItems). The cursor is preserved by item id when the selected item still
// exists, else clamped to its old index.
func (c *Column) ApplyVirtualSpec(spec events.VirtualColumnSpec) {
	if spec.Name != "" {
		c.Name = spec.Name
	}
	c.emptyText = spec.Empty
	// set replaces the column wholesale, so an absent field resets to default.
	c.Width = spec.Width
	c.headerFG = lipgloss.Color(spec.HeaderFG)
	c.headerBG = lipgloss.Color(spec.HeaderBG)

	c.colCmds = c.colCmds[:0]
	c.defaultCmd = ""
	for _, vc := range spec.Commands {
		c.colCmds = append(c.colCmds, VirtualCmd{
			ID: vc.ID, Name: vc.Name, Key: vc.Key, Default: vc.Default,
			RequiresItem: vc.RequiresItem, Ref: vc.Ref,
		})
		if vc.Default && c.defaultCmd == "" {
			c.defaultCmd = vc.ID
		}
	}

	prevName, prevIdx := "", c.list.Index()
	if sel := c.SelectedItem(); sel != nil {
		prevName = sel.Name
	}

	items := make([]Item, 0, len(spec.Items))
	for _, vi := range spec.Items {
		items = append(items, virtualItemToItem(vi))
	}
	c.Items = items // virtual columns control their own order
	c.syncDelegate()
	c.list.Reload()
	c.restoreVirtualCursor(prevName, prevIdx)
}

// restoreVirtualCursor re-selects the item named prevName after a re-push; if it
// is gone, it clamps to the previous index position (bounded by the new length).
func (c *Column) restoreVirtualCursor(prevName string, prevIdx int) {
	if prevName != "" {
		for i, it := range c.Items {
			if it.Name == prevName {
				c.list.SelectUnderlying(i)
				return
			}
		}
	}
	if n := len(c.Items); n > 0 {
		if prevIdx >= n {
			prevIdx = n - 1
		}
		if prevIdx < 0 {
			prevIdx = 0
		}
		c.list.Select(prevIdx)
	}
}

// virtualItemToItem converts a script-pushed VirtualItem into a model Item. The
// id (else title) becomes Name so the existing name-keyed selection works.
func virtualItemToItem(vi events.VirtualItem) Item {
	name := vi.ID
	if name == "" {
		name = vi.Title
	}
	var preview []string
	if vi.Preview != "" {
		preview = []string{vi.Preview}
	}
	return Item{
		Name:      name,
		Title:     vi.Title,
		Preview:   preview,
		FullPath:  vi.Path,
		Virtual:   true,
		Separator: vi.Separator,
		Meta:      vi.Meta,
		Icon:      vi.Icon,
		Accent:    vi.Accent,
		Data:      vi.Data,
	}
}
