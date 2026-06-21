package model

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/config"
	"kbrd/events"
)

// vspec builds a simple virtual-column spec with the given item titles.
func vspec(name string, titles ...string) events.VirtualColumnSpec {
	spec := events.VirtualColumnSpec{Name: name}
	for _, t := range titles {
		spec.Items = append(spec.Items, events.VirtualItem{ID: t, Title: t})
	}
	return spec
}

func TestVirtualColumn_SetAppendsAndRenders(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 2, 4)
	b.setVirtualColumn("tasks", vspec("Tasks", "alpha", "beta"))

	if len(b.columns) != 3 {
		t.Fatalf("columns = %d, want 3 (2 fs + 1 virtual)", len(b.columns))
	}
	vc := b.columns[2]
	if !vc.Virtual || vc.VID != "tasks" {
		t.Fatalf("tail column not the virtual one: virtual=%v vid=%q", vc.Virtual, vc.VID)
	}
	out := b.View()
	if !strings.Contains(out, "TASKS") {
		t.Errorf("View missing virtual column name:\n%s", out)
	}
	if !strings.Contains(out, "◇") {
		t.Errorf("View missing virtual marker glyph:\n%s", out)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("View missing virtual items:\n%s", out)
	}
}

func TestVirtualColumn_TabNavigatesInto(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 2, 4)
	b.setVirtualColumn("tasks", vspec("Tasks", "alpha"))

	b.selectedCol = 1 // last filesystem column
	b.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if b.selectedCol != 2 {
		t.Fatalf("Tab from last fs column: selectedCol = %d, want 2 (virtual)", b.selectedCol)
	}
	if !b.columns[b.selectedCol].Virtual {
		t.Fatal("Tab did not land on the virtual column")
	}
}

func TestVirtualColumn_MutationsBlocked(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 2, 4)
	b.setVirtualColumn("tasks", vspec("Tasks", "alpha"))
	vc := b.columns[2]
	real := b.columns[0]

	if _, err := b.createItem(vc, "x"); !errors.Is(err, errVirtualColumn) {
		t.Errorf("createItem on virtual: err = %v, want errVirtualColumn", err)
	}
	if err := b.deleteItem(vc, "alpha"); !errors.Is(err, errVirtualColumn) {
		t.Errorf("deleteItem on virtual: err = %v, want errVirtualColumn", err)
	}
	if err := b.renameItem(vc, "alpha", "z"); !errors.Is(err, errVirtualColumn) {
		t.Errorf("renameItem on virtual: err = %v, want errVirtualColumn", err)
	}
	if err := b.moveItem(real, vc, "alpha"); !errors.Is(err, errVirtualColumn) {
		t.Errorf("moveItem into virtual: err = %v, want errVirtualColumn", err)
	}
	if err := b.moveItem(vc, real, "alpha"); !errors.Is(err, errVirtualColumn) {
		t.Errorf("moveItem out of virtual: err = %v, want errVirtualColumn", err)
	}
}

func TestVirtualColumn_EmptyPersistsAndShowsPlaceholder(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 4)
	b.setVirtualColumn("tasks", events.VirtualColumnSpec{Name: "Tasks", Empty: "no open tasks"})

	if len(b.columns) != 2 {
		t.Fatalf("empty virtual column not present: %d columns", len(b.columns))
	}
	// A full reload (watcher) must not drop the virtual column.
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	if len(b.columns) != 2 || !b.columns[1].Virtual {
		t.Fatalf("virtual column lost across reload: %d columns", len(b.columns))
	}
	out := b.View()
	if !strings.Contains(out, "no open tasks") {
		t.Errorf("empty placeholder not rendered:\n%s", out)
	}
}

func TestVirtualColumn_ClearRemoves(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 2, 4)
	b.setVirtualColumn("tasks", vspec("Tasks", "alpha"))
	b.setVirtualColumn("other", vspec("Other", "beta"))
	if len(b.columns) != 4 {
		t.Fatalf("want 4 columns, got %d", len(b.columns))
	}
	b.clearVirtualColumn("tasks")
	if len(b.columns) != 3 {
		t.Fatalf("clear did not remove: %d columns", len(b.columns))
	}
	if b.virtualColumn("tasks") != nil {
		t.Error("tasks still in registry after clear")
	}
	b.clearAllVirtualColumns()
	if len(b.columns) != 2 {
		t.Fatalf("clearAll left virtual columns: %d", len(b.columns))
	}
	for _, c := range b.columns {
		if c.Virtual {
			t.Fatal("virtual column survived clearAll")
		}
	}
}

