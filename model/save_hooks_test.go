package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"kbrd/config"
	"kbrd/events"
	"kbrd/template"
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

func TestQuickCommand_ResolvesMnemonicByStableItemIdentity(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "alpha"})
	colB := newTestColumn(t, map[string]string{"a": "bravo"})
	b := &Board{
		columns:  []*Column{colA, colB},
		editor:   NewEditor(false),
		notifier: NewNotifier(""),
	}
	b.rebuildMnemonics()

	tag := b.mnemonicLookup(1)("a")
	if tag == "" {
		t.Fatal("expected mnemonic for colB/a")
	}
	ref := b.refByMnemonic[tag]

	b.columns = []*Column{colB, colA}
	cmd := b.itemActions().dispatch('e', ref)
	if cmd == nil {
		t.Fatal("expected editor command")
	}

	if b.editor.ColPath != colB.Path {
		t.Fatalf("editor ColPath = %q, want stable target %q", b.editor.ColPath, colB.Path)
	}
}

func TestFrontmatterSubmit_ResolvesByStableItemIdentity(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "---\nstatus: old-a\n---\nalpha\n"})
	colB := newTestColumn(t, map[string]string{"a": "---\nstatus: old-b\n---\nbravo\n"})
	target := refForItem(colB, colB.ItemByName("a"))
	b := &Board{
		columns:  []*Column{colA, colB},
		notifier: NewNotifier(""),
	}

	b.columns = []*Column{colB, colA}
	b.frontmatterActions().handleSubmit(frontmatterSubmitMsg{
		Target:   target,
		ColIndex: 1,
		FileName: "a",
		Key:      "status",
		Value:    "new-b",
	})

	rawB, err := os.ReadFile(filepath.Join(colB.Path, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawB), "status: new-b") {
		t.Fatalf("target item was not updated:\n%s", rawB)
	}
	rawA, err := os.ReadFile(filepath.Join(colA.Path, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawA), "status: new-b") {
		t.Fatalf("stale index updated the wrong item:\n%s", rawA)
	}
}

