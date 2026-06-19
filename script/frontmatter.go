package script

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"

	"kbrd/events"
)

// FrontmatterSuggestion is one key the frontmatter_suggestions hook offers: the
// key name and a default value to seed the editor with when the card does not
// already carry that key. Order follows hook registration, then the iteration
// order within each returned table.
type FrontmatterSuggestion struct {
	Key     string
	Default string
}

// FrontmatterSuggestionsResult is the host's verdict for one editor open.
// Skipped=true means a script invocation was in progress so the VM could not be
// entered; the caller proceeds with its built-in suggestions only.
type FrontmatterSuggestionsResult struct {
	Skipped     bool
	Suggestions []FrontmatterSuggestion
}

// FireFrontmatterSuggestions invokes the frontmatter_suggestions hook(s) for the
// item being edited. Unlike column_items every registered hook contributes —
// results are concatenated (later hooks can override an earlier default for the
// same key). A nil return means "no suggestions". Errors count against the
// hook's consecutive-error budget exactly like the other hooks. UI goroutine
// only.
func (h *Host) FireFrontmatterSuggestions(column, item string) FrontmatterSuggestionsResult {
	if h == nil || h.L == nil {
		return FrontmatterSuggestionsResult{}
	}
	entries := h.hooks[events.NameFrontmatterSuggestions]
	if len(entries) == 0 {
		return FrontmatterSuggestionsResult{}
	}
	if h.running {
		return FrontmatterSuggestionsResult{Skipped: true}
	}

	payload := map[string]any{
		"column": column,
		"item":   item,
	}

	// Same running-flag + deferred-drain discipline as the other hooks.
	h.running = true
	defer func() {
		h.running = false
		pending := h.deferred
		h.deferred = nil
		for _, ev := range pending {
			h.OnEvent(ev)
		}
	}()

	var res FrontmatterSuggestionsResult
	var disable []int
	for i, e := range entries {
		ret, err := h.invokeHookValue(e.fn, payload)
		if err != nil {
			e.consecutiveErrors++
			h.logger.Log("error", "hook "+events.NameFrontmatterSuggestions, err.Error())
			h.api.Notify("hook "+events.NameFrontmatterSuggestions+": "+err.Error(), "error")
			if h.cfg.ErrorThreshold > 0 && e.consecutiveErrors >= h.cfg.ErrorThreshold {
				disable = append(disable, i)
			}
			continue
		}
		e.consecutiveErrors = 0
		if ret == lua.LNil {
			continue
		}
		tbl, ok := ret.(*lua.LTable)
		if !ok {
			h.logger.Log("error", "hook "+events.NameFrontmatterSuggestions, fmt.Sprintf("expected table or nil, got %s", ret.Type()))
			h.api.Notify("hook "+events.NameFrontmatterSuggestions+": must return a table or nil", "error")
			continue
		}
		res.Suggestions = append(res.Suggestions, decodeFrontmatterSuggestions(tbl)...)
	}
	h.pruneHooks(events.NameFrontmatterSuggestions, disable)
	return res
}

// decodeFrontmatterSuggestions converts a returned table into key/default pairs.
// Both shapes are accepted: a map `{key = "default", ...}` and an array of
// `{key = "...", default = "..."}` tables (the latter preserves order, since Lua
// map iteration order is undefined). Non-string keys/values are coerced via
// LVAsString; entries without a key are skipped.
func decodeFrontmatterSuggestions(tbl *lua.LTable) []FrontmatterSuggestion {
	var out []FrontmatterSuggestion
	// Array part: ordered {key=, default=} tables.
	n := tbl.Len()
	for i := 1; i <= n; i++ {
		et, ok := tbl.RawGetInt(i).(*lua.LTable)
		if !ok {
			continue
		}
		k := lua.LVAsString(et.RawGetString("key"))
		if k == "" {
			continue
		}
		out = append(out, FrontmatterSuggestion{Key: k, Default: lua.LVAsString(et.RawGetString("default"))})
	}
	// Map part: {key = "default"} string entries (skip the array indices above).
	tbl.ForEach(func(k, v lua.LValue) {
		ks, ok := k.(lua.LString)
		if !ok {
			return
		}
		out = append(out, FrontmatterSuggestion{Key: string(ks), Default: lua.LVAsString(v)})
	})
	return out
}
