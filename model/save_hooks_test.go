package model

import (
	"os"
	"path/filepath"
	"testing"

	"kbrd/config"
	"kbrd/events"
)

// TestHookRunner_RendersSaveEvents locks in that the two new save-related events
// are hookable and render their variables: item_saved exposes {{.kind}} (like
// item_open), item_changed exposes the standard item vars.
func TestHookRunner_RendersSaveEvents(t *testing.T) {
	r := newTestRunner([]config.Hook{
		{Name: "s", ID: "s", Event: events.NameItemSaved, Template: "echo {{.kind}} {{.fileName}}"},
		{Name: "c", ID: "c", Event: events.NameItemChanged, Template: "echo {{.fileName}} {{.columnName}}"},
	})
	r.OnEvent(events.ItemSaved{Item: events.ItemRef{Column: "Todo", Name: "x"}, Kind: "append"})
	r.OnEvent(events.ItemChanged{Item: events.ItemRef{Column: "Todo", Name: "x"}})

	if len(r.queue) != 2 {
		t.Fatalf("queue len = %d want 2 (%+v)", len(r.queue), r.queue)
	}
	if r.queue[0].shell != "echo append x" {
		t.Errorf("item_saved render = %q want %q", r.queue[0].shell, "echo append x")
	}
	if r.queue[1].shell != "echo x Todo" {
		t.Errorf("item_changed render = %q want %q", r.queue[1].shell, "echo x Todo")
	}
}

// TestHandleSave_PublishesItemSaved confirms every in-app content write fires
// ItemSaved with the right Kind, so a hook can post-process the saved card.
func TestHandleSave_PublishesItemSaved(t *testing.T) {
	col := newTestColumn(t, map[string]string{"a": "old"})
	b := &Board{columns: []*Column{col}}
	rec := &recordingSub{}
	b.bus.Subscribe(rec)

	b.handleSave(editorSaveMsg{ColIndex: 0, FileName: "a", Content: "new"})
	b.handleAppend(editorAppendMsg{ColIndex: 0, FileName: "a", Text: "more"})
	b.handlePrepend(editorPrependMsg{ColIndex: 0, FileName: "a", Text: "top"})

	want := []events.Event{
		events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: "a"}, Kind: "save"},
		events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: "a"}, Kind: "append"},
		events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: "a"}, Kind: "prepend"},
	}
	if len(rec.evs) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(rec.evs), len(want), rec.evs)
	}
	for i := range want {
		if rec.evs[i] != want[i] {
			t.Errorf("event %d: got %+v want %+v", i, rec.evs[i], want[i])
		}
	}
}

// TestPublishItemChanges_GatesOnContentHash is the convergence guarantee for the
// item_changed watcher event: a reload that leaves a card's bytes unchanged
// fires nothing (so an idempotent rewriting hook settles), a real content change
// fires ItemChanged, and a newly present file fires too.
func TestPublishItemChanges_GatesOnContentHash(t *testing.T) {
	col := newTestColumn(t, map[string]string{"a": "hello"})
	b := &Board{columns: []*Column{col}}
	rec := &recordingSub{}
	b.bus.Subscribe(rec)
	pathA := col.Items[0].FullPath

	// Identical bytes on reload (the idempotent-rewrite case): no event.
	b.changes.snapshot(map[string]struct{}{pathA: {}}, b.columns)
	col.LoadItems()
	b.publishItemChanges()
	if len(rec.evs) != 0 {
		t.Fatalf("unchanged content should fire nothing, got %+v", rec.evs)
	}
	if b.changes.prior != nil {
		t.Error("publishItemChanges should clear the snapshot")
	}

	// Real content change: one ItemChanged.
	b.changes.snapshot(map[string]struct{}{pathA: {}}, b.columns)
	if err := os.WriteFile(pathA, []byte("hello, world — changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	col.LoadItems()
	b.publishItemChanges()
	if len(rec.evs) != 1 || rec.evs[0] != (events.ItemChanged{Item: events.ItemRef{Column: col.Name, Name: "a"}}) {
		t.Fatalf("changed content should fire one ItemChanged, got %+v", rec.evs)
	}

	// A newly present file (sentinel-0 prior hash) fires too.
	rec.evs = nil
	newPath := filepath.Join(col.Path, "b.md")
	b.changes.snapshot(map[string]struct{}{newPath: {}}, b.columns)
	if err := os.WriteFile(newPath, []byte("brand new"), 0o644); err != nil {
		t.Fatal(err)
	}
	col.LoadItems()
	b.publishItemChanges()
	if len(rec.evs) != 1 || rec.evs[0] != (events.ItemChanged{Item: events.ItemRef{Column: col.Name, Name: "b"}}) {
		t.Fatalf("new file should fire one ItemChanged for b, got %+v", rec.evs)
	}
}