func TestSearchActivatedEditSave_ResolvesByStableItemIdentity(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "alpha"})
	colB := newTestColumn(t, map[string]string{"a": "bravo"})
	b := &Board{
		cfg:      config.Config{Path: filepath.Dir(colA.Path)},
		columns:  []*Column{colA, colB},
		editor:   NewEditor(false),
		notifier: NewNotifier(""),
	}

	_, _ = b.searchActions().activateFile(b.cfg.Path, filepath.Join(colB.Path, "a.md"))
	if b.selectedCol != 1 {
		t.Fatalf("search activation selected col %d, want 1", b.selectedCol)
	}
	item := b.columns[b.selectedCol].SelectedItem()
	if item == nil || item.FullPath != filepath.Join(colB.Path, "a.md") {
		t.Fatalf("search activation selected wrong item: %+v", item)
	}
	_ = b.editor.OpenEdit(b.selectedCol, colB.Path, item.Name, item.FullPath)

	b.columns = []*Column{colB, colA}
	b.mutationHandlers().handleSave(editorSaveMsg{
		Target:   b.editor.itemTarget(),
		ColIndex: b.editor.ColIndex,
		FileName: b.editor.FileName,
		Content:  "saved",
	})

	rawB, err := os.ReadFile(filepath.Join(colB.Path, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(rawB)) != "saved" {
		t.Fatalf("search-selected target not saved, got %q", rawB)
	}
	rawA, err := os.ReadFile(filepath.Join(colA.Path, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(rawA) == "saved" {
		t.Fatal("stale index saved the wrong duplicate-name item")
	}
}

func TestMouseSelectionDelete_UsesStableRefForDelayedMutation(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "alpha"})
	colB := newTestColumn(t, map[string]string{"b": "bravo"})
	b := &Board{
		cfg:        config.Config{Path: filepath.Dir(colA.Path), ColumnWidth: 20, PreviewLines: 3},
		columns:    []*Column{colA, colB},
		termWidth:  90,
		termHeight: 30,
		notifier:   NewNotifier(""),
		editor:     NewEditor(false),
	}
	b.visibleHeight = 20
	setColumnHeights(b.columns, b.visibleHeight)
	_ = b.View()

	x, y, ok := mousePointForItem(b, 1)
	if !ok {
		t.Fatal("failed to find mouse hit point for second column item")
	}
	b.mouseRouter().HandleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if b.selectedCol != 1 {
		t.Fatalf("mouse selected col %d, want 1", b.selectedCol)
	}
	item := b.columns[b.selectedCol].SelectedItem()
	if item == nil || item.Name != "b" {
		t.Fatalf("mouse selected item %+v, want b", item)
	}
	msg := deleteConfirmMsg{Target: refForItem(b.columns[b.selectedCol], item), ColIndex: b.selectedCol, FileName: item.Name}

	b.columns = []*Column{colB, colA}
	b.mutationHandlers().handleDelete(msg)

	if _, err := os.Stat(filepath.Join(colB.Path, "b.md")); !os.IsNotExist(err) {
		t.Fatalf("target item still exists or unexpected err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(colA.Path, "a.md")); err != nil {
		t.Fatalf("stale mouse index deleted wrong item: %v", err)
	}
}

func mousePointForItem(b *Board, wantCol int) (int, int, bool) {
	width := b.termWidth
	if width == 0 {
		width = 80
	}
	header := b.statusPresenter().renderHeaderLayout(width)
	region := boardColumnsRegion{}
	region.measure(b, width)
	for x := 0; x < b.termWidth; x++ {
		colIdx, ok := region.columnAtMouse(b, x)
		if !ok || colIdx != wantCol {
			continue
		}
		col := b.columns[colIdx]
		for y := 0; y < region.columnsHeight; y++ {
			if _, ok := col.HitTest(y); ok {
				return x, y + header.height, true
			}
		}
	}
	return 0, 0, false
}

// TestHandleSave_PublishesItemSaved confirms every in-app content write fires
// ItemSaved with the right Kind, so a hook can post-process the saved card.
func TestHandleSave_PublishesItemSaved(t *testing.T) {
	col := newTestColumn(t, map[string]string{"a": "old"})
	b := &Board{columns: []*Column{col}}
	rec := &recordingSub{}
	b.bus.Subscribe(rec)

	target := refForItem(col, col.ItemByName("a"))
	b.mutationHandlers().handleSave(editorSaveMsg{Target: target, ColIndex: 0, FileName: "a", Content: "new"})
	b.mutationHandlers().handleAppend(editorAppendMsg{Target: target, ColIndex: 0, FileName: "a", Text: "more"})
	b.mutationHandlers().handlePrepend(editorPrependMsg{Target: target, ColIndex: 0, FileName: "a", Text: "top"})
	b.mutationHandlers().handleJournal(editorJournalMsg{Target: target, ColIndex: 0, FileName: "a", Text: "logged"})

	want := []events.Event{
		events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: "a"}, Kind: "save"},
		events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: "a"}, Kind: "append"},
		events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: "a"}, Kind: "prepend"},
		events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: "a"}, Kind: "journal"},
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

// A clipboard paste is an in-app content write too, so finalizing one must
// publish ItemSaved with the paste mode's Kind — append/prepend/journal map
// directly, and a whole-file replace is reported as "save". (pasteToItem's disk
// write + clipboard read run in a goroutine; handlePasteDone is the UI-goroutine
// finalizer that publishes, which is what a hook observes.)
func TestHandlePasteDone_PublishesItemSaved(t *testing.T) {
	col := newTestColumn(t, map[string]string{"a": "old"})
	b := &Board{columns: []*Column{col}}
	rec := &recordingSub{}
	b.bus.Subscribe(rec)

	done := func(kind, verb string) pasteDoneMsg {
		return pasteDoneMsg{ColName: col.Name, ColPath: col.Path, FileName: "a", Kind: kind, Verb: verb}
	}
	b.pasteActions().handleDone(done("append", "appended to "))
	b.pasteActions().handleDone(done("prepend", "prepended to "))
	b.pasteActions().handleDone(done("journal", "journaled to "))
	b.pasteActions().handleDone(done("save", "replaced "))

	want := []events.Event{
		events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: "a"}, Kind: "append"},
		events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: "a"}, Kind: "prepend"},
		events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: "a"}, Kind: "journal"},
		events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: "a"}, Kind: "save"},
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

