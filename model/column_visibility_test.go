package model

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"kbrd/config"
	"kbrd/events"
)

func newVisibilityBoard(t *testing.T, names ...string) *Board {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		colDir := filepath.Join(dir, name)
		if err := os.Mkdir(colDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(colDir, "card.md"), []byte("# "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b := NewBoard(config.Config{Path: dir, ColumnWidth: 24, PreviewLines: 3, NotifyBackend: "none"})
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	return b
}

func visibleColumnNames(b *Board) []string {
	names := make([]string, len(b.columns))
	for i, col := range b.columns {
		names[i] = col.Name
	}
	return names
}

func TestColumnVisibilityHideShowAndSelection(t *testing.T) {
	b := newVisibilityBoard(t, "1. Todo", "2. Doing", "3. Done")
	b.selectedCol = 1

	if err := b.hideColumn("2. Doing"); err != nil {
		t.Fatal(err)
	}
	if got, want := visibleColumnNames(b), []string{"1. Todo", "3. Done"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("visible columns = %v, want %v", got, want)
	}
	if b.selectedCol != 1 || b.columns[b.selectedCol].Name != "3. Done" {
		t.Fatalf("selection = %d/%q, want nearest column 1/3. Done", b.selectedCol, b.columns[b.selectedCol].Name)
	}

	// Repeated calls are successful no-ops, and show restores disk order.
	if err := b.hideColumn("2. Doing"); err != nil {
		t.Fatalf("idempotent hide: %v", err)
	}
	if err := b.showColumn("2. Doing"); err != nil {
		t.Fatal(err)
	}
	if err := b.showColumn("2. Doing"); err != nil {
		t.Fatalf("idempotent show: %v", err)
	}
	if got, want := visibleColumnNames(b), []string{"1. Todo", "2. Doing", "3. Done"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("restored columns = %v, want %v", got, want)
	}
}

func TestColumnVisibilityValidationAndShowAll(t *testing.T) {
	b := newVisibilityBoard(t, "Todo", "Done")

	if err := b.hideColumn("Missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing column error = %v", err)
	}
	b.setVirtualColumn("tasks", events.VirtualColumnSpec{Name: "Tasks"})
	if err := b.hideColumn("Tasks"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("virtual column error = %v", err)
	}
	b.clearAllVirtualColumns()
	if err := b.hideColumn("Todo"); err != nil {
		t.Fatal(err)
	}
	if err := b.hideColumn("Done"); err == nil || !strings.Contains(err.Error(), "final visible") {
		t.Fatalf("final-column error = %v", err)
	}

	// A virtual task view keeps the board operable while every filesystem
	// column is hidden.
	b.setVirtualColumn("tasks", events.VirtualColumnSpec{Name: "Tasks"})
	if err := b.hideColumn("Done"); err != nil {
		t.Fatalf("hide final filesystem column with virtual view: %v", err)
	}
	if got := visibleColumnNames(b); !reflect.DeepEqual(got, []string{"Tasks"}) {
		t.Fatalf("virtual-only view = %v", got)
	}
	b.showAllColumns()
	if got, want := visibleColumnNames(b), []string{"Done", "Todo", "Tasks"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("show all = %v, want %v", got, want)
	}
}

func TestColumnVisibilityTopLevelIntentSurvivesReloads(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Todo", "Archive"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	b := NewBoard(config.Config{Path: dir, ColumnWidth: 24, PreviewLines: 3, NotifyBackend: "none"})

	// This is the startup ordering used by top-level .kbrd.lua: hide first,
	// filesystem load second.
	if err := b.hideColumn("Archive"); err != nil {
		t.Fatal(err)
	}
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	if got := visibleColumnNames(b); !reflect.DeepEqual(got, []string{"Todo"}) {
		t.Fatalf("startup visibility = %v", got)
	}

	fresh, err := buildColumns(b.cfg, b.palette, b.itemsByPath())
	if err != nil {
		t.Fatal(err)
	}
	b.lifecycle().applyReloadedColumns(fresh)
	if got := visibleColumnNames(b); !reflect.DeepEqual(got, []string{"Todo"}) {
		t.Fatalf("full reload visibility = %v", got)
	}

	archive := b.filesystemColumn("Archive")
	if archive == nil {
		t.Fatal("hidden authoritative column missing")
	}
	if err := os.WriteFile(filepath.Join(archive.Path, "new.md"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	reloaded := buildColumn(archive.Path, archive.Name, b.cfg, b.palette, b.itemsByPath())
	b.lifecycle().HandleColumnReloaded(columnReloadedMsg{path: archive.Path, col: reloaded})
	if got := visibleColumnNames(b); !reflect.DeepEqual(got, []string{"Todo"}) {
		t.Fatalf("single-column reload visibility = %v", got)
	}
	if err := b.showColumn("Archive"); err != nil {
		t.Fatal(err)
	}
	if len(b.filesystemColumn("Archive").Items) != 1 {
		t.Fatalf("shown archive did not retain hidden reload: %+v", b.filesystemColumn("Archive").Items)
	}
}

func TestHiddenColumnRemainsScriptMutationTarget(t *testing.T) {
	b := newVisibilityBoard(t, "Todo", "Archive")
	if err := b.hideColumn("Archive"); err != nil {
		t.Fatal(err)
	}
	api := boardScriptAPI{b: b}
	if err := api.CreateItem("Archive", "hidden-task"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b.cfg.Path, "Archive", "hidden-task.md")); err != nil {
		t.Fatalf("hidden destination item: %v", err)
	}
	if got := visibleColumnNames(b); !reflect.DeepEqual(got, []string{"Todo"}) {
		t.Fatalf("mutation revealed hidden column: %v", got)
	}
}

func TestHiddenColumnsExcludedFromCurrentBoardSearch(t *testing.T) {
	b := newVisibilityBoard(t, "Todo", "Archive")
	if err := b.hideColumn("Archive"); err != nil {
		t.Fatal(err)
	}
	msg := searchResultsMsg{Results: []searchResult{
		{BoardPath: b.cfg.Path, Column: "Todo"},
		{BoardPath: b.cfg.Path, Column: "Archive"},
		{BoardPath: filepath.Join(b.cfg.Path, "other"), Column: "Archive"},
	}}
	got := b.filterHiddenSearchResults(msg).Results
	if len(got) != 2 || got[0].Column != "Todo" || samePath(got[1].BoardPath, b.cfg.Path) {
		t.Fatalf("filtered results = %+v", got)
	}
}

func TestHiddenColumnStillReceivesItemTransform(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Todo", "Archive"} {
		colDir := filepath.Join(dir, name)
		if err := os.Mkdir(colDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, card := range []string{"a", "b"} {
			if err := os.WriteFile(filepath.Join(colDir, card+".md"), []byte(card), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(`
kbrd.column.hide("Archive")
kbrd.on("column_items", function(ev)
  return { ev.items[2], ev.items[1] }
end)`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Path: dir, ColumnWidth: 24, PreviewLines: 3, NotifyBackend: "none"}
	cfg.Scripting = config.ScriptingConfig{Enabled: true, CommandTimeoutMs: 1000, HookTimeoutMs: 1000}
	b := NewBoard(cfg)
	b.initRuntime()
	defer b.Close()
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	b.applyColumnTransforms()

	archive := b.filesystemColumn("Archive")
	if archive == nil || !archive.transformed {
		t.Fatalf("hidden archive transform state = %+v", archive)
	}
	if got := itemNames(archive.Items); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Fatalf("hidden archive items = %v", got)
	}
	if got := visibleColumnNames(b); !reflect.DeepEqual(got, []string{"Todo"}) {
		t.Fatalf("transform revealed archive: %v", got)
	}
}

func TestColumnVisibilityResetsWhenSwitchingBoards(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	makeBoard := func(name string) string {
		dir := filepath.Join(t.TempDir(), name)
		for _, col := range []string{"Todo", "Archive"} {
			if err := os.MkdirAll(filepath.Join(dir, col), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	first := makeBoard("first")
	second := makeBoard("second")
	b := NewBoard(config.Config{Path: first, ColumnWidth: 24, PreviewLines: 3, NotifyBackend: "none"})
	defer b.Close()
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	if err := b.hideColumn("Archive"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.session().loadBoard(second); err != nil {
		t.Fatal(err)
	}
	if b.columnHidden("Archive") {
		t.Fatal("hidden state leaked into the next board")
	}
	if got, want := visibleColumnNames(b), []string{"Archive", "Todo"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("switched board columns = %v, want %v", got, want)
	}
}
