package script

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	lua "github.com/yuin/gopher-lua"

	"kbrd/events"
	"kbrd/frontmatter"
	"kbrd/natdate"
)

// installAPI builds the `kbrd` global on h.L. Called from New, never from
// outside the package — assumes the VM is fresh and h.mu is not yet held by
// a concurrent goroutine.
func (h *Host) installAPI() {
	L := h.L

	kbrd := L.NewTable()

	kbrd.RawSetString("notify", L.NewFunction(h.luaNotify))
	kbrd.RawSetString("status", L.NewFunction(h.luaStatus))
	kbrd.RawSetString("command", L.NewFunction(h.luaCommand))
	kbrd.RawSetString("has_command", L.NewFunction(h.luaHasCommand))
	kbrd.RawSetString("on", L.NewFunction(h.luaOn))
	kbrd.RawSetString("emit", L.NewFunction(h.luaEmit))
	kbrd.RawSetString("_uiGuard", L.NewFunction(h.luaUIGuard))
	kbrd.RawSetString("_remoteFetch", L.NewFunction(h.luaRemoteFetch))

	// kbrd.instance.name is this process's machine-local name, used to route
	// instance-scoped timers (kbrd.timer.every(.., { instance = "..." })) and
	// available for any per-machine branching a script wants to do.
	instance := L.NewTable()
	instance.RawSetString("name", lua.LString(h.instanceName))
	kbrd.RawSetString("instance", instance)

	board := L.NewTable()
	board.RawSetString("move", L.NewFunction(h.luaBoardMove))
	board.RawSetString("create", L.NewFunction(h.luaBoardCreate))
	board.RawSetString("templates", L.NewFunction(h.luaBoardTemplates))
	board.RawSetString("createFromTemplate", L.NewFunction(h.luaBoardCreateFromTemplate))
	board.RawSetString("rename", L.NewFunction(h.luaBoardRename))
	board.RawSetString("delete", L.NewFunction(h.luaBoardDelete))
	board.RawSetString("refresh", L.NewFunction(h.luaBoardRefresh))
	board.RawSetString("createColumn", L.NewFunction(h.luaBoardCreateColumn))
	board.RawSetString("focus", L.NewFunction(h.luaBoardFocus))
	board.RawSetString("select", L.NewFunction(h.luaBoardSelect))
	kbrd.RawSetString("board", board)

	fs := L.NewTable()
	fs.RawSetString("read", L.NewFunction(h.luaFSRead))
	fs.RawSetString("write", L.NewFunction(h.luaFSWrite))
	fs.RawSetString("exists", L.NewFunction(h.luaFSExists))
	fs.RawSetString("mkdir", L.NewFunction(h.luaFSMkdir))
	fs.RawSetString("glob", L.NewFunction(h.luaFSGlob))
	fs.RawSetString("get_frontmatter", L.NewFunction(h.luaFSGetFrontmatter))
	fs.RawSetString("set_frontmatter", L.NewFunction(h.luaFSSetFrontmatter))
	fs.RawSetString("delete_frontmatter", L.NewFunction(h.luaFSDeleteFrontmatter))
	kbrd.RawSetString("fs", fs)

	timer := L.NewTable()
	timer.RawSetString("every", L.NewFunction(h.luaTimerEvery))
	timer.RawSetString("after", L.NewFunction(h.luaTimerAfter))
	timer.RawSetString("cancel", L.NewFunction(h.luaTimerCancel))
	kbrd.RawSetString("timer", timer)

	async := L.NewTable()
	async.RawSetString("run", L.NewFunction(h.luaAsyncRun))
	async.RawSetString("cancel", L.NewFunction(h.luaAsyncCancel))
	kbrd.RawSetString("async", async)

	cell := L.NewTable()
	cell.RawSetString("set", L.NewFunction(h.luaCellSet))
	cell.RawSetString("clear", L.NewFunction(h.luaCellClear))
	cell.RawSetString("clear_all", L.NewFunction(h.luaCellClearAll))
	kbrd.RawSetString("cell", cell)

	column := L.NewTable()
	column.RawSetString("set", L.NewFunction(h.luaColumnSet))
	column.RawSetString("clear", L.NewFunction(h.luaColumnClear))
	column.RawSetString("clear_all", L.NewFunction(h.luaColumnClearAll))
	column.RawSetString("indicator", L.NewFunction(h.luaColumnIndicator))
	kbrd.RawSetString("column", column)

	store := L.NewTable()
	store.RawSetString("get", L.NewFunction(h.luaStoreGet))
	store.RawSetString("set", L.NewFunction(h.luaStoreSet))
	store.RawSetString("all", L.NewFunction(h.luaStoreAll))
	store.RawSetString("delete", L.NewFunction(h.luaStoreDelete))
	// Column-scoped: every store call targets a column by name, so it lives under
	// kbrd.column.* next to indicator/set (column is already on kbrd by reference).
	column.RawSetString("store", store)

	date := L.NewTable()
	date.RawSetString("parse", L.NewFunction(h.luaDateParse))
	kbrd.RawSetString("date", date)

	L.SetGlobal("kbrd", kbrd)

	// kbrd.ui — defined in Lua so the three wrappers can call coroutine.yield
	// directly. Yielding from a Go function in gopher-lua is awkward; a Lua
	// shim is the path of least resistance.
	if err := L.DoString(uiBootstrap); err != nil {
		// Should never happen — bootstrap is a constant string.
		panic("kbrd.ui bootstrap: " + err.Error())
	}
}

