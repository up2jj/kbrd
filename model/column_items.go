package model

import (
	"os"
	"path/filepath"
	"time"

	"kbrd/board"
	"kbrd/boardops"
	"kbrd/frontmatter"
)

func (c *Column) LoadItems() error {
	return c.loadItems(c.itemsByPath())
}

// itemsByPath snapshots the column's current items into a reload cache keyed by
// FullPath.
func (c *Column) itemsByPath() itemCache {
	cache := make(itemCache, len(c.Items))
	for _, it := range c.Items {
		cache[it.FullPath] = it
	}
	return cache
}

// loadItems rebuilds the column from disk, reusing any unchanged item from the
// cache so its file is never re-read. cache may be nil for a cold load.
func (c *Column) loadItems(cache itemCache) error {
	names, err := board.Items(c.Path)
	if err != nil {
		return err
	}

	items := []Item{}
	for _, name := range names {
		fullPath := filepath.Join(c.Path, name+".md")
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}
		if old, ok := cache.reuse(fullPath, info); ok {
			items = append(items, old)
			continue
		}
		if item, err := NewItem(fullPath, c.itemOpts); err == nil {
			items = append(items, item)
		}
	}

	c.Items = SortItems(items)
	c.pruneMarks()
	c.syncDelegate()
	c.list.Reload()
	return nil
}

// SetItems replaces the master item slice and the underlying list, preserving
// the cursor by item name. Used by the column_items transform to apply a
// script-defined order after a (re)load.
func (c *Column) SetItems(items []Item) {
	prevName := ""
	if sel := c.SelectedItem(); sel != nil {
		prevName = sel.Name
	}
	c.Items = items
	c.pruneMarks()
	c.syncDelegate()
	c.list.Reload()
	if prevName != "" {
		c.SelectByName(prevName)
	}
}

func (c *Column) TotalCount() int {
	return len(c.Items)
}

func (c *Column) SelectIndex(i int) {
	c.list.Select(i)
}

// SelectByName selects the item with the given name, if present.
func (c *Column) SelectByName(name string) {
	for i, item := range c.Items {
		if item.Name == name {
			c.list.SelectUnderlying(i)
			return
		}
	}
}

// CursorAtTop reports whether there is no selectable row above the cursor.
func (c *Column) CursorAtTop() bool { return c.list.AtTop() }

// CursorAtBottom reports whether there is no selectable row below the cursor.
func (c *Column) CursorAtBottom() bool { return c.list.AtBottom() }

// SelectFirst selects the first selectable (non-separator) visible item.
func (c *Column) SelectFirst() { c.list.SelectFirst() }

// SelectLast selects the last selectable (non-separator) visible item.
func (c *Column) SelectLast() { c.list.SelectLast() }

// VisibleItems returns the items currently rendered (post filter+sort), in
// display order.
func (c *Column) VisibleItems() []Item {
	vis := c.list.Visible()
	out := make([]Item, 0, len(vis))
	for _, ui := range vis {
		if ui >= 0 && ui < len(c.Items) {
			out = append(out, c.Items[ui])
		}
	}
	return out
}

func (c *Column) HasSelectedItem() bool {
	_, ok := c.list.Selected()
	return len(c.Items) > 0 && ok
}

func (c *Column) SelectedItem() *Item {
	ui, ok := c.list.Selected()
	if !ok || ui < 0 || ui >= len(c.Items) {
		return nil
	}
	item := c.Items[ui]
	return &item
}

func (c *Column) MoveItemTo(destCol *Column, itemName string) error {
	src := boardops.ColumnRef{Name: c.Name, Path: c.Path}
	dst := boardops.ColumnRef{Name: destCol.Name, Path: destCol.Path}
	if _, err := boardops.MoveItem(src, dst, itemName); err != nil {
		return err
	}
	c.LoadItems()
	destCol.LoadItems()
	return nil
}

func (c *Column) DeleteItem(itemName string) error {
	if _, err := boardops.DeleteItem(boardops.ColumnRef{Name: c.Name, Path: c.Path}, itemName); err != nil {
		return err
	}
	return nil
}

// CreateItem creates a new empty <name>.md item in the column. It will not
// overwrite an existing item (board.CreateItem uses O_EXCL). Returns the new
// item's filename.
func (c *Column) CreateItem(name string) (string, error) {
	return c.CreateItemContent(name, "")
}

