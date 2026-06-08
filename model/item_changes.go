package model

import "kbrd/events"

// changeTracker detects per-item content changes across a watcher reload. It is
// the source of the item_changed event: between the debounce tick (snapshot)
// and the reload applying (changed), it remembers each changed file's content
// hash, then reports only the items whose bytes actually differ afterwards.
//
// Gating on a content hash — not mtime — is what lets a hook bound to
// item_changed converge: a hook may rewrite the file in place, but an
// idempotent rewrite produces identical bytes, so the next reload sees the same
// hash and fires no further event. (A non-idempotent rewrite keeps changing the
// hash and so loops — a documented hazard of the item_changed event.)
//
// Like hookRunner, it is free of *Board; the thin glue below routes the board's
// columns and bus in. Its zero value is ready to use.
type changeTracker struct {
	prior map[string]uint64 // changed path -> content hash before the reload
}

// snapshot records the pre-reload content hash of every changed path, read from
// the currently displayed columns. A path not found among the filesystem items
// maps to 0 — a sentinel a real FNV-64a hash never produces, so it reads as
// "newly present" when compared after the reload. Virtual items have no file
// backing and are skipped.
func (c *changeTracker) snapshot(dirty map[string]struct{}, cols []*Column) {
	if len(dirty) == 0 {
		c.prior = nil
		return
	}
	prior := make(map[string]uint64, len(dirty))
	for path := range dirty {
		if path != "" {
			prior[path] = 0
		}
	}
	for _, col := range cols {
		for _, it := range col.Items {
			if it.Virtual {
				continue
			}
			if _, ok := prior[it.FullPath]; ok {
				prior[it.FullPath] = it.contentHash
			}
		}
	}
	c.prior = prior
}

// changed returns the freshly reloaded items whose content hash differs from
// the snapshot (including newly present files, whose sentinel 0 never matches a
// real hash), then clears the snapshot. A metadata-only touch (mtime bump,
// identical bytes) leaves the hash unchanged and so is omitted.
func (c *changeTracker) changed(cols []*Column) []events.ItemRef {
	if c.prior == nil {
		return nil
	}
	prior := c.prior
	c.prior = nil
	var refs []events.ItemRef
	for _, col := range cols {
		for _, it := range col.Items {
			if it.Virtual {
				continue
			}
			if old, ok := prior[it.FullPath]; ok && it.contentHash != old {
				refs = append(refs, events.ItemRef{Column: col.Name, Name: it.Name})
			}
		}
	}
	return refs
}

// publishItemChanges fires events.ItemChanged for each card whose content
// changed during the just-applied watcher reload. Called from the reload-apply
// handlers in board.go after the new columns are in place.
func (b *Board) publishItemChanges() {
	for _, ref := range b.changes.changed(b.columns) {
		b.bus.Publish(events.ItemChanged{Item: ref})
	}
}