const uiBootstrap = `
kbrd.ui = {}
function kbrd.ui.pick(title, choices)
  kbrd._uiGuard("pick")
  return coroutine.yield({_uiReq = true, kind = "pick", title = title or "", choices = choices or {}})
end
function kbrd.ui.prompt(title, default)
  kbrd._uiGuard("prompt")
  return coroutine.yield({_uiReq = true, kind = "prompt", title = title or "", default = default or ""})
end
function kbrd.ui.confirm(title)
  kbrd._uiGuard("confirm")
  return coroutine.yield({_uiReq = true, kind = "confirm", title = title or ""})
end

-- Defang os.exit globally — scripts have no legitimate need to kill the kbrd
-- process, and an accidental call would tear down the TUI mid-render.
os.exit = function() error("os.exit is disabled in kbrd scripts") end

-- Remote require: a searcher at the front of package.loaders that handles
-- module names looking like https:// or github:owner/repo/path@ref. It defers
-- the fetch/cache to the Go side (kbrd._remoteFetch) and compiles the source
-- into the SAME VM, so a remote module sees the global kbrd table and can call
-- kbrd.on(...) at load time. Non-remote names return nil so the built-in
-- searchers handle them as usual.
local function kbrd_remote_searcher(name)
  if not (name:match("^https?://") or name:match("^github:")) then
    return nil
  end
  local src, err = kbrd._remoteFetch(name)
  if not src then
    return "\n\t[kbrd remote] " .. tostring(err)
  end
  local chunk, lerr = loadstring(src, "@" .. name)
  if not chunk then
    error("kbrd: compiling remote module '" .. name .. "': " .. tostring(lerr))
  end
  return chunk
end
table.insert(package.loaders, 1, kbrd_remote_searcher)
`

// errResult pushes a (nil, errMsg) tuple — the conventional Lua return
// shape for "operation failed, here's why".
func errResult(L *lua.LState, err error) int {
	L.Push(lua.LNil)
	L.Push(lua.LString(err.Error()))
	return 2
}

// kbrd.fs.read(path) → string | nil, err
func (h *Host) luaFSRead(L *lua.LState) int {
	path := L.CheckString(1)
	body, err := h.api.FSRead(path)
	if err != nil {
		return errResult(L, err)
	}
	L.Push(lua.LString(body))
	return 1
}

// kbrd.fs.write(path, body) → true | nil, err
func (h *Host) luaFSWrite(L *lua.LState) int {
	path := L.CheckString(1)
	body := L.CheckString(2)
	if err := h.api.FSWrite(path, body); err != nil {
		return errResult(L, err)
	}
	L.Push(lua.LTrue)
	return 1
}

// kbrd.fs.exists(path) → bool
func (h *Host) luaFSExists(L *lua.LState) int {
	path := L.CheckString(1)
	L.Push(lua.LBool(h.api.FSExists(path)))
	return 1
}

// kbrd.fs.mkdir(path) → true | nil, err     (mkdir -p semantics)
func (h *Host) luaFSMkdir(L *lua.LState) int {
	path := L.CheckString(1)
	if err := h.api.FSMkdir(path); err != nil {
		return errResult(L, err)
	}
	L.Push(lua.LTrue)
	return 1
}

// kbrd.fs.glob(pattern) → list of paths
func (h *Host) luaFSGlob(L *lua.LState) int {
	pattern := L.CheckString(1)
	matches, err := h.api.FSGlob(pattern)
	if err != nil {
		return errResult(L, err)
	}
	t := L.NewTable()
	for i, m := range matches {
		t.RawSetInt(i+1, lua.LString(m))
	}
	L.Push(t)
	return 1
}

// kbrd.fs.get_frontmatter(path)       → table (every top-level key) | nil, err
// kbrd.fs.get_frontmatter(path, key)  → value | nil [, err]
//
// Reads the card's frontmatter without modifying it. With a key, returns just that
// key's value (nil if the key is absent); without a key, returns a table of every
// top-level key (empty table when the card has no frontmatter). YAML scalars come
// back as Lua strings/numbers/booleans; an unquoted date like `due: 2026-06-24`
// is returned as the string "2026-06-24" (ready for kbrd.date.parse). A read error
// (e.g. missing file) or malformed YAML returns nil, err.
func (h *Host) luaFSGetFrontmatter(L *lua.LState) int {
	path := L.CheckString(1)
	raw, err := h.api.FSRead(path)
	if err != nil {
		return errResult(L, err)
	}
	block, _, _ := frontmatter.Split(raw)
	parsed, err := frontmatter.Parse([]byte(block))
	if err != nil {
		return errResult(L, err)
	}
	if L.GetTop() >= 2 {
		key := L.CheckString(2)
		L.Push(toLValue(L, parsed.Data[key]))
		return 1
	}
	if parsed.Data == nil {
		L.Push(L.NewTable())
		return 1
	}
	L.Push(toLValue(L, parsed.Data))
	return 1
}

