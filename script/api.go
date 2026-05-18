package script

import (
	"fmt"

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

	L.SetGlobal("kbrd", kbrd)
}

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
	event := L.CheckString(1)
	fn := L.CheckFunction(2)
	h.hooks[event] = append(h.hooks[event], fn)
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
