package script

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"

	"kbrd/events"
)

// ColumnItem is one entry of a column_items hook's returned list, decoded to
// Go. Name (falling back to Path) identifies an existing item; Separator+Title
// describe an injected grouping row instead.
type ColumnItem struct {
	Name      string
	Path      string
	Separator bool
	Title     string
}

// ColumnItemsResult is the host's verdict for one column transform.
// Changed=false means "leave the column untouched" — no hook registered, every
// hook declined (returned nil), or every hook errored. Skipped=true means a
// script invocation was in progress so the VM could not be entered; the caller
// should retry once the host is idle (see Busy).
type ColumnItemsResult struct {
	Changed bool
	Skipped bool
	Items   []ColumnItem
}

// Busy reports whether a script invocation is in progress — including a
// command suspended on a kbrd.ui.* primitive. While busy, FireColumnItems
// returns Skipped and the model retries after the script finishes.
func (h *Host) Busy() bool { return h != nil && h.running }

// FireColumnItems invokes the column_items hook(s) for one filesystem column.
// pinned is read-only context; unpinned is the transform target. Hooks fire in
// registration order; the first one returning a table wins (a nil return means
// "declined — try the next hook"). Errors count against the hook's
// consecutive-error budget exactly like fireHook. UI goroutine only.
func (h *Host) FireColumnItems(column string, pinned, unpinned []map[string]interface{}) ColumnItemsResult {
	if h == nil || h.L == nil {
		return ColumnItemsResult{}
	}
	entries := h.hooks[events.NameColumnItems]
	if len(entries) == 0 {
		return ColumnItemsResult{}
	}
	if h.running {
		return ColumnItemsResult{Skipped: true}
	}

	payload := map[string]interface{}{
		"column": column,
		"pinned": itemMapsToIface(pinned),
		"items":  itemMapsToIface(unpinned),
	}

	// Same running-flag + deferred-drain discipline as fireHook: events
	// published from inside the hook body queue instead of re-entering the VM.
	h.running = true
	defer func() {
		h.running = false
		pending := h.deferred
		h.deferred = nil
		for _, ev := range pending {
			h.OnEvent(ev)
		}
	}()

	var res ColumnItemsResult
	var disable []int
	for i, e := range entries {
		ret, err := h.invokeHookValue(e.fn, payload)
		if err != nil {
			e.consecutiveErrors++
			h.logger.Log("error", "hook "+events.NameColumnItems, err.Error())
			h.api.Notify("hook "+events.NameColumnItems+": "+err.Error(), "error")
			if h.cfg.ErrorThreshold > 0 && e.consecutiveErrors >= h.cfg.ErrorThreshold {
				disable = append(disable, i)
			}
			continue
		}
		e.consecutiveErrors = 0
		if ret == lua.LNil {
			continue // declined — leave the column to the next hook (or default order)
		}
		tbl, ok := ret.(*lua.LTable)
		if !ok {
			h.logger.Log("error", "hook "+events.NameColumnItems, fmt.Sprintf("expected table or nil, got %s", ret.Type()))
			h.api.Notify("hook "+events.NameColumnItems+": must return a table or nil", "error")
			continue
		}
		res = decodeColumnItems(tbl)
		break
	}
	h.pruneHooks(events.NameColumnItems, disable)
	return res
}

// decodeColumnItems converts the hook's returned array of item tables into
// ColumnItems. Non-table entries are skipped.
func decodeColumnItems(tbl *lua.LTable) ColumnItemsResult {
	res := ColumnItemsResult{Changed: true}
	n := tbl.Len()
	res.Items = make([]ColumnItem, 0, n)
	for i := 1; i <= n; i++ {
		et, ok := tbl.RawGetInt(i).(*lua.LTable)
		if !ok {
			continue
		}
		res.Items = append(res.Items, ColumnItem{
			Name:      lua.LVAsString(et.RawGetString("name")),
			Path:      lua.LVAsString(et.RawGetString("path")),
			Separator: lua.LVAsBool(et.RawGetString("separator")),
			Title:     lua.LVAsString(et.RawGetString("title")),
		})
	}
	return res
}

// itemMapsToIface widens []map[string]interface{} to []interface{} so toLValue
// can convert it into a Lua array of tables.
func itemMapsToIface(in []map[string]interface{}) []interface{} {
	out := make([]interface{}, len(in))
	for i, m := range in {
		out[i] = m
	}
	return out
}
