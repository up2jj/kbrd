package script

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"

	"kbrd/events"
)

// HTTPRequestData is the payload handed to an http_request hook. It describes an
// incoming request in serve mode before the built-in cookie auth runs. Headers
// are the canonical multi-value form so a hook can read every value of
// repeated headers (Cookie, etc.); in Lua they appear as
// headers["Cookie"] = {"a=1", "b=2"}. The request body is deliberately omitted
// (cost, and form rewriting is unsupported in v1).
type HTTPRequestData struct {
	Method     string
	Path       string
	RawQuery   string
	Headers    map[string][]string
	RemoteAddr string
}

// HTTPRewrite carries request mutations a continuing http_request verdict asks
// the server to apply to the live request before it reaches the handler. Empty
// Path/Query mean "leave unchanged". Form-body fields are not rewritable in v1.
type HTTPRewrite struct {
	Path       string
	Query      string
	SetHeaders map[string]string
	DelHeaders []string
}

// HTTPRequestVerdict is the decoded return of an http_request hook.
//
// Skipped=true means a script invocation was already in progress so the VM
// could not be entered — the caller should fail open (let the request through
// the normal chain). Action is one of "continue" (default), "respond", or
// "redirect". For "respond" the caller writes Status/Headers/Body; for
// "redirect" it issues Location with Status (default 303); for "continue" it
// applies Rewrite (if any) and proceeds.
type HTTPRequestVerdict struct {
	Skipped  bool
	Action   string
	Status   int
	Body     string
	Headers  map[string]string
	Location string
	Rewrite  *HTTPRewrite
}

// HTTPResponseData is the payload handed to an http_response hook: the request
// context plus the response the built-in handler produced (captured, not yet
// flushed to the client).
type HTTPResponseData struct {
	Method  string
	Path    string
	Status  int
	Headers map[string][]string
	Body    string
}

// HTTPResponseVerdict is the decoded return of an http_response hook. Changed is
// false when the hook declined (returned nil) or the VM was busy (Skipped).
// Status overrides the response status when non-zero; SetHeaders are merged onto
// the response; Body, when non-nil, replaces the response body (a nil Body means
// "leave the body alone", distinct from an empty-string override).
type HTTPResponseVerdict struct {
	Skipped    bool
	Changed    bool
	Status     int
	SetHeaders map[string]string
	Body       *string
}

// HasHook reports whether at least one hook is registered for the named event.
// Nil-safe. The web middleware uses it for the zero-overhead path (no hook → no
// channel hop, no response buffering).
func (h *Host) HasHook(name string) bool {
	return h != nil && len(h.hooks[name]) > 0
}

// FireHTTPRequest invokes the http_request hook(s) and returns the first
// non-nil verdict. Mirrors FireColumnItems: same running-flag + deferred-drain
// discipline and per-hook error budget. Hooks fire in registration order; the
// first one returning a table wins (nil means "declined — try the next").
// Scheduler goroutine only.
func (h *Host) FireHTTPRequest(data HTTPRequestData) HTTPRequestVerdict {
	if h == nil || h.L == nil {
		return HTTPRequestVerdict{}
	}
	entries := h.hooks[events.NameHTTPRequest]
	if len(entries) == 0 {
		return HTTPRequestVerdict{}
	}
	if h.running {
		return HTTPRequestVerdict{Skipped: true}
	}

	payload := map[string]interface{}{
		"method":      data.Method,
		"path":        data.Path,
		"query":       data.RawQuery,
		"headers":     headersToIface(data.Headers),
		"remote_addr": data.RemoteAddr,
	}

	h.running = true
	defer func() {
		h.running = false
		pending := h.deferred
		h.deferred = nil
		for _, ev := range pending {
			h.OnEvent(ev)
		}
	}()

	var res HTTPRequestVerdict
	var disable []int
	for i, e := range entries {
		ret, err := h.invokeHookValue(e.fn, payload)
		if err != nil {
			e.consecutiveErrors++
			h.logger.Log("error", "hook "+events.NameHTTPRequest, err.Error())
			h.api.Notify("hook "+events.NameHTTPRequest+": "+err.Error(), "error")
			if h.cfg.ErrorThreshold > 0 && e.consecutiveErrors >= h.cfg.ErrorThreshold {
				disable = append(disable, i)
			}
			continue
		}
		e.consecutiveErrors = 0
		if ret == lua.LNil {
			continue // declined — try the next hook
		}
		tbl, ok := ret.(*lua.LTable)
		if !ok {
			h.logger.Log("error", "hook "+events.NameHTTPRequest, fmt.Sprintf("expected table or nil, got %s", ret.Type()))
			h.api.Notify("hook "+events.NameHTTPRequest+": must return a table or nil", "error")
			continue
		}
		res = decodeHTTPRequestVerdict(tbl)
		break
	}
	h.pruneHooks(events.NameHTTPRequest, disable)
	return res
}

