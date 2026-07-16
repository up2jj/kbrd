package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"kbrd/config"
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
	if got := b.cells.cells[builtinCellLayer.id()].FG; got != string(b.palette.Warning) {
		t.Fatalf("inactive layer color = %q", got)
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
