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
		InstructionLimit: 10_000_000,
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
	assertCellText(t, b, scriptActivityCellID, "lua loading")

	msg := cmd()
	if _, ok := msg.(scriptInitRunMsg); !ok {
		t.Fatalf("script init start produced %T, want scriptInitRunMsg", msg)
	}
	_, _ = b.Update(msg)

	if b.cells.cells[scriptActivityCellID] != nil {
		t.Fatal("script activity cell should clear after init")
	}
	if b.scripts == nil {
		t.Fatal("scripts should be initialized after scriptInitRunMsg")
	}
	if len(b.commands) != 1 || b.commands[0].ID != "lua-test" {
		t.Fatalf("commands after script init = %+v, want lua-test", b.commands)
	}
}

func TestSwitchBoardShowsScriptActivityBeforeLoad(t *testing.T) {
	b := NewBoard(config.Config{Path: t.TempDir(), NotifyBackend: "none",
		Scripting: config.ScriptingConfig{Enabled: true}})

	_, cmd := b.Update(switchBoardMsg{Path: t.TempDir()})
	if cmd == nil {
		t.Fatal("switchBoardMsg should schedule switchBoardLoadMsg")
	}
	assertCellText(t, b, scriptActivityCellID, "lua loading")

	msg := cmd()
	if _, ok := msg.(switchBoardLoadMsg); !ok {
		t.Fatalf("switch board produced %T, want switchBoardLoadMsg", msg)
	}
}
