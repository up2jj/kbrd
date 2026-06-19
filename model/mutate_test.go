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

// TestScriptAPINavigation locks in the navigation contract: FocusColumn /
// SelectItem mutate only selection state and publish NOTHING themselves — the
// board's emitSelectionChanges diff is what turns the move into a single
// column_change + item_select. Double-firing here would be the bug.
func TestScriptAPINavigation(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a1": "x"})
	colB := newTestColumn(t, map[string]string{"b1": "x", "b2": "y"})
	b := &Board{columns: []*Column{colA, colB}, selectedCol: 0}
	colA.SelectByName("a1")
	rec := &recordingSub{}
	b.bus.Subscribe(rec)
	api := boardScriptAPI{b: b}

	// FocusColumn alone publishes nothing; the wrapper's diff does.
	prevCol, prevItem := b.snapshotSelection()
	if err := api.FocusColumn(colB.Name); err != nil {
		t.Fatalf("FocusColumn: %v", err)
	}
	if b.selectedCol != 1 {
		t.Fatalf("selectedCol = %d, want 1", b.selectedCol)
	}
	if len(rec.evs) != 0 {
		t.Fatalf("FocusColumn published %+v, want nothing (the diff emits)", rec.evs)
	}
	b.emitSelectionChanges(prevCol, prevItem)
	if len(rec.evs) != 2 {
		t.Fatalf("after diff got %d events, want 2 (column_change + item_select): %+v", len(rec.evs), rec.evs)
	}

	// SelectItem moves the cursor onto the named card.
	if err := api.SelectItem(colB.Name, "b2"); err != nil {
		t.Fatalf("SelectItem: %v", err)
	}
	if sel := colB.SelectedItem(); sel == nil || sel.Name != "b2" {
		t.Fatalf("SelectedItem = %+v, want b2", sel)
	}

	// Errors are clear and leave selection put.
	if err := api.FocusColumn("ghost"); err == nil {
		t.Fatal("FocusColumn(ghost) should error")
	}
	if err := api.SelectItem(colB.Name, "missing"); err == nil {
		t.Fatal("SelectItem(missing) should error")
	}
	if b.selectedCol != 1 {
		t.Fatalf("selectedCol after failed nav = %d, want 1 (unchanged)", b.selectedCol)
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