func TestVirtualColumn_CursorStableAcrossRepush(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 4)
	b.setVirtualColumn("tasks", vspec("Tasks", "a", "b", "c"))
	vc := b.columns[1]
	vc.SelectByName("b")

	// Re-push without "a": cursor should stay on "b".
	b.setVirtualColumn("tasks", vspec("Tasks", "b", "c"))
	if sel := vc.SelectedItem(); sel == nil || sel.Name != "b" {
		t.Fatalf("cursor not preserved by id: got %v", sel)
	}

	// Re-push without "b" (the selected one): clamps to a valid item, no panic.
	vc.SelectByName("c")
	b.setVirtualColumn("tasks", vspec("Tasks", "x", "y"))
	if sel := vc.SelectedItem(); sel == nil {
		t.Fatal("no selection after selected id vanished")
	}
}

func TestVirtualColumn_ScopeFiltering(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 4)
	// Give the real column a selected item so scope, not item-presence, is what
	// the filtering below exercises.
	if err := os.WriteFile(filepath.Join(b.columns[0].Path, "card.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := b.columns[0].LoadItems(); err != nil {
		t.Fatalf("LoadItems: %v", err)
	}
	b.setVirtualColumn("tasks", events.VirtualColumnSpec{
		Name:     "Tasks",
		Items:    []events.VirtualItem{{ID: "a", Title: "a"}},
		Commands: []events.VirtualCommand{{ID: "done", Name: "Done", Ref: "vcol:tasks:done"}},
	})
	b.commands = []config.Command{
		{ID: "files-only", Name: "FilesOnly", Scope: "files"},
		{ID: "virt-only", Name: "VirtOnly", Scope: "virtual"},
		{ID: "both", Name: "Both", Scope: "all"},
		{ID: "default", Name: "Default"}, // empty scope == files
	}

	real := b.commandsForColumn(b.columns[0])
	if has(real, "VirtOnly") || !has(real, "FilesOnly") || !has(real, "Both") || !has(real, "Default") {
		t.Errorf("real column scope wrong: %v", names(real))
	}
	if has(real, "Done") {
		t.Error("column-scoped command leaked onto a real column")
	}

	virt := b.commandsForColumn(b.columns[1])
	if !has(virt, "Done") || !has(virt, "VirtOnly") || !has(virt, "Both") {
		t.Errorf("virtual column missing expected commands: %v", names(virt))
	}
	if has(virt, "FilesOnly") || has(virt, "Default") {
		t.Errorf("files-scoped command leaked onto virtual column: %v", names(virt))
	}
	// Column-scoped command ranks first.
	if names(virt)[0] != "Done" {
		t.Errorf("column-scoped command not first: %v", names(virt))
	}
}

func TestCommandsForColumn_EmptyColumnItemFilter(t *testing.T) {
	t.Parallel()
	no := false
	b := boardWithNCols(t, 1, 4)
	// An empty virtual column with one item-independent column command and one
	// that needs an item. The real column (columns[0]) is over an empty dir, so
	// it has no selected item either.
	b.setVirtualColumn("tasks", events.VirtualColumnSpec{
		Name: "Tasks",
		Commands: []events.VirtualCommand{
			{ID: "add", Name: "Add", RequiresItem: false, Ref: "vcol:tasks:add"},
			{ID: "done", Name: "Done", RequiresItem: true, Ref: "vcol:tasks:done"},
		},
	})
	b.commands = []config.Command{
		{ID: "sync", Name: "Sync", Scope: "all", RequiresItem: &no},
		{ID: "files-add", Name: "FilesAdd", Scope: "files", RequiresItem: &no},
		{ID: "edit", Name: "Edit", Scope: "all"}, // requires item (default)
	}

	// Empty real column: only requiresItem: false files/all globals survive.
	real := b.commandsForColumn(b.columns[0])
	if !has(real, "Sync") || !has(real, "FilesAdd") {
		t.Errorf("empty real column dropped item-independent commands: %v", names(real))
	}
	if has(real, "Edit") {
		t.Errorf("empty real column kept an item-requiring command: %v", names(real))
	}

	// Empty virtual column: item-independent column command + virtual/all
	// item-independent globals; item-requiring ones gone.
	virt := b.commandsForColumn(b.columns[1])
	if !has(virt, "Add") || !has(virt, "Sync") {
		t.Errorf("empty virtual column dropped item-independent commands: %v", names(virt))
	}
	if has(virt, "Done") || has(virt, "Edit") {
		t.Errorf("empty virtual column kept item-requiring commands: %v", names(virt))
	}
}

