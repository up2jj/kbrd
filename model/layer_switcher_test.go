package model

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"kbrd/config"
	"kbrd/events"
	"kbrd/script"
)

func TestLayerSwitcherWidthStableAcrossUnevenRows(t *testing.T) {
	s := LayerSwitcher{palette: DarkPalette()}
	s.Open([]script.LayerInfo{
		{ID: "short", Name: "Short", Default: true},
		{ID: "long", Name: "A substantially longer layer", Description: "with a longer description"},
	}, "short")

	initial := s.View(120, 30)
	shortSelected := lipgloss.Width(initial)
	s.Move(1)
	longSelected := lipgloss.Width(s.View(120, 30))
	if shortSelected != longSelected {
		t.Fatalf("switcher width changed with selection: short=%d long=%d", shortSelected, longSelected)
	}

	s.Append("short")
	filtered := s.View(120, 30)
	if got := lipgloss.Width(filtered); got != shortSelected {
		t.Fatalf("switcher width changed while filtering: initial=%d filtered=%d", shortSelected, got)
	}
	if got, want := lipgloss.Height(filtered), lipgloss.Height(initial); got != want {
		t.Fatalf("switcher height changed while filtering: initial=%d filtered=%d", want, got)
	}
}

func TestLayerSwitcherAllowsOpenKeyInFilter(t *testing.T) {
	s := LayerSwitcher{palette: DarkPalette()}
	s.Open([]script.LayerInfo{
		{ID: "work", Name: "Work", Default: true},
		{ID: "personal", Name: "Personal"},
	}, "work")

	for _, r := range "personal" {
		s.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	if !s.Active() {
		t.Fatal("typing l closed the layer switcher")
	}
	index, ok := s.SelectedIndex()
	if !ok || s.layers[index].ID != "personal" {
		t.Fatalf("selected layer = %q, want personal", s.layers[index].ID)
	}
}

func TestLayerSwitcherChangesCommandsAndVirtualColumns(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(`
kbrd.layer{ id="work", name="Work", description="work context", default=true, setup=function()
  kbrd.command("mode", "Work command", function() end)
  kbrd.column.set("work", { name="Work view", items={} })
end }
kbrd.layer{ id="home", name="Home", description="home context", setup=function()
  kbrd.command("mode", "Home command", function() end)
end }`), 0o644); err != nil {
		t.Fatal(err)
	}
	b := newLayerTestBoard(t, dir)
	if len(b.columns) != 2 || !b.columns[1].Virtual {
		t.Fatalf("default columns = %+v", b.columns)
	}
	if len(b.commands) != 1 || b.commands[0].Name != "Work command" {
		t.Fatalf("default commands = %+v", b.commands)
	}
	b.statusPresenter().updateBuiltinCells()
	assertBuiltinCellText(t, b, builtinCellLayer, "◆ layer Work")

	if _, cmd := b.handleKey(keyPressText("l")); cmd != nil || !b.layerSwitcher.Active() {
		t.Fatal("l did not open the layer switcher")
	}
	view := b.layerSwitcher.View(100, 30)
	if !strings.Contains(view, "Switch layer") || !strings.Contains(view, "work context") || !strings.Contains(view, "★") {
		t.Fatalf("switcher view missing metadata:\n%s", view)
	}
	for _, r := range "home" {
		_, _ = b.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	b.selectedCol = 1
	b.zoom.Toggle()
	_, cmd := b.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did not schedule a layer switch")
	}
	msg := cmd()
	if _, ok := msg.(switchLayerMsg); !ok {
		t.Fatalf("switch command produced %T", msg)
	}
	_, _ = b.Update(msg)
	if len(b.columns) != 1 || b.columns[0].Virtual {
		t.Fatalf("columns after switch = %+v", b.columns)
	}
	if b.selectedCol != 0 || b.zoom.Active() {
		t.Fatalf("selection/zoom after removed virtual column = %d/%v", b.selectedCol, b.zoom.Active())
	}
	if len(b.commands) != 1 || b.commands[0].Name != "Home command" {
		t.Fatalf("commands after switch = %+v", b.commands)
	}
	b.statusPresenter().updateBuiltinCells()
	assertBuiltinCellText(t, b, builtinCellLayer, "◆ layer Home")
}

