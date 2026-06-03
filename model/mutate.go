package model

import "kbrd/events"

// This file holds the canonical board-mutation primitives. Each performs the
// filesystem change via the Column method AND publishes the corresponding
// event, so every entry point — user key handlers, the Lua API, and any future
// caller — fires hooks consistently. Publishing the event is impossible to
// forget because it lives next to the mutation, not at the call site (the
// `item_moved`-on-manual-move bug came from inline publishing that drifted).
//
// These helpers deliberately do NOT touch UI state (notifier, selection,
// reload) — callers own that. All run on the Bubble Tea goroutine.

func (b *Board) createItem(col *Column, name string) (string, error) {
	path, err := col.CreateItem(name)
	if err != nil {
		return "", err
	}
	b.bus.Publish(events.ItemCreated{Item: events.ItemRef{Column: col.Name, Name: name}})
	return path, nil
}

func (b *Board) renameItem(col *Column, oldName, newName string) error {
	if err := col.RenameItem(oldName, newName); err != nil {
		return err
	}
	b.bus.Publish(events.ItemRenamed{
		Item:    events.ItemRef{Column: col.Name, Name: newName},
		OldName: oldName,
	})
	return nil
}

func (b *Board) deleteItem(col *Column, name string) error {
	if err := col.DeleteItem(name); err != nil {
		return err
	}
	b.bus.Publish(events.ItemDeleted{Column: col.Name, Name: name})
	return nil
}

func (b *Board) moveItem(src, dst *Column, name string) error {
	if err := src.MoveItemTo(dst, name); err != nil {
		return err
	}
	b.bus.Publish(events.ItemMoved{
		Item: events.ItemRef{Column: src.Name, Name: name},
		From: src.Name,
		To:   dst.Name,
	})
	return nil
}