func TestVirtualColumn_SeparatorInert(t *testing.T) {
	t.Parallel()
	sep := Item{Separator: true, Virtual: true, Title: "Group"}
	if sep.FilterValue() != "" {
		t.Errorf("separator FilterValue = %q, want empty (excluded from filter)", sep.FilterValue())
	}

	b := boardWithNCols(t, 1, 4)
	b.setVirtualColumn("tasks", events.VirtualColumnSpec{
		Name: "Tasks",
		Items: []events.VirtualItem{
			{Separator: true, Title: "Group"},
			{ID: "real", Title: "real-task"},
		},
	})
	b.rebuildMnemonics()
	// The separator (Name "Group") must get no mnemonic; the real item does.
	vc := b.columns[1]
	if tag := b.mnemonicLookup(1)("Group"); tag != "" {
		t.Error("separator should not get a mnemonic")
	}
	if tag := b.mnemonicByRef[refForItem(vc, &vc.Items[1])]; tag == "" {
		t.Error("real virtual item should get a mnemonic")
	}
}

// TestVirtualColumn_EndToEnd drives the real Lua host: a .kbrd.lua registers a
// virtual column with a command, then pressing the command key on that column
// runs the script, which writes a file we can observe.
func TestVirtualColumn_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.txt")
	script := `
kbrd.column.set("tasks", {
  name = "Tasks",
  items = { { id = "t1", title = "first", data = { out = "` + out + `" } } },
  commands = {
    { id = "touch", name = "Touch", key = "c", default = true,
      run = function(ctx) kbrd.fs.write(ctx.data.out, "done") end },
  },
})`
	if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{Path: dir, ColumnWidth: 20, PreviewLines: 3}
	cfg.Scripting = config.ScriptingConfig{Enabled: true, CommandTimeoutMs: 2000, HookTimeoutMs: 500, InstructionLimit: 10_000_000}
	b := NewBoard(cfg)
	if b.scripts == nil {
		t.Fatal("scripting host not initialized")
	}
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	b.termWidth = 200
	b.termHeight = 40
	b.visibleHeight = 32
	for _, c := range b.columns {
		c.SetHeight(b.visibleHeight)
	}

	// 1 real column (todo) + 1 virtual (tasks).
	if len(b.columns) != 2 || !b.columns[1].Virtual {
		t.Fatalf("virtual column not attached: %d columns", len(b.columns))
	}

	// Focus the virtual column and press the command key.
	b.selectedCol = 1
	_, cmd := b.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	drain(cmd) // run the produced tea.Cmd(s) so the script executes

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("command did not write output file: %v", err)
	}
	if string(got) != "done" {
		t.Fatalf("output = %q, want %q", got, "done")
	}
}

// TestVirtualColumn_BoardLoadFires verifies board_load is actually published on
// startup (it drives the canonical kbrd.on("board_load", ...) trigger).
func TestVirtualColumn_BoardLoadFires(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	// On board_load, register a (static) virtual column — no async needed.
	script := `kbrd.on("board_load", function()
  kbrd.column.set("tasks", { name = "Tasks", items = { { id = "a", title = "alpha" } } })
end)`
	if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{Path: dir, ColumnWidth: 20, PreviewLines: 3}
	cfg.Scripting = config.ScriptingConfig{Enabled: true, CommandTimeoutMs: 2000, HookTimeoutMs: 500, InstructionLimit: 10_000_000}
	b := NewBoard(cfg)
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	// Before board_load, only the filesystem column exists.
	if len(b.columns) != 1 {
		t.Fatalf("pre-load: %d columns, want 1", len(b.columns))
	}
	// Drive the startup signal that publishes board_load.
	b.Update(watchStartMsg{})
	if len(b.columns) != 2 || !b.columns[1].Virtual {
		t.Fatalf("board_load did not create the virtual column: %d columns", len(b.columns))
	}
}

// drain executes a tea.Cmd (and any batched children) synchronously so the
// side effects of a dispatched command land before assertions.
func drain(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	_ = cmd()
}

func has(cmds []config.Command, name string) bool {
	for _, c := range cmds {
		if c.Name == name {
			return true
		}
	}
	return false
}

func names(cmds []config.Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}