func TestLayerCellShowsNoneWhenDefaultActivationFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(`
kbrd.layer{ id="broken", name="Broken", default=true, setup=function()
  error("cannot activate")
end }
kbrd.layer{ id="other", setup=function() end }`), 0o644); err != nil {
		t.Fatal(err)
	}
	b := newLayerTestBoard(t, dir)
	b.statusPresenter().updateBuiltinCells()
	assertBuiltinCellText(t, b, builtinCellLayer, "◇ layer none")
	assertBuiltinCellText(t, b, builtinCellScriptError, "✕ lua")
	if !strings.Contains(b.scriptInitError, "cannot activate") {
		t.Fatalf("script init error = %q", b.scriptInitError)
	}
	if got := b.cells.cells[builtinCellLayer.id()].FG; got != string(b.palette.Warning) {
		t.Fatalf("inactive layer color = %q", got)
	}
}

func TestLayerSwitchFailureReopensPickerWithPersistentFeedback(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(`
kbrd.layer{ id="ok", name="Working", default=true, setup=function() end }
kbrd.layer{ id="broken", name="Broken", setup=function() error("boom") end }`), 0o644); err != nil {
		t.Fatal(err)
	}
	b := newLayerTestBoard(t, dir)

	_, cmd := b.handleSwitchLayer(switchLayerMsg{ID: "broken"})
	if cmd == nil {
		t.Fatal("failed switch did not notify")
	}
	if !b.layerSwitcher.Active() {
		t.Fatal("failed switch did not reopen the layer picker")
	}
	if view := b.layerSwitcher.View(100, 30); !strings.Contains(view, "boom") {
		t.Fatalf("layer picker missing setup error:\n%s", view)
	}
	if active, ok := b.scripts.ActiveLayer(); !ok || active.ID != "ok" {
		t.Fatalf("active layer after failure = %+v, %v", active, ok)
	}
	if !strings.Contains(b.scriptLayerError, "boom") {
		t.Fatalf("persistent layer error = %q", b.scriptLayerError)
	}
	b.statusPresenter().updateBuiltinCells()
	assertBuiltinCellText(t, b, builtinCellScriptError, "✕ lua")
	if len(b.commandWarnings) == 0 || b.commandWarnings[len(b.commandWarnings)-1].Source != layerWarningSource {
		t.Fatalf("layer warning missing from command details: %+v", b.commandWarnings)
	}

	_, _ = b.handleSwitchLayer(switchLayerMsg{ID: "ok"})
	if b.scriptLayerError != "" {
		t.Fatalf("layer error survived successful switch: %q", b.scriptLayerError)
	}
	b.statusPresenter().updateBuiltinCells()
	if cell := b.cells.cells[builtinCellScriptError.id()]; cell != nil {
		t.Fatalf("script error cell survived successful switch: %+v", cell)
	}
}

func TestLayerSwitcherReturnsFromAsyncVirtualOnlyLayer(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"1. To do", "2. In progress", "archive"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(`
local function scan(id, ready)
  kbrd.async.run("scan", function()
    kbrd.column.set(id, { name=id, items={} })
    if ready then ready() end
  end)
end

kbrd.layer{ id="default", name="Default", default=true, setup=function()
  scan("tasks")
  kbrd.column.show_all()
end }
kbrd.layer{ id="focus", name="Task focus", setup=function()
  local hidden = false
  scan("tasks-todo", function()
    if hidden then return end
    hidden = true
    assert(kbrd.column.hide("1. To do"))
    assert(kbrd.column.hide("2. In progress"))
    assert(kbrd.column.hide("archive"))
  end)
  scan("tasks-doing")
end }`), 0o644); err != nil {
		t.Fatal(err)
	}
	b := newLayerTestBoard(t, dir)

	_, _ = b.handleSwitchLayer(switchLayerMsg{ID: "focus"})
	pending := b.scripts.PendingAsync()
	if len(pending) != 2 {
		t.Fatalf("focus async scans = %d, want 2", len(pending))
	}
	for _, cmd := range pending {
		if err := b.scripts.FireAsync(cmd.Token, "", 1, ""); err != nil {
			t.Fatalf("finish focus scan: %v", err)
		}
	}
	if len(b.hiddenColumns) != 3 || len(b.columns) != 2 {
		t.Fatalf("focused columns = visible %d, hidden %v", len(b.columns), b.hiddenColumns)
	}
	b.selectedCol = 0

	_, cmd := b.handleSwitchLayer(switchLayerMsg{ID: "default"})
	if cmd == nil {
		t.Fatal("return to default did not report success")
	}
	active, ok := b.scripts.ActiveLayer()
	if !ok || active.ID != "default" {
		t.Fatalf("active layer = %+v, %v", active, ok)
	}
	if len(b.hiddenColumns) != 0 || len(b.columns) != 3 {
		t.Fatalf("default columns = visible %d, hidden %v", len(b.columns), b.hiddenColumns)
	}
}

