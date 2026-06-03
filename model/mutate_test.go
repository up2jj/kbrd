package model

import (
	"testing"

	"kbrd/events"
)

type recordingSub struct{ evs []events.Event }

func (r *recordingSub) OnEvent(e events.Event) { r.evs = append(r.evs, e) }

// TestMutationHelpers_PublishEvents locks in the core guarantee of the
// centralized helpers: every mutation publishes its event. This is what lets
// every entry point (user keys, Lua API) fire hooks without remembering to
// publish — the bug that previously left manual moves silent.
func TestMutationHelpers_PublishEvents(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"task": "hi"})
	colB := newTestColumn(t, nil)
	b := &Board{columns: []*Column{colA, colB}}
	rec := &recordingSub{}
	b.bus.Subscribe(rec)

	if _, err := b.createItem(colA, "fresh"); err != nil {
		t.Fatalf("createItem: %v", err)
	}
	if err := b.renameItem(colA, "fresh", "renamed"); err != nil {
		t.Fatalf("renameItem: %v", err)
	}
	if err := b.moveItem(colA, colB, "task"); err != nil {
		t.Fatalf("moveItem: %v", err)
	}
	if err := b.deleteItem(colA, "renamed"); err != nil {
		t.Fatalf("deleteItem: %v", err)
	}

	want := []events.Event{
		events.ItemCreated{Item: events.ItemRef{Column: colA.Name, Name: "fresh"}},
		events.ItemRenamed{Item: events.ItemRef{Column: colA.Name, Name: "renamed"}, OldName: "fresh"},
		events.ItemMoved{Item: events.ItemRef{Column: colA.Name, Name: "task"}, From: colA.Name, To: colB.Name},
		events.ItemDeleted{Column: colA.Name, Name: "renamed"},
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
