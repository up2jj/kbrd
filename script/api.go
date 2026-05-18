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
	kbrd.RawSetString("board", board)

	L.SetGlobal("kbrd", kbrd)
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
		srcCol = lua.LVAsString(v.RawGetString("column"))
		name = lua.LVAsString(v.RawGetString("name"))
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