// kbrd.fs.set_frontmatter(path, key, value) → true | nil, err
// kbrd.fs.set_frontmatter(path, { key = value, ... }) → true | nil, err
//
// Sets one or more top-level frontmatter keys on the card at path, merging them
// into the existing block: an existing key is replaced in place and a new key is
// appended, every other line preserved (frontmatter.Set, folded per key). The
// table form sets all of its keys in one write, applied in sorted key order for
// a stable diff. Values may be strings (written verbatim as a YAML scalar),
// numbers, or booleans; the caller owns any quoting a string scalar needs. The
// card must exist — reading it first means a missing path returns the read error
// rather than minting a new file.
func (h *Host) luaFSSetFrontmatter(L *lua.LState) int {
	path := L.CheckString(1)
	raw, err := h.api.FSRead(path)
	if err != nil {
		return errResult(L, err)
	}
	if tbl, ok := L.Get(2).(*lua.LTable); ok {
		pairs, err := scalarPairs(tbl)
		if err != nil {
			return errResult(L, err)
		}
		for _, kv := range pairs {
			raw = frontmatter.Set(raw, kv.key, kv.value)
		}
	} else {
		key := L.CheckString(2)
		value, err := luaScalar(L.Get(3))
		if err != nil {
			return errResult(L, err)
		}
		raw = frontmatter.Set(raw, key, value)
	}
	if err := h.api.FSWrite(path, raw); err != nil {
		return errResult(L, err)
	}
	L.Push(lua.LTrue)
	return 1
}

// fmPair is a resolved frontmatter key and its YAML-scalar value.
type fmPair struct{ key, value string }

// scalarPairs converts a Lua table of string keys to YAML-scalar value strings,
// returned in sorted key order so a multi-key write produces a deterministic
// diff. Non-string keys (e.g. an array table) are skipped; a value that is not a
// string/number/boolean is a hard error, since silently dropping it would lose a
// key the caller meant to set.
func scalarPairs(tbl *lua.LTable) ([]fmPair, error) {
	vals := map[string]lua.LValue{}
	var keys []string
	tbl.ForEach(func(k, v lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			keys = append(keys, string(ks))
			vals[string(ks)] = v
		}
	})
	sort.Strings(keys)
	pairs := make([]fmPair, 0, len(keys))
	for _, k := range keys {
		s, err := luaScalar(vals[k])
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", k, err)
		}
		pairs = append(pairs, fmPair{key: k, value: s})
	}
	return pairs, nil
}

// luaScalar renders a Lua value as a YAML scalar for frontmatter.Set. Strings
// pass through verbatim (the caller owns quoting); booleans and numbers take
// their YAML form. Other types are rejected so a misuse surfaces as an error.
func luaScalar(lv lua.LValue) (string, error) {
	switch v := lv.(type) {
	case lua.LString:
		return string(v), nil
	case lua.LBool:
		if bool(v) {
			return "true", nil
		}
		return "false", nil
	case lua.LNumber:
		return strconv.FormatFloat(float64(v), 'g', -1, 64), nil
	default:
		return "", fmt.Errorf("frontmatter value must be a string, number, or boolean, got %s", lv.Type().String())
	}
}

// kbrd.fs.delete_frontmatter(path, key) → true | nil, err
//
// Removes a top-level frontmatter key from the card at path (frontmatter.Delete);
// a key that is absent leaves the file unchanged. The card must exist.
func (h *Host) luaFSDeleteFrontmatter(L *lua.LState) int {
	path := L.CheckString(1)
	key := L.CheckString(2)
	raw, err := h.api.FSRead(path)
	if err != nil {
		return errResult(L, err)
	}
	if err := h.api.FSWrite(path, frontmatter.Delete(raw, key)); err != nil {
		return errResult(L, err)
	}
	L.Push(lua.LTrue)
	return 1
}

// kbrd.board.refresh() → true | nil, err
func (h *Host) luaBoardRefresh(L *lua.LState) int {
	if err := h.api.Refresh(); err != nil {
		return errResult(L, err)
	}
	L.Push(lua.LTrue)
	return 1
}

// kbrd.board.focus(column) → true | nil, err
// Moves the board's focus to the named column. The resulting column_change /
// item_select hooks fire after the script returns (via the board's selection
// diff), so a focus hook won't re-enter mid-run.
func (h *Host) luaBoardFocus(L *lua.LState) int {
	column := L.CheckString(1)
	if err := h.api.FocusColumn(column); err != nil {
		return errResult(L, err)
	}
	L.Push(lua.LTrue)
	return 1
}

