package script

import (
	"fmt"
	"time"

	lua "github.com/yuin/gopher-lua"

	"kbrd/events"
)

// installAPI builds the `kbrd` global on h.L. Called from New, never from
// outside the package — assumes the VM is fresh and h.mu is not yet held by
// a concurrent goroutine.
func (h *Host) installAPI() {
	L := h.L

	kbrd := L.NewTable()

	kbrd.RawSetString("notify", L.NewFunction(h.luaNotify))
	kbrd.RawSetString("command", L.NewFunction(h.luaCommand))
	kbrd.RawSetString("on", L.NewFunction(h.luaOn))
	kbrd.RawSetString("_uiGuard", L.NewFunction(h.luaUIGuard))

	board := L.NewTable()
	board.RawSetString("move", L.NewFunction(h.luaBoardMove))
	board.RawSetString("refresh", L.NewFunction(h.luaBoardRefresh))
	board.RawSetString("createColumn", L.NewFunction(h.luaBoardCreateColumn))
	kbrd.RawSetString("board", board)

	fs := L.NewTable()
	fs.RawSetString("read", L.NewFunction(h.luaFSRead))
	fs.RawSetString("write", L.NewFunction(h.luaFSWrite))
	fs.RawSetString("exists", L.NewFunction(h.luaFSExists))
	fs.RawSetString("mkdir", L.NewFunction(h.luaFSMkdir))
	fs.RawSetString("glob", L.NewFunction(h.luaFSGlob))
	kbrd.RawSetString("fs", fs)

	timer := L.NewTable()
	timer.RawSetString("every", L.NewFunction(h.luaTimerEvery))
	timer.RawSetString("after", L.NewFunction(h.luaTimerAfter))
	timer.RawSetString("cancel", L.NewFunction(h.luaTimerCancel))
	kbrd.RawSetString("timer", timer)

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

// kbrd.board.refresh() → true | nil, err
func (h *Host) luaBoardRefresh(L *lua.LState) int {
	if err := h.api.Refresh(); err != nil {
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

// kbrd.timer.every(ms, fn) → handle
// kbrd.timer.after(ms, fn) → handle
// Sub-100ms intervals are silently clamped to 100ms — protects against
// accidental tight loops starving the UI.
func (h *Host) luaTimerEvery(L *lua.LState) int  { return h.scheduleTimer(L, true) }
func (h *Host) luaTimerAfter(L *lua.LState) int  { return h.scheduleTimer(L, false) }

func (h *Host) scheduleTimer(L *lua.LState, repeat bool) int {
	if h.inTimer {
		// Forbids exponential timer pyramids and self-rescheduling polling
		// patterns. Repeating timers (re-armed by the host, not Lua) still
		// work; this only catches scripts trying to call kbrd.timer.* from
		// inside a timer body or its side-effect hooks.
		L.RaiseError("kbrd.timer: cannot schedule a timer from inside a timer callback (use kbrd.timer.every for periodic work)")
		return 0
	}
	ms := L.CheckInt(1)
	fn := L.CheckFunction(2)
	const minMs = 100
	if ms < minMs {
		ms = minMs
	}
	dur := time.Duration(ms) * time.Millisecond
	token := h.allocToken()
	h.timers[token] = &timerEntry{fn: fn, interval: dur, repeat: repeat}
	h.pendingTimers = append(h.pendingTimers, TimerSchedule{Token: token, Duration: dur})
	L.Push(lua.LString(token))
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

// kbrd.command(shortcut, name, fn) — short form
// kbrd.command{ shortcut=, name=, description=, run= } — table form
func (h *Host) luaCommand(L *lua.LState) int {
	if h.inTimer {
		L.RaiseError("kbrd.command: cannot register commands from inside a timer callback (register from init.lua or a command body)")
		return 0
	}
	var (
		shortcut    string
		name        string
		description string
		fn          *lua.LFunction
	)

	if L.GetTop() == 1 && L.Get(1).Type() == lua.LTTable {
		t := L.CheckTable(1)
		shortcut = lua.LVAsString(t.RawGetString("shortcut"))
		name = lua.LVAsString(t.RawGetString("name"))
		description = lua.LVAsString(t.RawGetString("description"))
		if v, ok := t.RawGetString("run").(*lua.LFunction); ok {
			fn = v
		}
	} else {
		shortcut = L.CheckString(1)
		name = L.CheckString(2)
		fn = L.CheckFunction(3)
	}

	if shortcut == "" || name == "" || fn == nil {
		L.RaiseError("kbrd.command: shortcut, name, and run/fn are required")
		return 0
	}
	if len([]rune(shortcut)) != 1 {
		L.RaiseError("kbrd.command: shortcut must be a single character")
		return 0
	}

	ref := fmt.Sprintf("lua:%s", shortcut)
	// Replace any existing registration with the same shortcut so reloads work.
	for i, c := range h.commands {
		if c.Shortcut == shortcut {
			h.commands[i] = luaCommand{
				Name: name, Shortcut: shortcut, Description: description,
				Ref: ref, fn: fn,
			}
			return 0
		}
	}
	h.commands = append(h.commands, luaCommand{
		Name: name, Shortcut: shortcut, Description: description,
		Ref: ref, fn: fn,
	})
	return 0
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
//       (in which case columnName must already be provided)
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