func TestLayerSwitcherKeepsVirtualColumnsHiddenAcrossReconciliation(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(`
kbrd.layer{ id="default", name="Default", default=true, setup=function()
  kbrd.column.set("all", { name="All tasks" })
end }
kbrd.layer{ id="focus", name="Focus", setup=function()
  kbrd.column.set("focused", { name="Focused tasks" })
end }`), 0o644); err != nil {
		t.Fatal(err)
	}
	b := newLayerTestBoard(t, dir)

	if err := b.hideAllColumns(events.ColumnKindVirtual); err != nil {
		t.Fatal(err)
	}
	if got := visibleColumnNames(b); !reflect.DeepEqual(got, []string{"todo"}) {
		t.Fatalf("hidden default layer columns = %v", got)
	}
	_, _ = b.handleSwitchLayer(switchLayerMsg{ID: "focus"})
	if len(b.virtualCols) != 1 || b.virtualCols[0].VID != "focused" {
		t.Fatalf("focus virtual registry = %+v", b.virtualCols)
	}
	if got := visibleColumnNames(b); !reflect.DeepEqual(got, []string{"todo"}) {
		t.Fatalf("layer reconciliation revealed virtual column: %v", got)
	}
	if err := b.showAllColumnsByKind(events.ColumnKindVirtual); err != nil {
		t.Fatal(err)
	}
	if got := visibleColumnNames(b); !reflect.DeepEqual(got, []string{"todo", "Focused tasks"}) {
		t.Fatalf("restored focus virtual column = %v", got)
	}
}

func TestLayerSwitcherIsConditional(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := NewBoard(config.Config{Path: dir, ColumnWidth: 20, PreviewLines: 3})
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	_, cmd := b.handleKey(keyPressText("l"))
	if cmd != nil || b.layerSwitcher.Active() {
		t.Fatal("l should be inert without declared layers")
	}
	b.statusPresenter().updateBuiltinCells()
	if cell := b.cells.cells[builtinCellLayer.id()]; cell != nil {
		t.Fatalf("layer cell shown without declarations: %+v", cell)
	}
	for _, group := range b.helpActions().groups() {
		for _, entry := range group.Items {
			if entry.RunKey == "l" || entry.Label == "switch script layer" {
				t.Fatal("help advertised layers without declarations")
			}
		}
	}
}

func TestHelpIncludesLayerSwitcherWhenDeclared(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(`
kbrd.layer{ id="only", default=true, setup=function() end }`), 0o644); err != nil {
		t.Fatal(err)
	}
	b := newLayerTestBoard(t, dir)
	for _, group := range b.helpActions().groups() {
		for _, entry := range group.Items {
			if entry.RunKey == "l" {
				return
			}
		}
	}
	t.Fatal("help did not include layer switcher")
}

func newLayerTestBoard(t *testing.T, dir string) *Board {
	t.Helper()
	cfg := config.Config{Path: dir, ColumnWidth: 20, PreviewLines: 3, NotifyBackend: "none"}
	cfg.Scripting = config.ScriptingConfig{
		Enabled: true, CommandTimeoutMs: 2000, HookTimeoutMs: 500, InstructionLimit: 10_000_000,
	}
	b := NewBoard(cfg)
	b.initRuntime()
	if b.scripts == nil {
		t.Fatal("scripting host not initialized")
	}
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	b.termWidth, b.termHeight, b.visibleHeight = 100, 30, 20
	setColumnHeights(b.columns, b.visibleHeight)
	return b
}