// kbrd.board.select(column, name) → true | nil, err
// Focuses the named column and moves its cursor onto the named item. Errors if
// the column or item doesn't exist.
func (h *Host) luaBoardSelect(L *lua.LState) int {
	column := L.CheckString(1)
	name := L.CheckString(2)
	if err := h.api.SelectItem(column, name); err != nil {
		return errResult(L, err)
	}
	L.Push(lua.LTrue)
	return 1
}

// kbrd.board.createColumn(name) → true | nil, err
func (h *Host) luaBoardCreateColumn(L *lua.LState) int {
	name := L.CheckString(1)
	if err := h.api.CreateColumn(name); err != nil {
		return errResult(L, err)
	}
	L.Push(lua.LTrue)
	return 1
}

// kbrd.timer.every(interval, fn [, opts]) → handle
// kbrd.timer.after(interval, fn [, opts]) → handle
// interval is either a number of milliseconds (e.g. 1500) or a Go duration
// string (e.g. "30s", "5m", "1h30s"). Sub-100ms intervals are silently
// clamped to 100ms — protects against accidental tight loops starving the UI.
//
// opts is an optional table. opts.instance, when set, restricts the timer to a
// single named instance: the timer is only registered when opts.instance equals
// kbrd.instance.name, so the same .kbrd.lua can run a repeating task on one box
// (e.g. an always-on `serve`) without firing on every clone. A skipped timer
// returns an inert handle, so kbrd.timer.cancel on it is a harmless no-op.
func (h *Host) luaTimerEvery(L *lua.LState) int { return h.scheduleTimer(L, true) }
func (h *Host) luaTimerAfter(L *lua.LState) int { return h.scheduleTimer(L, false) }

func (h *Host) scheduleTimer(L *lua.LState, repeat bool) int {
	if h.inTimer {
		// Forbids exponential timer pyramids and self-rescheduling polling
		// patterns. Repeating timers (re-armed by the host, not Lua) still
		// work; this only catches scripts trying to call kbrd.timer.* from
		// inside a timer body or its side-effect hooks.
		L.RaiseError("kbrd.timer: cannot schedule a timer from inside a timer callback (use kbrd.timer.every for periodic work)")
		return 0
	}
	dur, err := luaDuration(L.CheckAny(1))
	if err != nil {
		L.RaiseError("kbrd.timer: %s", err.Error())
		return 0
	}
	fn := L.CheckFunction(2)
	// Instance routing: allocate a handle either way so the script can store it,
	// but only register (and schedule) the timer when the target matches.
	token := h.allocToken()
	if opts, ok := L.Get(3).(*lua.LTable); ok {
		if want := lua.LVAsString(opts.RawGetString("instance")); want != "" && want != h.instanceName {
			L.Push(lua.LString(token))
			return 1
		}
	}
	const minDur = 100 * time.Millisecond
	if dur < minDur {
		dur = minDur
	}
	h.timers[token] = &timerEntry{fn: fn, interval: dur, repeat: repeat}
	h.pendingTimers = append(h.pendingTimers, TimerSchedule{Token: token, Duration: dur, Repeat: repeat})
	L.Push(lua.LString(token))
	return 1
}

// kbrd.async.run(shellCmd, fn) → handle string
// Runs shellCmd on a worker goroutine; when it finishes, fn(result) is
// called on the UI thread with `{out, exitCode, error}`.
//
// Use this for slow shell calls (curl, large finds, lengthy git ops) that
// would otherwise freeze the TUI. The script returns immediately — the
// callback fires whenever the command finishes.
func (h *Host) luaAsyncRun(L *lua.LState) int {
	if h.inTimer {
		L.RaiseError("kbrd.async.run: cannot start async work from inside a timer callback")
		return 0
	}
	cmd := L.CheckString(1)
	fn := L.CheckFunction(2)
	token := h.allocToken()
	h.asyncCallbacks[token] = fn
	h.pendingAsyncCmds = append(h.pendingAsyncCmds, AsyncCmd{Token: token, Shell: cmd})
	L.Push(lua.LString(token))
	return 1
}

// kbrd.async.cancel(handle)
// Drops the callback. The shell process keeps running (Go doesn't easily
// kill subprocesses), but its result is discarded when it finishes.
func (h *Host) luaAsyncCancel(L *lua.LState) int {
	token := L.CheckString(1)
	delete(h.asyncCallbacks, token)
	return 0
}