// FireHTTPResponse invokes the http_response hook(s) and returns the first
// non-nil verdict. Same discipline as FireHTTPRequest. Scheduler goroutine only.
func (h *Host) FireHTTPResponse(data HTTPResponseData) HTTPResponseVerdict {
	if h == nil || h.L == nil {
		return HTTPResponseVerdict{}
	}
	entries := h.hooks[events.NameHTTPResponse]
	if len(entries) == 0 {
		return HTTPResponseVerdict{}
	}
	if h.running {
		return HTTPResponseVerdict{Skipped: true}
	}

	payload := map[string]interface{}{
		"method":  data.Method,
		"path":    data.Path,
		"status":  data.Status,
		"headers": headersToIface(data.Headers),
		"body":    data.Body,
	}

	h.running = true
	defer func() {
		h.running = false
		pending := h.deferred
		h.deferred = nil
		for _, ev := range pending {
			h.OnEvent(ev)
		}
	}()

	var res HTTPResponseVerdict
	var disable []int
	for i, e := range entries {
		ret, err := h.invokeHookValue(e.fn, payload)
		if err != nil {
			e.consecutiveErrors++
			h.logger.Log("error", "hook "+events.NameHTTPResponse, err.Error())
			h.api.Notify("hook "+events.NameHTTPResponse+": "+err.Error(), "error")
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
			h.logger.Log("error", "hook "+events.NameHTTPResponse, fmt.Sprintf("expected table or nil, got %s", ret.Type()))
			h.api.Notify("hook "+events.NameHTTPResponse+": must return a table or nil", "error")
			continue
		}
		res = decodeHTTPResponseVerdict(tbl)
		break
	}
	h.pruneHooks(events.NameHTTPResponse, disable)
	return res
}

// decodeHTTPRequestVerdict converts an http_request hook's returned table into a
// verdict. Mirrors decodeColumnItems. An absent action defaults to "continue".
func decodeHTTPRequestVerdict(tbl *lua.LTable) HTTPRequestVerdict {
	v := HTTPRequestVerdict{
		Action:   lua.LVAsString(tbl.RawGetString("action")),
		Status:   int(lua.LVAsNumber(tbl.RawGetString("status"))),
		Body:     lua.LVAsString(tbl.RawGetString("body")),
		Location: lua.LVAsString(tbl.RawGetString("location")),
		Headers:  decodeStringMap(tbl.RawGetString("headers")),
	}
	if v.Action == "" {
		v.Action = "continue"
	}
	if rw, ok := tbl.RawGetString("rewrite").(*lua.LTable); ok {
		v.Rewrite = &HTTPRewrite{
			Path:       lua.LVAsString(rw.RawGetString("path")),
			Query:      lua.LVAsString(rw.RawGetString("query")),
			SetHeaders: decodeStringMap(rw.RawGetString("set_headers")),
			DelHeaders: decodeStringSlice(rw.RawGetString("del_headers")),
		}
	}
	return v
}

// decodeHTTPResponseVerdict converts an http_response hook's returned table into
// a verdict. The body key is honored only when present, so an empty-string
// override is distinguishable from "leave the body alone".
func decodeHTTPResponseVerdict(tbl *lua.LTable) HTTPResponseVerdict {
	v := HTTPResponseVerdict{
		Changed:    true,
		Status:     int(lua.LVAsNumber(tbl.RawGetString("status"))),
		SetHeaders: decodeStringMap(tbl.RawGetString("set_headers")),
	}
	if body := tbl.RawGetString("body"); body != lua.LNil {
		s := lua.LVAsString(body)
		v.Body = &s
	}
	return v
}

// decodeStringMap converts a Lua table of string→string into a Go map. Non-table
// values yield nil.
func decodeStringMap(v lua.LValue) map[string]string {
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return nil
	}
	out := make(map[string]string)
	tbl.ForEach(func(k, val lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			out[string(ks)] = lua.LVAsString(val)
		}
	})
	return out
}

// decodeStringSlice converts a Lua array of strings into a Go slice. Non-table
// values yield nil.
func decodeStringSlice(v lua.LValue) []string {
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return nil
	}
	n := tbl.Len()
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, lua.LVAsString(tbl.RawGetInt(i)))
	}
	return out
}

// headersToIface widens a canonical header map into the nested
// map[string]interface{}{key: []string{...}} shape toLValue understands, so Lua
// sees each header as an array of its values.
func headersToIface(h map[string][]string) map[string]interface{} {
	out := make(map[string]interface{}, len(h))
	for k, vals := range h {
		out[k] = vals
	}
	return out
}
