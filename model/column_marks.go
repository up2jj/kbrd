package model

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// ToggleMark flips the mark for itemName. It returns the new marked state.
func (c *Column) ToggleMark(itemName string) bool {
	if itemName == "" {
		return false
	}
	if c.marked == nil {
		c.marked = map[string]struct{}{}
	}
	if _, ok := c.marked[itemName]; ok {
		delete(c.marked, itemName)
		return false
	}
	c.marked[itemName] = struct{}{}
	return true
}

func (c *Column) ClearMarks() {
	c.marked = nil
}

func (c *Column) MarkedCount() int {
	return len(c.marked)
}

func (c *Column) IsMarked(itemName string) bool {
	if c.marked == nil {
		return false
	}
	_, ok := c.marked[itemName]
	return ok
}

// MarkedItems returns marked, currently visible items in display order.
func (c *Column) MarkedItems() []Item {
	if len(c.marked) == 0 {
		return nil
	}
	visible := c.VisibleItems()
	out := make([]Item, 0, min(len(c.marked), len(visible)))
	for _, item := range visible {
		if item.Separator {
			continue
		}
		if c.IsMarked(item.Name) {
			out = append(out, item)
		}
	}
	return out
}

func (c *Column) pruneMarks() {
	if len(c.marked) == 0 {
		return
	}
	present := make(map[string]struct{}, len(c.Items))
	for _, item := range c.Items {
		present[item.Name] = struct{}{}
	}
	for name := range c.marked {
		if _, ok := present[name]; !ok {
			delete(c.marked, name)
		}
	}
	if len(c.marked) == 0 {
		c.marked = nil
	}
}

func (b *Board) toggleFocusedMark() tea.Cmd {
	if len(b.columns) == 0 || b.selectedCol < 0 || b.selectedCol >= len(b.columns) {
		return nil
	}
	col := b.columns[b.selectedCol]
	if col.Virtual {
		return nil
	}
	item := col.SelectedItem()
	if item == nil || item.Separator {
		return nil
	}
	for i, other := range b.columns {
		if i != b.selectedCol {
			other.ClearMarks()
		}
	}
	marked := col.ToggleMark(item.Name)
	if marked {
		return b.notifier.Success("marked " + item.Name)
	}
	return b.notifier.Success("unmarked " + item.Name)
}

func (b *Board) clearFocusedMarks() tea.Cmd {
	if len(b.columns) == 0 || b.selectedCol < 0 || b.selectedCol >= len(b.columns) {
		return nil
	}
	col := b.columns[b.selectedCol]
	if col.Virtual {
		return nil
	}
	n := col.MarkedCount()
	col.ClearMarks()
	if n == 0 {
		return nil
	}
	return b.notifier.Success("cleared " + strconv.Itoa(n) + " mark(s)")
}
