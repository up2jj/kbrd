package model

// This file is the model-side glue for the Lua column_items transform hook.
// The hook lets a script reorder, filter, or group (via separators) the items
// of a filesystem column. Two invariants are enforced here, not in Lua:
//
//   - Pinned items stay on top in default order; only the unpinned group is
//     the transform target (the hook still sees the pinned group, read-only).
//   - Returned entries are identity-mapped back to real Items (by name, then
//     path), so a script can never corrupt item state — unknown entries are
//     ignored unless they are separators.
//
// The Lua VM is single-threaded and only safe on the UI goroutine, so the
// transform is never fired inside Column.loadItems (which runs on watcher
// worker goroutines). Columns always load in default order; the transform is
// applied afterwards, on the UI goroutine, when the result is swapped in.

// applyColumnTransforms runs the column_items hook over every filesystem
// column. Used after bulk (re)loads.
func (b *Board) applyColumnTransforms() {
	for _, col := range b.columns {
		b.applyColumnTransform(col)
	}
}

// applyColumnTransform fires the column_items hook for one column and rewrites
// its items in script order. No-op for virtual columns (they already control
// their own order via kbrd.column.set), when scripting is off, or when every
// hook declines. If the host is mid-script the transform is deferred via
// transformPending and re-applied by drainColumnTransform. UI goroutine only.
func (b *Board) applyColumnTransform(col *Column) {
	if col == nil || col.Virtual || b.scripts == nil {
		return
	}
	var pinned, unpinned []Item
	for _, it := range col.Items {
		switch {
		case it.Separator:
			// Synthetic row injected by a previous transform — drop before
			// re-running so separators never accumulate.
		case it.Pinned:
			pinned = append(pinned, it)
		default:
			unpinned = append(unpinned, it)
		}
	}
	pt := make([]map[string]any, len(pinned))
	for i, it := range pinned {
		pt[i] = itemHookTable(it)
	}
	ut := make([]map[string]any, len(unpinned))
	for i, it := range unpinned {
		ut[i] = itemHookTable(it)
	}

	res := b.scripts.FireColumnItems(col.Name, pt, ut)
	if res.Skipped {
		b.transformPending = true
		return // keep the current transformed marker until the drain re-fires
	}
	if !res.Changed {
		// No hook, every hook declined, or all errored — the column is back to
		// (or still in) default order, so the header marker must not lie.
		col.transformed = false
		return
	}
	col.transformed = true

	// Identity-map the returned order back onto real Items. Each source item
	// is used at most once, so a duplicated entry can't clone a card.
	byName := make(map[string]int, len(unpinned))
	byPath := make(map[string]int, len(unpinned))
	for i, it := range unpinned {
		byName[it.Name] = i
		byPath[it.FullPath] = i
	}
	used := make([]bool, len(unpinned))
	out := make([]Item, 0, len(pinned)+len(res.Items))
	out = append(out, pinned...)
	for _, ci := range res.Items {
		if ci.Separator {
			out = append(out, Item{Separator: true, Title: ci.Title})
			continue
		}
		idx, ok := byName[ci.Name]
		if !ok {
			idx, ok = byPath[ci.Path]
		}
		if !ok || used[idx] {
			continue // unknown (or pinned, or duplicate) entry — ignored
		}
		used[idx] = true
		out = append(out, unpinned[idx])
	}
	col.SetItems(out)
}

// drainColumnTransform re-applies the transform once the script host is idle
// again, covering FireColumnItems calls skipped while a script was running
// (e.g. kbrd.board.refresh() from a command body). Called from the Update
// wrapper, so it converges on the first message after the script finishes.
func (b *Board) drainColumnTransform() {
	if !b.transformPending || b.scripts == nil || b.scripts.Busy() {
		return
	}
	b.transformPending = false
	b.applyColumnTransforms()
}

// reloadColumnAfterMutation re-reads a column from disk and re-applies the
// column_items transform. UI-goroutine mutation handlers use this instead of a
// bare col.LoadItems() so a script-defined order survives the reload (the
// watcher would eventually re-apply it, but only after the debounce window).
func (b *Board) reloadColumnAfterMutation(col *Column) {
	col.LoadItems()
	b.applyColumnTransform(col)
}

// itemHookTable is the Lua-facing view of one item handed to the column_items
// hook. The keys are stable API surface; data round-trips the item's full
// frontmatter map (including unknown keys like priority).
func itemHookTable(it Item) map[string]any {
	return map[string]any{
		"name":   it.Name,
		"title":  it.Title,
		"pinned": it.Pinned,
		"tags":   it.Tags,
		"meta":   it.Meta,
		"icon":   it.Icon,
		"accent": it.Accent,
		"path":   it.FullPath,
		"data":   it.Data,
	}
}
