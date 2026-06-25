package model

import (
	"testing"

	"kbrd/config"
	"kbrd/events"
)

func testCfg() config.Config {
	return config.Config{Path: "/board", BoardName: "Board", Hooks: config.HooksConfig{TimeoutMs: 1000}}
}

func newTestRunner(hooks []config.Hook) *hookRunner {
	return newHookRunner(testCfg(), hooks)
}

func TestHookRunner_QueuesInDefinitionOrder(t *testing.T) {
	r := newTestRunner([]config.Hook{
		{Name: "a", ID: "a", Event: events.NameItemCreated, Template: "echo a"},
		{Name: "b", ID: "b", Event: events.NameItemCreated, Template: "echo b"},
		{Name: "m", ID: "m", Event: events.NameItemMoved, Template: "echo {{.fromColumn}}-{{.toColumn}}"},
	})
	r.OnEvent(events.ItemCreated{Item: events.ItemRef{Column: "Todo", Name: "x"}})
	r.OnEvent(events.ItemMoved{Item: events.ItemRef{Column: "Todo", Name: "x"}, From: "Todo", To: "Done"})

	if len(r.queue) != 3 {
		t.Fatalf("queue len = %d want 3 (%+v)", len(r.queue), r.queue)
	}
	if r.queue[0].name != "a" || r.queue[1].name != "b" || r.queue[2].name != "m" {
		t.Fatalf("queue order: %+v", r.queue)
	}
	if r.queue[2].shell != "echo Todo-Done" {
		t.Errorf("moved render = %q want %q", r.queue[2].shell, "echo Todo-Done")
	}
}

func TestHookRunner_SequentialDrain(t *testing.T) {
	r := newTestRunner([]config.Hook{
		{Name: "a", ID: "a", Event: events.NameItemCreated, Template: "echo a"},
		{Name: "b", ID: "b", Event: events.NameItemCreated, Template: "echo b"},
	})
	r.OnEvent(events.ItemCreated{Item: events.ItemRef{Column: "C", Name: "x"}})
	b := &Board{cfg: testCfg(), hooks: r}
	hooks := boardHooks{board: b}

	// First drain starts exactly one hook.
	if cmd := hooks.collectCmd(); cmd == nil {
		t.Fatal("first collectCmd returned nil")
	}
	if !r.running || len(r.queue) != 1 {
		t.Fatalf("after first drain: running=%v queueLen=%d want running=true len=1", r.running, len(r.queue))
	}
	// Second drain is a no-op while a hook is running (one at a time).
	if cmd := hooks.collectCmd(); cmd != nil {
		t.Fatal("second collectCmd should be nil while a hook runs")
	}
	// Finishing clears the flag; the next drain starts the second hook.
	hooks.handleDone(hookDoneMsg{Name: "a"})
	if r.running {
		t.Fatal("running should be cleared after handleDone")
	}
	if cmd := hooks.collectCmd(); cmd == nil {
		t.Fatal("third collectCmd returned nil; second hook never started")
	}
	if len(r.queue) != 0 {
		t.Fatalf("queue should be empty after draining both, got %d", len(r.queue))
	}
}

func TestHookRunner_IgnoresNonHookableEvents(t *testing.T) {
	r := newTestRunner([]config.Hook{
		{Name: "a", ID: "a", Event: events.NameItemCreated, Template: "echo a"},
	})
	// High-frequency events are not hookable from YAML; they must not queue.
	r.OnEvent(events.ItemSelect{Item: events.ItemRef{Column: "C", Name: "x"}})
	r.OnEvent(events.ColumnChange{Column: "C", Prev: "D"})
	r.OnEvent(events.BoardRefresh{Reason: "watcher"})
	if len(r.queue) != 0 {
		t.Fatalf("queue should be empty for non-hookable events, got %d (%+v)", len(r.queue), r.queue)
	}
}
