package model

import "kbrd/events"

// finalizeItemSave refreshes the column after an in-app content write, restores
// selection by item identity, and publishes the hook-facing ItemSaved event.
// Callers resolve their own stable target before invoking it.
func (b *Board) finalizeItemSave(col *Column, itemName, kind string) {
	b.reloadColumnAfterMutation(col)
	col.SelectByName(itemName)
	b.bus.Publish(events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: itemName}, Kind: kind})
}
