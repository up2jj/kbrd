package model

import (
	"os"
	"path/filepath"
	"strings"
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

// TestScriptAPICreateItemFromTemplate exercises the full Lua-facing path:
// a real template file on disk, defaults applied, render, create, and the
// ItemCreated event — without a running Lua VM.
func TestScriptAPICreateItemFromTemplate(t *testing.T) {
	col := newTestColumn(t, nil)
	boardDir := filepath.Dir(col.Path)
	tmplDir := filepath.Join(col.Path, ".kbrd_templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tmpl := `---
name: Bug report
filename: "bug-{{slug .title}}"
steps:
  - fields:
      - {key: title, type: input, required: true}
      - {key: severity, type: select, options: [low, high], default: low}
---
# {{.title}} ({{.severity}})
`
	if err := os.WriteFile(filepath.Join(tmplDir, "bug.md"), []byte(tmpl), 0o644); err != nil {
		t.Fatal(err)
	}

	b := &Board{columns: []*Column{col}}
	b.cfg.Path = boardDir
	rec := &recordingSub{}
	b.bus.Subscribe(rec)
	api := boardScriptAPI{b: b}

	infos, err := api.ListTemplates(col.Name)
	if err != nil || len(infos) != 1 || infos[0].Name != "Bug report" || infos[0].Scope != "column" {
		t.Fatalf("ListTemplates = %+v, %v", infos, err)
	}

	if err := api.CreateItemFromTemplate(col.Name, "Bug report", map[string]any{"title": "It broke"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(col.Path, "bug-it-broke.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# It broke (low)\n" {
		t.Errorf("content = %q", data)
	}
	want := events.ItemCreated{Item: events.ItemRef{Column: col.Name, Name: "bug-it-broke"}}
	if len(rec.evs) != 1 || rec.evs[0] != events.Event(want) {
		t.Errorf("events = %+v", rec.evs)
	}

	// Unknown template names enumerate what exists.
	err = api.CreateItemFromTemplate(col.Name, "Nope", nil)
	if err == nil || !strings.Contains(err.Error(), "Bug report") {
		t.Errorf("err = %v, want available-template listing", err)
	}
}

// TestCreateItemContent locks in the template path: the file is created with
// the rendered body and the same ItemCreated event fires.
func TestCreateItemContent(t *testing.T) {
	col := newTestColumn(t, nil)
	b := &Board{columns: []*Column{col}}
	rec := &recordingSub{}
	b.bus.Subscribe(rec)

	if _, err := b.createItemContent(col, "card", "# Title\nbody"); err != nil {
		t.Fatalf("createItemContent: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(col.Path, "card.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# Title\nbody\n" {
		t.Errorf("content = %q", data)
	}
	want := events.ItemCreated{Item: events.ItemRef{Column: col.Name, Name: "card"}}
	if len(rec.evs) != 1 || rec.evs[0] != events.Event(want) {
		t.Errorf("events = %+v, want [%+v]", rec.evs, want)
	}
}