// kbrd.cell.set(id, opts) — add or replace a header cell. opts is a table:
//
//	{ text = "...", fg = "#rrggbb", bg = "#rrggbb", bold = true }
//
// Safe to call from timer callbacks: a kbrd.timer.every body that re-sets a cell
// each tick is the supported way to animate (flicker, ticking values, etc.).
func (h *Host) luaCellSet(L *lua.LState) int {
	id := L.CheckInt(1)
	t := L.CheckTable(2)
	h.api.CellSet(id, events.CellOpts{
		Text: lua.LVAsString(t.RawGetString("text")),
		FG:   lua.LVAsString(t.RawGetString("fg")),
		BG:   lua.LVAsString(t.RawGetString("bg")),
		Bold: lua.LVAsBool(t.RawGetString("bold")),
	})
	return 0
}

// kbrd.cell.clear(id) — remove a single header cell.
func (h *Host) luaCellClear(L *lua.LState) int {
	h.api.CellClear(L.CheckInt(1))
	return 0
}

// kbrd.cell.clear_all() — remove every script-set cell (built-ins are kept).
func (h *Host) luaCellClearAll(L *lua.LState) int {
	h.api.CellClearAll()
	return 0
}

// vcolRefPrefix returns the dispatch-ref prefix shared by all column-scoped
// commands of a virtual column, used to find/clear its run closures.
func vcolRefPrefix(vid string) string { return "vcol:" + vid + ":" }

// clearVcolFns drops the run closures registered for one virtual column id so a
// replace (kbrd.column.set again) or removal (kbrd.column.clear) doesn't leak
// stale Lua references.
func (h *Host) clearVcolFns(vid string) {
	prefix := vcolRefPrefix(vid)
	for ref := range h.vcolFns {
		if len(ref) >= len(prefix) && ref[:len(prefix)] == prefix {
			delete(h.vcolFns, ref)
		}
	}
}

// kbrd.column.set(id, { name=, empty=, items={...}, commands={...} }) —
// create or replace a virtual column. Idempotent; safe from timers/async
// callbacks. See the events.VirtualColumnSpec types for the field shapes.
func (h *Host) luaColumnSet(L *lua.LState) int {
	id := L.CheckString(1)
	spec := L.CheckTable(2)
	if id == "" {
		L.RaiseError("kbrd.column.set: id is required")
		return 0
	}

	// Replacing a column drops the previous run closures for this id.
	h.clearVcolFns(id)

	out := events.VirtualColumnSpec{
		Name:  lua.LVAsString(spec.RawGetString("name")),
		Empty: lua.LVAsString(spec.RawGetString("empty")),
		Width: int(lua.LVAsNumber(spec.RawGetString("width"))),
	}
	if out.Name == "" {
		out.Name = id
	}
	if h, ok := spec.RawGetString("header").(*lua.LTable); ok {
		out.HeaderFG = lua.LVAsString(h.RawGetString("fg"))
		out.HeaderBG = lua.LVAsString(h.RawGetString("bg"))
	}

	if items, ok := spec.RawGetString("items").(*lua.LTable); ok {
		items.ForEach(func(_, v lua.LValue) {
			it, ok := v.(*lua.LTable)
			if !ok {
				return
			}
			item := events.VirtualItem{
				ID:        lua.LVAsString(it.RawGetString("id")),
				Title:     lua.LVAsString(it.RawGetString("title")),
				Preview:   lua.LVAsString(it.RawGetString("preview")),
				Meta:      lua.LVAsString(it.RawGetString("meta")),
				Icon:      lua.LVAsString(it.RawGetString("icon")),
				Accent:    lua.LVAsString(it.RawGetString("accent")),
				Path:      lua.LVAsString(it.RawGetString("path")),
				Separator: lua.LVAsBool(it.RawGetString("separator")),
			}
			if d, ok := it.RawGetString("data").(*lua.LTable); ok {
				if m, ok := fromLValue(d).(map[string]any); ok {
					item.Data = m
				}
			}
			out.Items = append(out.Items, item)
		})
	}

	if cmds, ok := spec.RawGetString("commands").(*lua.LTable); ok {
		cmds.ForEach(func(_, v lua.LValue) {
			ct, ok := v.(*lua.LTable)
			if !ok {
				return
			}
			cid := lua.LVAsString(ct.RawGetString("id"))
			fn, _ := ct.RawGetString("run").(*lua.LFunction)
			if cid == "" || fn == nil {
				return
			}
			ref := vcolRefPrefix(id) + cid
			h.vcolFns[ref] = fn
			// requiresItem defaults to true; LVAsBool can't tell absent from
			// false, so only override when the key is actually present.
			requiresItem := true
			if v := ct.RawGetString("requiresItem"); v != lua.LNil {
				requiresItem = lua.LVAsBool(v)
			}
			out.Commands = append(out.Commands, events.VirtualCommand{
				ID:           cid,
				Name:         lua.LVAsString(ct.RawGetString("name")),
				Key:          lua.LVAsString(ct.RawGetString("key")),
				Default:      lua.LVAsBool(ct.RawGetString("default")),
				RequiresItem: requiresItem,
				Ref:          ref,
			})
		})
	}

	h.api.VirtualColumnSet(id, out)
	return 0
}

