package model

import (
	"errors"
	"fmt"
	"os"

	"kbrd/board"
	"kbrd/events"
	"kbrd/frontmatter"
)

// errVirtualColumn is returned when a mutation targets a virtual (script-owned,
// fileless) column, where item create/move/rename/delete have no meaning. The
// guards live here, at the centralized mutators, so every entry point — key
// handlers, the Lua kbrd.board.* API, MCP — is covered.
var errVirtualColumn = errors.New("virtual columns are read-only")

// This file holds the canonical board-mutation primitives. Each performs the
// filesystem change via the Column method AND publishes the corresponding
// event, so every entry point — user key handlers, the Lua API, and any future
// caller — fires hooks consistently. Publishing the event is impossible to
// forget because it lives next to the mutation, not at the call site (the
// `item_moved`-on-manual-move bug came from inline publishing that drifted).
//
// These helpers deliberately do NOT touch UI state (notifier, selection,
// reload) — callers own that. All run on the Bubble Tea goroutine.
//
// Mutations whose Column method reloads the column internally also re-apply
// the column_items transform here, for the same reason events are published
// here: the call sites can't forget it.

func (b *Board) createItem(col *Column, name string) (string, error) {
	return b.createItemContent(col, name, "")
}

func (b *Board) createItemContent(col *Column, name, content string) (string, error) {
	if col.Virtual {
		return "", errVirtualColumn
	}
	path, err := col.CreateItemContent(name, content)
	if err != nil {
		return "", err
	}
	b.bus.Publish(events.ItemCreated{Item: events.ItemRef{Column: col.Name, Name: name}})
	b.applyColumnTransform(col)
	return path, nil
}

func (b *Board) renameItem(col *Column, oldName, newName string) error {
	if col.Virtual {
		return errVirtualColumn
	}
	if err := col.RenameItem(oldName, newName); err != nil {
		return err
	}
	b.bus.Publish(events.ItemRenamed{
		Item:    events.ItemRef{Column: col.Name, Name: newName},
		OldName: oldName,
	})
	b.applyColumnTransform(col)
	return nil
}

func (b *Board) deleteItem(col *Column, name string) error {
	if col.Virtual {
		return errVirtualColumn
	}
	if err := col.DeleteItem(name); err != nil {
		return err
	}
	b.bus.Publish(events.ItemDeleted{Column: col.Name, Name: name})
	return nil
}

// setFrontmatter rewrites a single top-level frontmatter key on the named card,
// creating the block if absent (frontmatter.Set's create path). value is a
// verbatim YAML scalar. Like the other primitives it performs the FS change and
// re-applies the column_items transform; UI state (selection, notifier) is the
// caller's. Shared so the Lua API / MCP can reuse it.
func (b *Board) setFrontmatter(col *Column, name, key, value string) error {
	return b.rewriteFrontmatter(col, name, func(raw string) string {
		return frontmatter.Set(raw, key, value)
	})
}

// deleteFrontmatter removes a single top-level frontmatter key from the named
// card. A key the card does not carry is a no-op (no write). Same FS-change +
// transform discipline as setFrontmatter.
func (b *Board) deleteFrontmatter(col *Column, name, key string) error {
	return b.rewriteFrontmatter(col, name, func(raw string) string {
		return frontmatter.Delete(raw, key)
	})
}

// rewriteFrontmatter reads the named card, applies rewrite to its raw content,
// and writes the result back — skipping the write (and reload) when rewrite is a
// no-op, so removing an absent key never touches the file. Shared by
// set/deleteFrontmatter.
func (b *Board) rewriteFrontmatter(col *Column, name string, rewrite func(raw string) string) error {
	if col.Virtual {
		return errVirtualColumn
	}
	path := ""
	for _, item := range col.Items {
		if item.Name == name {
			path = item.FullPath
			break
		}
	}
	if path == "" {
		return fmt.Errorf("item not found: %s", name)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated := rewrite(string(raw))
	if updated == string(raw) {
		return nil // nothing changed (e.g. removing an absent key)
	}
	if err := board.ReplaceFileContent(path, updated); err != nil {
		return err
	}
	col.LoadItems()
	b.applyColumnTransform(col)
	return nil
}

func (b *Board) moveItem(src, dst *Column, name string) error {
	if src.Virtual || dst.Virtual {
		return errVirtualColumn
	}
	if err := src.MoveItemTo(dst, name); err != nil {
		return err
	}
	b.bus.Publish(events.ItemMoved{
		Item: events.ItemRef{Column: src.Name, Name: name},
		From: src.Name,
		To:   dst.Name,
	})
	b.applyColumnTransform(src)
	b.applyColumnTransform(dst)
	return nil
}
