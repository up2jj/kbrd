package model

// colIndicator is a short, styled label a script attaches to a column's header
// (the per-column analogue of a header cell). It is purely presentational and
// script-driven — kbrd never sets one itself.
type colIndicator struct {
	Text string // short label, e.g. "↓ priority"; empty means "none"
	FG   string // "#rrggbb" or "" for the default soft accent
	Bold bool
}

// colIndicators is the registry of per-column indicators, keyed by column name.
// It is the source of truth: the render loop looks one up by Column.Name each
// frame, so indicators survive column reloads (filesystem columns are rebuilt
// from disk, but this map is read fresh) without any re-projection.
//
// Zero-value-safe: a nil map reads as "no indicator" and set lazily allocates,
// so the Board needs only the field — no init.
type colIndicators struct{ m map[string]colIndicator }

// get returns the indicator for a column, or the zero value (empty Text) if the
// column has none. Safe on a nil map.
func (c colIndicators) get(name string) colIndicator { return c.m[name] }

// set stores ind for the named column. An empty Text clears the entry, matching
// the Lua nil-clears contract.
func (c *colIndicators) set(name string, ind colIndicator) {
	if ind.Text == "" {
		delete(c.m, name)
		return
	}
	if c.m == nil {
		c.m = map[string]colIndicator{}
	}
	c.m[name] = ind
}

// clear removes the named column's indicator (no-op if absent).
func (c *colIndicators) clear(name string) { delete(c.m, name) }

// clearAll removes every script-set indicator.
func (c *colIndicators) clearAll() { c.m = nil }