// kbrd.column.clear(id) — remove a single virtual column.
func (h *Host) luaColumnClear(L *lua.LState) int {
	id := L.CheckString(1)
	h.clearVcolFns(id)
	h.api.VirtualColumnClear(id)
	return 0
}

// kbrd.column.clear_all() — remove every script-set virtual column.
func (h *Host) luaColumnClearAll(L *lua.LState) int {
	h.vcolFns = make(map[string]*lua.LFunction)
	h.api.VirtualColumnClearAll()
	return 0
}

// kbrd.column.indicator(name, "text" | {text=, fg=, bold=} | nil)
// Sets a short, styled label on the named column's header. A nil second arg —
// or an empty text — clears the column's indicator.
func (h *Host) luaColumnIndicator(L *lua.LState) int {
	name := L.CheckString(1)
	v := L.Get(2)
	var o events.ColumnIndicatorOpts
	switch v.Type() {
	case lua.LTNil:
		// leave o zero → clears below
	case lua.LTString:
		o.Text = lua.LVAsString(v)
	case lua.LTTable:
		t := v.(*lua.LTable)
		o.Text = lua.LVAsString(t.RawGetString("text"))
		o.FG = lua.LVAsString(t.RawGetString("fg"))
		o.Bold = lua.LVAsBool(t.RawGetString("bold"))
	default:
		L.ArgError(2, "expected string, table, or nil")
	}
	if o.Text == "" {
		h.api.ColumnIndicatorClear(name)
	} else {
		h.api.ColumnIndicatorSet(name, o)
	}
	return 0
}

// kbrd.column.store.get(column, key) → value | nil   (nil, err on failure)
// A present key returns its value; an absent key returns a single nil, so a
// script can tell "missing" (one return) from "error" (nil + message).
func (h *Host) luaStoreGet(L *lua.LState) int {
	column := L.CheckString(1)
	key := L.CheckString(2)
	v, ok, err := h.api.ColumnConfigGet(column, key)
	if err != nil {
		return errResult(L, err)
	}
	if !ok {
		L.Push(lua.LNil)
		return 1
	}
	L.Push(toLValue(L, v))
	return 1
}

// kbrd.column.store.set(column, key, value) → true | nil, err
func (h *Host) luaStoreSet(L *lua.LState) int {
	column := L.CheckString(1)
	key := L.CheckString(2)
	val := fromLValue(L.Get(3))
	if err := h.api.ColumnConfigSet(column, key, val); err != nil {
		return errResult(L, err)
	}
	L.Push(lua.LTrue)
	return 1
}

// kbrd.column.store.all(column) → { key = value, ... } | nil, err
func (h *Host) luaStoreAll(L *lua.LState) int {
	column := L.CheckString(1)
	m, err := h.api.ColumnConfigAll(column)
	if err != nil {
		return errResult(L, err)
	}
	L.Push(toLValue(L, m))
	return 1
}

// kbrd.column.store.delete(column, key) → true | nil, err
func (h *Host) luaStoreDelete(L *lua.LState) int {
	column := L.CheckString(1)
	key := L.CheckString(2)
	if err := h.api.ColumnConfigDelete(column, key); err != nil {
		return errResult(L, err)
	}
	L.Push(lua.LTrue)
	return 1
}

// kbrd._uiGuard(name) — called by the kbrd.ui.* Lua wrappers before yielding.
// Rejects with a clear message if invoked from a timer body or a hook, both
// of which run via PCall and have no coroutine to yield from.
func (h *Host) luaUIGuard(L *lua.LState) int {
	if h.inTimer {
		L.RaiseError("%s", "kbrd.ui."+L.OptString(1, "*")+": cannot be used from a timer callback")
		return 0
	}
	return 0
}

// kbrd.timer.cancel(handle) → nil
// Drops the timer immediately. Any tick already in flight for this token
// becomes a no-op when FireTimer can't find it in the map.
func (h *Host) luaTimerCancel(L *lua.LState) int {
	token := L.CheckString(1)
	delete(h.timers, token)
	return 0
}

// kbrd.notify(msg [, level])
func (h *Host) luaNotify(L *lua.LState) int {
	msg := L.CheckString(1)
	level := L.OptString(2, "info")
	h.api.Notify(msg, level)
	return 0
}

// kbrd.status(msg [, ttl]) — writes a transient message to the in-app status
// bar. Unlike kbrd.notify (an OS toast), this shows inside kbrd and auto-
// expires. Safe to call from a timer callback, which makes it the idiomatic
// way to surface periodic-job activity ("synced 12:00:03"). ttl is optional —
// a number of milliseconds or a duration string ("5s"); omit it for the
// default. The model picks the message up from the queue and arms its expiry.
func (h *Host) luaStatus(L *lua.LState) int {
	msg := L.CheckString(1)
	var ttl time.Duration
	if L.GetTop() >= 2 {
		d, err := luaDuration(L.CheckAny(2))
		if err != nil {
			L.RaiseError("kbrd.status: %s", err.Error())
			return 0
		}
		ttl = d
	}
	h.pendingStatus = append(h.pendingStatus, StatusMsg{Text: msg, TTL: ttl})
	return 0
}