// A paste completes asynchronously, so handlePasteDone must finalize against the
// column the write actually targeted — identified by its stable path — not
// whatever column now sits where it used to. A reorder between dispatch and
// completion must not redirect the ItemSaved/select to the wrong column, and a
// target the board no longer holds is a safe no-op.
func TestHandlePasteDone_ResolvesByStableIdentity(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "a"})
	colB := newTestColumn(t, map[string]string{"b": "b"})
	b := &Board{columns: []*Column{colA, colB}}
	rec := &recordingSub{}
	b.bus.Subscribe(rec)

	// The paste targeted colB (index 1 at dispatch); the columns are reordered
	// before completion so an index-based finalize would now hit colA.
	done := pasteDoneMsg{ColName: colB.Name, ColPath: colB.Path, FileName: "b", Kind: "append", Verb: "appended to "}
	b.columns = []*Column{colB, colA}

	b.pasteActions().handleDone(done)

	if len(rec.evs) != 1 {
		t.Fatalf("want 1 event, got %d: %+v", len(rec.evs), rec.evs)
	}
	if got := rec.evs[0].(events.ItemSaved); got.Item.Column != colB.Name || got.Item.Name != "b" {
		t.Fatalf("finalized against the wrong column: %+v (want %s/b)", got, colB.Name)
	}

	// A target the board no longer contains (e.g. board switched): no event, no panic.
	rec.evs = nil
	b.pasteActions().handleDone(pasteDoneMsg{ColName: "ghost", ColPath: "/no/such/dir", FileName: "x", Kind: "append", Verb: "appended to "})
	if len(rec.evs) != 0 {
		t.Fatalf("paste into a vanished column should no-op, got %+v", rec.evs)
	}
}

func TestEditorSave_ResolvesByStableItemIdentity(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"same": "a"})
	colB := newTestColumn(t, map[string]string{"same": "b"})
	target := colB.ItemByName("same")
	b := &Board{columns: []*Column{colA, colB}}

	// The editor opened colB/same, then columns reordered before save. An
	// index-based save would now write colA/same.
	b.columns = []*Column{colB, colA}
	b.mutationHandlers().handleSave(editorSaveMsg{
		Target:   refForItem(colB, target),
		ColIndex: 1,
		FileName: "same",
		Content:  "updated",
	})

	gotB, err := os.ReadFile(target.FullPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotB) != "updated\n" {
		t.Fatalf("target content = %q, want updated", gotB)
	}
	gotA, err := os.ReadFile(colA.ItemByName("same").FullPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotA) != "a" {
		t.Fatalf("wrong column was edited: %q", gotA)
	}
}

func TestDeleteConfirm_ResolvesByStableItemIdentity(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"same": "a"})
	colB := newTestColumn(t, map[string]string{"same": "b"})
	target := colB.ItemByName("same")
	b := &Board{columns: []*Column{colA, colB}}

	b.columns = []*Column{colB, colA}
	b.mutationHandlers().handleDelete(deleteConfirmMsg{
		Target:   refForItem(colB, target),
		ColIndex: 1,
		FileName: "same",
	})

	if _, err := os.Stat(target.FullPath); !os.IsNotExist(err) {
		t.Fatalf("target still exists or unexpected err: %v", err)
	}
	if _, err := os.Stat(colA.ItemByName("same").FullPath); err != nil {
		t.Fatalf("wrong column item was deleted: %v", err)
	}
}

func TestRenameConfirm_ResolvesByStableItemIdentity(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"same": "a"})
	colB := newTestColumn(t, map[string]string{"same": "b"})
	target := colB.ItemByName("same")
	b := &Board{columns: []*Column{colA, colB}}

	b.columns = []*Column{colB, colA}
	b.mutationHandlers().handleRenameItemConfirm(renameItemConfirmMsg{
		Target:   refForItem(colB, target),
		ColIndex: 1,
		OldName:  "same",
		NewName:  "renamed",
	})

	if _, err := os.Stat(filepath.Join(colB.Path, "renamed.md")); err != nil {
		t.Fatalf("target was not renamed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(colA.Path, "same.md")); err != nil {
		t.Fatalf("wrong column item was renamed: %v", err)
	}
}

