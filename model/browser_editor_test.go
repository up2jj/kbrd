package model

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"kbrd/browseredit"
	"kbrd/events"
)

func TestBrowserLinkTargetsUseSafeFilesystemCardNames(t *testing.T) {
	todo := newTestColumn(t, map[string]string{"zeta": "", "current": "", "bad|name": ""})
	todo.Name = "Todo"
	done := newTestColumn(t, map[string]string{"Alpha": "", "zeta": ""})
	done.Name = "Done"
	b := &Board{filesystemCols: []*Column{todo, done}}

	got := b.browserLinkTargets(todo.ItemByName("current").FullPath)
	want := []browseredit.LinkTarget{
		{Name: "Alpha", Column: "Done"},
		{Name: "zeta", Column: "Done"},
		{Name: "zeta", Column: "Todo"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("link targets = %+v, want %+v", got, want)
	}
}

func TestBrowserSaveUsesBoardMutationBoundary(t *testing.T) {
	col := newTestColumn(t, map[string]string{"a": "---\nowner: disk\n---\nold\n"})
	item := col.ItemByName("a")
	expected, err := filepath.EvalSymlinks(item.FullPath)
	if err != nil {
		t.Fatal(err)
	}
	b := &Board{
		columns: []*Column{col}, notifier: NewNotifier(""), browserEditor: browseredit.New(),
		browserTargets: map[string]browserEditorTarget{"session": {Ref: refForItem(col, item), ExpectedPath: expected}},
	}
	t.Cleanup(func() { _ = b.browserEditor.Close() })
	rec := &recordingSub{}
	b.bus.Subscribe(rec)
	raw, _ := os.ReadFile(item.FullPath)
	reply := make(chan browseredit.SaveResult, 1)
	b.handleBrowserEditorSave(browserEditorSaveRequestMsg{Request: browseredit.SaveRequest{
		SessionID: "session", BaseRevision: browseredit.Revision(string(raw)), Body: "new\n", Reply: reply,
	}})
	result := <-reply
	if result.Err != nil || result.Conflict || result.Gone {
		t.Fatalf("save result = %+v", result)
	}
	got, _ := os.ReadFile(item.FullPath)
	if want := "---\nowner: disk\n---\nnew\n"; string(got) != want {
		t.Fatalf("card = %q, want %q", got, want)
	}
	if len(rec.evs) != 1 || rec.evs[0] != (events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: "a"}, Kind: "browser"}) {
		t.Fatalf("events = %+v", rec.evs)
	}
}

func TestBrowserSaveRejectsStaleAndMissingTarget(t *testing.T) {
	col := newTestColumn(t, map[string]string{"a": "old"})
	item := col.ItemByName("a")
	expected, _ := filepath.EvalSymlinks(item.FullPath)
	b := &Board{
		columns: []*Column{col}, notifier: NewNotifier(""), browserEditor: browseredit.New(),
		browserTargets: map[string]browserEditorTarget{"session": {Ref: refForItem(col, item), ExpectedPath: expected}},
	}
	t.Cleanup(func() { _ = b.browserEditor.Close() })
	rec := &recordingSub{}
	b.bus.Subscribe(rec)

	reply := make(chan browseredit.SaveResult, 1)
	b.handleBrowserEditorSave(browserEditorSaveRequestMsg{Request: browseredit.SaveRequest{
		SessionID: "session", BaseRevision: browseredit.Revision("different"), Body: "mine", Reply: reply,
	}})
	if result := <-reply; !result.Conflict || result.Gone {
		t.Fatalf("stale result = %+v", result)
	}
	if len(rec.evs) != 0 {
		t.Fatalf("stale save emitted events: %+v", rec.evs)
	}
	got, _ := os.ReadFile(item.FullPath)
	if string(got) != "old" {
		t.Fatalf("stale save changed card: %q", got)
	}

	if err := os.Remove(item.FullPath); err != nil {
		t.Fatal(err)
	}
	reply = make(chan browseredit.SaveResult, 1)
	b.handleBrowserEditorSave(browserEditorSaveRequestMsg{Request: browseredit.SaveRequest{
		SessionID: "session", BaseRevision: browseredit.Revision("old"), Body: "resurrect", Reply: reply,
	}})
	if result := <-reply; !result.Gone {
		t.Fatalf("missing result = %+v", result)
	}
	if _, err := os.Stat(item.FullPath); !os.IsNotExist(err) {
		t.Fatalf("missing card was recreated: %v", err)
	}
}