// kbrd.date.parse(phrase [, layout]) → string | nil, err
//
// Resolves a natural-language date phrase (English or Polish) relative to now and
// formats it with an optional Go layout (default "2006-01-02"). On an unparseable
// phrase it returns (nil, message), following the API's error-tuple convention.
func (h *Host) luaDateParse(L *lua.LState) int {
	phrase := L.CheckString(1)
	layout := L.OptString(2, "2006-01-02")
	t, err := natdate.Parse(phrase, time.Now())
	if err != nil {
		return errResult(L, err)
	}
	L.Push(lua.LString(t.Format(layout)))
	return 1
}

// luaDuration interprets a Lua value as a time.Duration: a number is taken as
// milliseconds, a string is parsed via time.ParseDuration. Used by the timer
// and status APIs so both accept the same forms.
func luaDuration(v lua.LValue) (time.Duration, error) {
	switch arg := v.(type) {
	case lua.LNumber:
		return time.Duration(int(arg)) * time.Millisecond, nil
	case lua.LString:
		d, err := time.ParseDuration(string(arg))
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q (use milliseconds or a string like \"30s\")", string(arg))
		}
		return d, nil
	default:
		return 0, fmt.Errorf("duration must be a number of milliseconds or a duration string")
	}
}

// kbrd.command(id, name, fn) — short form
// kbrd.command{ id=, name=, description=, run= } — table form
func (h *Host) luaCommand(L *lua.LState) int {
	if h.inTimer {
		L.RaiseError("kbrd.command: cannot register commands from inside a timer callback (register from init.lua or a command body)")
		return 0
	}
	var (
		id          string
		name        string
		description string
		scope       string
		fn          *lua.LFunction
	)

	if L.GetTop() == 1 && L.Get(1).Type() == lua.LTTable {
		t := L.CheckTable(1)
		id = lua.LVAsString(t.RawGetString("id"))
		name = lua.LVAsString(t.RawGetString("name"))
		description = lua.LVAsString(t.RawGetString("description"))
		scope = lua.LVAsString(t.RawGetString("scope"))
		if v, ok := t.RawGetString("run").(*lua.LFunction); ok {
			fn = v
		}
	} else {
		id = L.CheckString(1)
		name = L.CheckString(2)
		fn = L.CheckFunction(3)
	}

	if id == "" || name == "" || fn == nil {
		L.RaiseError("kbrd.command: id, name, and run/fn are required")
		return 0
	}
	scope = normalizeScope(scope)

	ref := fmt.Sprintf("lua:%s", id)
	// Replace any existing registration with the same id so reloads work.
	for i, c := range h.commands {
		if c.ID == id {
			h.commands[i] = luaCommand{
				Name: name, ID: id, Description: description, Scope: scope,
				Ref: ref, fn: fn,
			}
			return 0
		}
	}
	h.commands = append(h.commands, luaCommand{
		Name: name, ID: id, Description: description, Scope: scope,
		Ref: ref, fn: fn,
	})
	return 0
}

// normalizeScope canonicalizes a command scope string. The empty/unknown value
// maps to "files" (the backward-compatible default: shown only on filesystem
// columns). "line" is the in-editor line-command scope (kept off every column
// menu; surfaced only by the editor's ctrl+l picker).
func normalizeScope(s string) string {
	switch s {
	case "virtual", "all", "line":
		return s
	default:
		return "files"
	}
}

// kbrd.has_command(id) → bool
// Returns true if a Lua command with this id is currently registered.
// Useful in init.lua for guarded re-registration or feature-detection.
func (h *Host) luaHasCommand(L *lua.LState) int {
	id := L.CheckString(1)
	for _, c := range h.commands {
		if c.ID == id {
			L.Push(lua.LTrue)
			return 1
		}
	}
	L.Push(lua.LFalse)
	return 1
}

// kbrd.emit(name [, payload]) → true | nil, err
//
// Publishes a custom event that every kbrd.on(name, fn) listener receives, with
// the optional payload table passed as the listener's argument. Built-in event
// names are reserved and rejected. Listeners fire after the current script
// returns (deferred), so emit never re-enters the VM mid-run — a listener that
// itself emits is fine, bounded by an internal recursion cap.
func (h *Host) luaEmit(L *lua.LState) int {
	name := L.CheckString(1)
	var data map[string]any
	if tbl := L.OptTable(2, nil); tbl != nil {
		if m, ok := fromLValue(tbl).(map[string]any); ok {
			data = m
		}
	}
	if err := h.Emit(name, data); err != nil {
		return errResult(L, err)
	}
	L.Push(lua.LTrue)
	return 1
}