func TestDelayedMessageConstructorsCarryStableRefs(t *testing.T) {
	col := newTestColumn(t, map[string]string{"a": "alpha"})
	item := col.ItemByName("a")
	itemRef := refForItem(col, item)
	colRef := refForColumn(col)

	if msg := newStableEditorSaveMsg(itemRef, 0, item.Name, "body"); !msg.Target.hasStableIdentity() {
		t.Fatal("editor save constructor dropped item ref")
	}
	if msg := newStableDeleteConfirmMsg(itemRef, 0, item.Name); !msg.Target.hasStableIdentity() {
		t.Fatal("delete constructor dropped item ref")
	}
	if msg := newStableRenameItemConfirmMsg(itemRef, 0, item.Name, "b"); !msg.Target.hasStableIdentity() {
		t.Fatal("rename item constructor dropped item ref")
	}
	if msg := newStableFrontmatterSubmitMsg(itemRef, 0, item.Name, "status", "new", false); !msg.Target.hasStableIdentity() {
		t.Fatal("frontmatter constructor dropped item ref")
	}
	if msg := newStableEditorNewMsg(colRef, 0, "b"); !msg.Column.hasStableIdentity() {
		t.Fatal("editor new constructor dropped column ref")
	}
	if msg := newStableTemplateSubmitMsg(colRef, 0, template.Template{Filename: "b"}, nil); !msg.Column.hasStableIdentity() {
		t.Fatal("template constructor dropped column ref")
	}
	if msg := newStableRenameColumnConfirmMsg(colRef, 0, col.Name, "renamed"); !msg.Column.hasStableIdentity() {
		t.Fatal("rename column constructor dropped column ref")
	}
}

