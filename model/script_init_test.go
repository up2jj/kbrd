package model

import (
	"os"
	"path/filepath"
	"testing"

	"kbrd/config"
)

func TestScriptInitActivityAndCommandMerge(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"),
		[]byte(`kbrd.command("lua-test", "Lua Test", function() end)`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Path: dir, NotifyBackend: "none"}
	cfg.Scripting = config.ScriptingConfig{
		Enabled:          true,
		CommandTimeoutMs: 2000,
		HookTimeoutMs:    500,
	}
	b := NewBoard(cfg)

	if b.scripts != nil {
		t.Fatal("scripts should not be initialized before scriptInitRunMsg")
	}
	if len(b.commands) != 0 {
		t.Fatalf("commands before script init = %d, want 0", len(b.commands))
	}

	_, cmd := b.Update(scriptInitStartMsg{})
	if cmd == nil {
		t.Fatal("scriptInitStartMsg should schedule scriptInitRunMsg")
	}
	assertBuiltinCellText(t, b, builtinCellScriptActivity, "lua loading")

	msg := cmd()
	if _, ok := msg.(scriptInitRunMsg); !ok {
		t.Fatalf("script init start produced %T, want scriptInitRunMsg", msg)
	}
	_, _ = b.Update(msg)

	if b.cells.cells[builtinCellScriptActivity.id()] != nil {
		t.Fatal("script activity cell should clear after init")
	}
	if b.scripts == nil {
		t.Fatal("scripts should be initialized after scriptInitRunMsg")
	}
	if len(b.commands) != 1 || b.commands[0].ID != "lua-test" {
		t.Fatalf("commands after script init = %+v, want lua-test", b.commands)
	}
}

func TestLuaCommandVisibilityUsesSelectedCardFrontmatter(t *testing.T) {
	dir := t.TempDir()
	columnDir := filepath.Join(dir, "todo")
	if err := os.Mkdir(columnDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(columnDir, name+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("note", "---\ntype: note\n---\n# Note\n")
	write("task", "---\ntype: task\n---\n# Task\n")
	if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(`
kbrd.command{
  id = "not-task",
  name = "Not task",
  visible = function(ctx) return not ctx.data or ctx.data.type ~= "task" end,
  run = function() end,
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{Path: dir, NotifyBackend: "none"}
	cfg.Scripting = config.ScriptingConfig{Enabled: true, CommandTimeoutMs: 2000, HookTimeoutMs: 500}
	b := NewBoard(cfg)
	if err := b.initRuntime(); err != nil {
		t.Fatalf("initRuntime: %v", err)
	}
	defer b.closeScripting()
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}
	col := b.columns[0]

	col.SelectByName("note")
	noteCommands := b.commandContext().commandsForColumn(col)
	if got := names(noteCommands); !has(noteCommands, "Not task") {
		t.Fatalf("commands for note = %v, want Not task", got)
	}
	col.SelectByName("task")
	taskCommands := b.commandContext().commandsForColumn(col)
	if got := names(taskCommands); has(taskCommands, "Not task") {
		t.Fatalf("commands for task = %v, want predicate-hidden command", got)
	}
}

func TestSwitchBoardShowsScriptActivityBeforeLoad(t *testing.T) {
	b := NewBoard(config.Config{Path: t.TempDir(), NotifyBackend: "none",
		Scripting: config.ScriptingConfig{Enabled: true}})

	_, cmd := b.Update(switchBoardMsg{Path: t.TempDir()})
	if cmd == nil {
		t.Fatal("switchBoardMsg should schedule switchBoardLoadMsg")
	}
	assertBuiltinCellText(t, b, builtinCellScriptActivity, "lua loading")

	msg := cmd()
	if _, ok := msg.(switchBoardLoadMsg); !ok {
		t.Fatalf("switch board produced %T, want switchBoardLoadMsg", msg)
	}
}
