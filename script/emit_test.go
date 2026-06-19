package script

import (
	"errors"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

var errTestNav = errors.New("no such column")

// luaString/luaNumber/luaBool/luaStringSlice read back a global the test script
// set, so a test can assert on what the Lua side observed.
func luaString(t *testing.T, h *Host, name string) string {
	t.Helper()
	return lua.LVAsString(h.L.GetGlobal(name))
}

func luaNumber(t *testing.T, h *Host, name string) float64 {
	t.Helper()
	return float64(lua.LVAsNumber(h.L.GetGlobal(name)))
}

func luaBool(t *testing.T, h *Host, name string) bool {
	t.Helper()
	return lua.LVAsBool(h.L.GetGlobal(name))
}

func luaStringSlice(t *testing.T, h *Host, name string) []string {
	t.Helper()
	tbl, ok := h.L.GetGlobal(name).(*lua.LTable)
	if !ok {
		t.Fatalf("global %q is not a table", name)
	}
	out := make([]string, 0, tbl.Len())
	for i := 1; i <= tbl.Len(); i++ {
		out = append(out, lua.LVAsString(tbl.RawGetInt(i)))
	}
	return out
}

// TestEmitFiresListenerAfterCommand verifies that kbrd.emit reaches a
// kbrd.on listener with its payload, and that the listener fires AFTER the
// emitting command returns (the deferred-drain contract) — never re-entering
// the VM mid-run.
func TestEmitFiresListenerAfterCommand(t *testing.T) {
	dir := writeInit(t, `
order = {}
kbrd.on("ping", function(p)
  table.insert(order, "listener:"..tostring(p.n))
end)
kbrd.command("c", "Emit", function()
  table.insert(order, "before-emit")
  kbrd.emit("ping", { n = 7 })
  table.insert(order, "after-emit")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Read back the global `order` table the script built.
	got := luaStringSlice(t, h, "order")
	want := []string{"before-emit", "after-emit", "listener:7"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v (listener must fire after the command body)", got, want)
	}
}

// TestEmitMultipleListeners confirms every registered listener for a custom
// event fires.
func TestEmitMultipleListeners(t *testing.T) {
	dir := writeInit(t, `
hits = 0
kbrd.on("evt", function() hits = hits + 1 end)
kbrd.on("evt", function() hits = hits + 1 end)
kbrd.command("c", "Emit", function() kbrd.emit("evt") end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if n := luaNumber(t, h, "hits"); n != 2 {
		t.Fatalf("hits = %v, want 2", n)
	}
}

// TestEmitReservedNameRejected ensures a script cannot spoof a built-in event.
func TestEmitReservedNameRejected(t *testing.T) {
	dir := writeInit(t, `
ok, err = kbrd.emit("item_moved", {})
kbrd.command("c", "C", function() end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if v := luaBool(t, h, "ok"); v {
		t.Fatal("emit of a reserved name should return falsy, got truthy")
	}
	if s := luaString(t, h, "err"); !strings.Contains(s, "reserved") {
		t.Fatalf("err = %q, want it to mention 'reserved'", s)
	}
}

// TestEmitLoopTerminates verifies the depth guard: two listeners that re-emit
// each other must converge instead of hanging or overflowing the stack.
func TestEmitLoopTerminates(t *testing.T) {
	dir := writeInit(t, `
fires = 0
kbrd.on("a", function() fires = fires + 1; kbrd.emit("b") end)
kbrd.on("b", function() fires = fires + 1; kbrd.emit("a") end)
kbrd.command("c", "Kick", function() kbrd.emit("a") end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	// The real assertion is that this returns at all (no hang / stack overflow).
	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	n := luaNumber(t, h, "fires")
	if n == 0 {
		t.Fatal("expected the chain to fire at least once")
	}
	if n > maxEmitDepth+1 {
		t.Fatalf("fires = %v, expected the depth guard to cap the chain near %d", n, maxEmitDepth)
	}
}

// TestBoardFocus and TestBoardSelect check the navigation API routes through to
// the BoardAPI with the right arguments and surfaces errors.
func TestBoardFocus(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("f", "Focus", function() kbrd.board.focus("Done") end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.focuses) != 1 || api.focuses[0] != "Done" {
		t.Fatalf("focuses = %v, want [Done]", api.focuses)
	}
}

func TestBoardSelect(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("s", "Select", function() kbrd.board.select("Todo", "card-1") end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.selects) != 1 || api.selects[0] != "Todo/card-1" {
		t.Fatalf("selects = %v, want [Todo/card-1]", api.selects)
	}
}

func TestBoardSelectError(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("s", "Select", function()
  local ok, err = kbrd.board.select("Nope", "x")
  if not ok then kbrd.notify("err:"..err, "error") end
end)`)
	api := &fakeAPI{navErr: errTestNav}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !contains(api.notifies, "error:err:no such column") {
		t.Fatalf("expected error surfaced to script, got %v", api.notifies)
	}
}