func TestColumnMutationWrappersUseBoardopsBehavior(t *testing.T) {
	src := newTestColumn(t, map[string]string{"a": "alpha"})
	dst := newTestColumn(t, map[string]string{})

	if got, err := src.CreateItemContent("b", "bravo"); err != nil || got != "b.md" {
		t.Fatalf("CreateItemContent = %q, %v; want b.md, nil", got, err)
	}
	if _, err := os.Stat(filepath.Join(src.Path, "b.md")); err != nil {
		t.Fatalf("created item missing: %v", err)
	}

	if err := src.RenameItem("b", "c"); err != nil {
		t.Fatalf("RenameItem: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src.Path, "c.md")); err != nil {
		t.Fatalf("renamed item missing: %v", err)
	}

	if err := src.MoveItemTo(dst, "c"); err != nil {
		t.Fatalf("MoveItemTo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst.Path, "c.md")); err != nil {
		t.Fatalf("moved item missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src.Path, "c.md")); !os.IsNotExist(err) {
		t.Fatalf("moved item still in source or unexpected err: %v", err)
	}

	if err := dst.DeleteItem("c"); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst.Path, "c.md")); !os.IsNotExist(err) {
		t.Fatalf("deleted item still exists or unexpected err: %v", err)
	}
}

func TestDelayedMutations_RejectIndexOnlyTargets(t *testing.T) {
	col := newTestColumn(t, map[string]string{"a": "---\nstatus: old\n---\nbody\n"})
	mkBoard := func() *Board {
		return &Board{cfg: config.Config{Path: filepath.Dir(col.Path)}, columns: []*Column{col}, notifier: NewNotifier("none")}
	}

	t.Run("save", func(t *testing.T) {
		b := mkBoard()
		b.mutationHandlers().handleSave(editorSaveMsg{ColIndex: 0, FileName: "a", Content: "changed"})
		raw, err := os.ReadFile(filepath.Join(col.Path, "a.md"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "changed") {
			t.Fatalf("index-only save mutated file:\n%s", raw)
		}
	})

	t.Run("delete", func(t *testing.T) {
		b := mkBoard()
		b.mutationHandlers().handleDelete(deleteConfirmMsg{ColIndex: 0, FileName: "a"})
		if _, err := os.Stat(filepath.Join(col.Path, "a.md")); err != nil {
			t.Fatalf("index-only delete removed file: %v", err)
		}
	})

	t.Run("rename", func(t *testing.T) {
		b := mkBoard()
		b.mutationHandlers().handleRenameItemConfirm(renameItemConfirmMsg{ColIndex: 0, OldName: "a", NewName: "renamed"})
		if _, err := os.Stat(filepath.Join(col.Path, "renamed.md")); !os.IsNotExist(err) {
			t.Fatalf("index-only rename created target or unexpected err: %v", err)
		}
		if _, err := os.Stat(filepath.Join(col.Path, "a.md")); err != nil {
			t.Fatalf("index-only rename removed original: %v", err)
		}
	})

	t.Run("frontmatter", func(t *testing.T) {
		b := mkBoard()
		b.frontmatterActions().handleSubmit(frontmatterSubmitMsg{ColIndex: 0, FileName: "a", Key: "status", Value: "new"})
		raw, err := os.ReadFile(filepath.Join(col.Path, "a.md"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "status: new") {
			t.Fatalf("index-only frontmatter submit mutated file:\n%s", raw)
		}
	})

	t.Run("new", func(t *testing.T) {
		b := mkBoard()
		b.mutationHandlers().handleNew(editorNewMsg{ColIndex: 0, FileName: "created"})
		if _, err := os.Stat(filepath.Join(col.Path, "created.md")); !os.IsNotExist(err) {
			t.Fatalf("index-only new created file or unexpected err: %v", err)
		}
	})

	t.Run("template", func(t *testing.T) {
		b := mkBoard()
		b.mutationHandlers().handleTemplateSubmit(templateSubmitMsg{ColIndex: 0, Template: template.Template{Filename: "from-template", Body: "body"}})
		if _, err := os.Stat(filepath.Join(col.Path, "from-template.md")); !os.IsNotExist(err) {
			t.Fatalf("index-only template created file or unexpected err: %v", err)
		}
	})
}

// TestMutationsSelectTargetFile locks in that a content/create/rename mutation
// leaves its target file selected, even when a *different* card was selected
// going in. This is the guarantee that callers no longer rely on the bubbles
// cursor index happening to still point at the right item.
func TestMutationsSelectTargetFile(t *testing.T) {
	mk := func() *Board {
		col := newTestColumn(t, map[string]string{"a": "a", "b": "b"})
		return &Board{columns: []*Column{col}}
	}
	sel := func(b *Board) string {
		if it := b.columns[0].SelectedItem(); it != nil {
			return it.Name
		}
		return ""
	}

	cases := []struct {
		name string
		op   func(b *Board)
		want string
	}{
		{"append", func(b *Board) {
			b.mutationHandlers().handleAppend(editorAppendMsg{Target: refForItem(b.columns[0], b.columns[0].ItemByName("b")), ColIndex: 0, FileName: "b", Text: "x"})
		}, "b"},
		{"prepend", func(b *Board) {
			b.mutationHandlers().handlePrepend(editorPrependMsg{Target: refForItem(b.columns[0], b.columns[0].ItemByName("b")), ColIndex: 0, FileName: "b", Text: "x"})
		}, "b"},
		{"journal", func(b *Board) {
			b.mutationHandlers().handleJournal(editorJournalMsg{Target: refForItem(b.columns[0], b.columns[0].ItemByName("b")), ColIndex: 0, FileName: "b", Text: "x"})
		}, "b"},
		{"save", func(b *Board) {
			b.mutationHandlers().handleSave(editorSaveMsg{Target: refForItem(b.columns[0], b.columns[0].ItemByName("b")), ColIndex: 0, FileName: "b", Content: "x"})
		}, "b"},
		{"new", func(b *Board) {
			b.mutationHandlers().handleNew(editorNewMsg{Column: refForColumn(b.columns[0]), ColIndex: 0, FileName: "c"})
		}, "c"},
		{"rename", func(b *Board) {
			b.mutationHandlers().handleRenameItemConfirm(renameItemConfirmMsg{Target: refForItem(b.columns[0], b.columns[0].ItemByName("b")), ColIndex: 0, OldName: "b", NewName: "b2"})
		}, "b2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := mk()
			b.columns[0].SelectByName("a")
			tc.op(b)
			if got := sel(b); got != tc.want {
				t.Errorf("after %s, selected = %q want %q", tc.name, got, tc.want)
			}
		})
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