// kbrd.on(event, fn)
func (h *Host) luaOn(L *lua.LState) int {
	if h.inTimer {
		L.RaiseError("kbrd.on: cannot register hooks from inside a timer callback (register from init.lua or a command body)")
		return 0
	}
	event := L.CheckString(1)
	fn := L.CheckFunction(2)
	h.hooks[event] = append(h.hooks[event], &hookEntry{fn: fn})
	return 0
}

// kbrd.board.move(item, columnName)
// item: table with .column and .name (matches ctx.item) or a string filename
//
//	(in which case columnName must already be provided)
func (h *Host) luaBoardMove(L *lua.LState) int {
	itemArg := L.Get(1)
	col := L.CheckString(2)

	var srcCol, name string
	switch v := itemArg.(type) {
	case *lua.LTable:
		// Accept either the explicit form {column=, name=} or a ctx table
		// (which uses columnName/fileName). Lets `kbrd.board.move(ctx, "done")`
		// just work without unwrapping.
		srcCol = lua.LVAsString(v.RawGetString("column"))
		if srcCol == "" {
			srcCol = lua.LVAsString(v.RawGetString("columnName"))
		}
		name = lua.LVAsString(v.RawGetString("name"))
		if name == "" {
			name = lua.LVAsString(v.RawGetString("fileName"))
		}
	case lua.LString:
		// Caller must have passed an item from ctx; raw string isn't enough
		// to identify source column. Reject early with a clear message.
		L.RaiseError("kbrd.board.move: pass ctx.item (table), not a string")
		return 0
	default:
		L.RaiseError("kbrd.board.move: first argument must be ctx.item")
		return 0
	}
	if name == "" {
		L.RaiseError("kbrd.board.move: item.name is empty")
		return 0
	}

	if err := h.api.MoveItem(events.ItemRef{Column: srcCol, Name: name}, col); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LTrue)
	return 1
}

// luaItemRef extracts {column, name} from a Lua value that is either an explicit
// {column=, name=} table or a ctx table ({columnName=, fileName=}). Mirrors the
// item-parsing in kbrd.board.move so callers can pass ctx.item directly.
func luaItemRef(L *lua.LState, fn string, v lua.LValue) events.ItemRef {
	t, ok := v.(*lua.LTable)
	if !ok {
		L.RaiseError("%s: first argument must be ctx.item (table)", fn)
		return events.ItemRef{}
	}
	col := lua.LVAsString(t.RawGetString("column"))
	if col == "" {
		col = lua.LVAsString(t.RawGetString("columnName"))
	}
	name := lua.LVAsString(t.RawGetString("name"))
	if name == "" {
		name = lua.LVAsString(t.RawGetString("fileName"))
	}
	return events.ItemRef{Column: col, Name: name}
}

// kbrd.board.create(column, name) → true | nil, err
func (h *Host) luaBoardCreate(L *lua.LState) int {
	column := L.CheckString(1)
	name := L.CheckString(2)
	if err := h.api.CreateItem(column, name); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LTrue)
	return 1
}

// kbrd.board.templates(column) → {{name=..., scope="column"|"board"}, ...} | nil, err
func (h *Host) luaBoardTemplates(L *lua.LState) int {
	column := L.CheckString(1)
	infos, err := h.api.ListTemplates(column)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	out := L.NewTable()
	for i, info := range infos {
		t := L.NewTable()
		t.RawSetString("name", lua.LString(info.Name))
		t.RawSetString("scope", lua.LString(info.Scope))
		out.RawSetInt(i+1, t)
	}
	L.Push(out)
	return 1
}

// kbrd.board.createFromTemplate(column, template, values?) → true | nil, err
//
// values maps field keys to answers: string for input/text/select, a list of
// strings for multiselect, boolean for confirm. Omitted keys take the field's
// default; required fields must be provided. When the template declares no
// filename, pass the new card's name as values._filename.
func (h *Host) luaBoardCreateFromTemplate(L *lua.LState) int {
	column := L.CheckString(1)
	template := L.CheckString(2)
	values := map[string]any{}
	if tbl := L.OptTable(3, nil); tbl != nil {
		if m, ok := fromLValue(tbl).(map[string]any); ok {
			values = m
		}
	}
	if err := h.api.CreateItemFromTemplate(column, template, values); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LTrue)
	return 1
}

// kbrd.board.rename(item, newName) → true | nil, err
func (h *Host) luaBoardRename(L *lua.LState) int {
	ref := luaItemRef(L, "kbrd.board.rename", L.Get(1))
	newName := L.CheckString(2)
	if ref.Name == "" {
		L.RaiseError("kbrd.board.rename: item.name is empty")
		return 0
	}
	if err := h.api.RenameItem(ref, newName); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LTrue)
	return 1
}

// kbrd.board.delete(item) → true | nil, err
func (h *Host) luaBoardDelete(L *lua.LState) int {
	ref := luaItemRef(L, "kbrd.board.delete", L.Get(1))
	if ref.Name == "" {
		L.RaiseError("kbrd.board.delete: item.name is empty")
		return 0
	}
	if err := h.api.DeleteItem(ref); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LTrue)
	return 1
}
