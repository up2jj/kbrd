package script

import (
	"strings"
	"testing"
)

// A :lua expression that emits an event must defer the listener until the
// expression finishes, exactly like a command body — never firing a hook that
// re-enters the VM mid-PCall. Without the running guard in EvalWithContext the
// emit would dispatch immediately and the listener would interleave with the
// function body (order would be before,listener,after).
func TestEvalDefersEmittedHooks(t *testing.T) {
	dir := writeInit(t, `
order = {}
kbrd.on("ping", function() table.insert(order, "listener") end)
kbrd.register("emitnow", function()
  table.insert(order, "before")
  kbrd.emit("ping")
  table.insert(order, "after")
  return "ok"
end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	out, ok, err := h.Eval("emitnow()")
	if err != nil || !ok || out != "ok" {
		t.Fatalf("Eval = (%q, %v, %v), want (\"ok\", true, nil)", out, ok, err)
	}
	if h.running {
		t.Fatal("running flag leaked true after Eval returned")
	}

	got := luaStringSlice(t, h, "order")
	want := []string{"before", "after", "listener"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v (listener must fire after the expression, not re-enter mid-eval)", got, want)
	}
}

// EvalWithContext exposes the supplied ctx table to the expression (and to the
// registered functions it calls), and removes it afterward.
func TestEvalWithContext(t *testing.T) {
	dir := writeInit(t, `kbrd.register("up", function() return string.upper(ctx.line) end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	out, ok, err := h.EvalWithContext("up()", map[string]any{"line": "hello"})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !ok || out != "HELLO" {
		t.Fatalf("EvalWithContext = (%q, %v), want (%q, true)", out, ok, "HELLO")
	}

	// ctx is cleared after the call: a bare Eval referencing ctx is nil.
	if _, ok, _ := h.Eval("ctx"); ok {
		t.Fatalf("ctx leaked into a later Eval")
	}
}

// A range operand exposes ctx.lines as a table the function can iterate.
func TestEvalWithContextLines(t *testing.T) {
	dir := writeInit(t, `kbrd.register("join", function() return table.concat(ctx.lines, "+") end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	out, ok, err := h.EvalWithContext("join()", map[string]any{"lines": []any{"a", "b", "c"}})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !ok || out != "a+b+c" {
		t.Fatalf("EvalWithContext lines = (%q, %v), want (%q, true)", out, ok, "a+b+c")
	}
}

// EvalCompletions returns registered names in order with their usage hints.
func TestEvalCompletions(t *testing.T) {
	dir := writeInit(t, `
kbrd.register("indent", function(n) return n end, "indent(n) — indent line")
kbrd.register{ name = "wrap", fn = function() return "" end, usage = "wrap(w)" }
kbrd.register("plain", function() return "" end)
`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	got := h.EvalCompletions()
	if len(got) != 3 {
		t.Fatalf("completions = %v, want 3", got)
	}
	if got[0].Name != "indent" || got[0].Usage != "indent(n) — indent line" {
		t.Fatalf("completion[0] = %+v", got[0])
	}
	if got[1].Name != "wrap" || got[1].Usage != "wrap(w)" {
		t.Fatalf("completion[1] = %+v", got[1])
	}
	if got[2].Name != "plain" || got[2].Usage != "" {
		t.Fatalf("completion[2] = %+v", got[2])
	}
}

func TestEditorOpenQueue(t *testing.T) {
	h, err := New(defaultCfg(), &fakeAPI{}, nil, writeInit(t, ""), "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	// table form
	if _, _, err := h.Eval(`kbrd.editor.open({path="foo.md", line=5})`); err != nil {
		t.Fatalf("eval table: %v", err)
	}
	// string form
	if _, _, err := h.Eval(`kbrd.editor.open("bar.md", 3)`); err != nil {
		t.Fatalf("eval string: %v", err)
	}
	reqs := h.PendingEditorOpen()
	if len(reqs) != 2 {
		t.Fatalf("got %d reqs, want 2", len(reqs))
	}
	if reqs[0].Path != "foo.md" || reqs[0].Line != 5 {
		t.Fatalf("req0 = %+v", reqs[0])
	}
	if reqs[1].Path != "bar.md" || reqs[1].Line != 3 {
		t.Fatalf("req1 = %+v", reqs[1])
	}
	if h.PendingEditorOpen() != nil {
		t.Fatalf("drain should empty the queue")
	}
}