// CreateItemContent is CreateItem with initial file content (e.g. a rendered
// template body).
func (c *Column) CreateItemContent(name, content string) (string, error) {
	res, err := boardops.CreateItem(boardops.ColumnRef{Name: c.Name, Path: c.Path}, name, content)
	if err != nil {
		return "", err
	}
	c.LoadItems()
	return filepath.Base(res.Path), nil
}

// The content mutations below resolve the item's path through the in-memory
// list (so virtual-column items mutate wherever they really live) and
// delegate the content semantics to package board, shared with the web
// frontend.

func (c *Column) AppendText(itemName, text string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	return board.AppendLine(fullPath, text)
}

func (c *Column) PrependText(itemName, text string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	return board.PrependLine(fullPath, text)
}

func (c *Column) ReplaceFile(itemName, text string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	return board.ReplaceFileContent(fullPath, text)
}

func (c *Column) JournalText(itemName string, at time.Time, text string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	return board.JournalLine(fullPath, at, text)
}

func (c *Column) CopyContent(itemName string) ([]byte, error) {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(fullPath)
}

func (c *Column) OpenFile(itemName string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	return openFile(fullPath)
}

func (c *Column) RenameItem(oldName, newName string) error {
	if _, err := boardops.RenameItem(boardops.ColumnRef{Name: c.Name, Path: c.Path}, oldName, newName); err != nil {
		return err
	}
	return c.LoadItems()
}

func (c *Column) Rename(newName string) error {
	newPath := filepath.Join(filepath.Dir(c.Path), newName)
	if err := board.RenameNoClobber(c.Path, newPath); err != nil {
		return err
	}
	c.Name = newName
	c.Path = newPath
	return c.LoadItems()
}

// PinItem toggles an item's pin state by rewriting its `pinned` frontmatter
// key: pinning sets `pinned: true`, unpinning removes the key. The file name is
// left untouched. LoadItems re-derives Pinned from the new frontmatter.
func (c *Column) PinItem(itemName string) error {
	for i := range c.Items {
		if c.Items[i].Name == itemName {
			raw, err := os.ReadFile(c.Items[i].FullPath)
			if err != nil {
				return err
			}
			var updated string
			if c.Items[i].Pinned {
				updated = frontmatter.Delete(string(raw), "pinned")
			} else {
				updated = frontmatter.Set(string(raw), "pinned", "true")
			}
			if err := board.ReplaceFileContent(c.Items[i].FullPath, updated); err != nil {
				return err
			}
			if err := c.LoadItems(); err != nil {
				return err
			}
			// Pinning re-sorts the column; keep the cursor on the toggled item
			// so it stays selected at its new position. The name is unchanged.
			c.SelectByName(itemName)
			return nil
		}
	}
	return os.ErrNotExist
}

func (c *Column) fullPathFor(itemName string) string {
	for _, item := range c.Items {
		if item.Name == itemName {
			return item.FullPath
		}
	}
	return ""
}

// ItemByName returns the loaded item with the given name, or nil if absent.
// Unlike SelectedItem it does not depend on the cursor, so it resolves the right
// card even when board selection has moved on (e.g. a still-open editor whose
// line command must bind to the file it was opened against).
func (c *Column) ItemByName(name string) *Item {
	for i := range c.Items {
		if c.Items[i].Name == name {
			item := c.Items[i]
			return &item
		}
	}
	return nil
}

func (c *Column) IndexByName(name string) (int, bool) {
	for i := range c.Items {
		if c.Items[i].Name == name {
			return i, true
		}
	}
	return 0, false
}

// ItemByPath returns the loaded item at path, or nil if absent.
func (c *Column) ItemByPath(path string) *Item {
	for i := range c.Items {
		if samePath(c.Items[i].FullPath, path) {
			item := c.Items[i]
			return &item
		}
	}
	return nil
}

// FrontmatterKeys returns the parsed frontmatter keys currently loaded for this
// column. It keeps callers from reaching into the item slice when they only
// need aggregate card metadata.
func (c *Column) FrontmatterKeys() []string {
	seen := map[string]struct{}{}
	for i := range c.Items {
		for k := range c.Items[i].Data {
			seen[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	return keys
}
