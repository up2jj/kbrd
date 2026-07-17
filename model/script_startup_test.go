package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"kbrd/config"
)

func startupTestBoard(t *testing.T, body string) *Board {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Path: dir, NotifyBackend: "none"}
	cfg.Scripting = config.ScriptingConfig{Enabled: true, CommandTimeoutMs: 1000, HookTimeoutMs: 1000, InstructionLimit: 1_000_000}
	b := NewBoard(cfg)
	b.termWidth, b.termHeight = 100, 30
	return b
}

func TestFolderScriptFailureBlocksStartupAndRendersDebugScreen(t *testing.T) {
	b := startupTestBoard(t, "print('before failure')\nerror('boom')\n")
	_, cmd := b.Update(scriptInitRunMsg{})
	if cmd != nil {
		t.Fatal("failed preflight should not start board loading")
	}
	if !b.scriptStartup.active {
		t.Fatal("startup debug screen is not active")
	}
	view := ansi.Strip(b.View().Content)
	for _, want := range []string{".kbrd.lua startup failed", ".kbrd.lua:2", "boom", "before failure", "e edit in $EDITOR", "r retry"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if len(b.columns) != 0 || b.watcher != nil {
		t.Fatal("board data started loading despite script failure")
	}
	_, _ = b.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !b.scriptStartup.expanded || !strings.Contains(ansi.Strip(b.View().Content), "Traceback") {
		t.Fatal("enter did not expand the traceback")
	}
}

func TestRetryKeepsSuccessfulHostAndContinuesStartup(t *testing.T) {
	b := startupTestBoard(t, "error('broken')")
	_, _ = b.Update(scriptInitRunMsg{})
	if err := os.WriteFile(filepath.Join(b.cfg.Path, ".kbrd.lua"), []byte(`kbrd.command("ok", "OK", function() end)`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, cmd := b.Update(scriptInitRunMsg{})
	if b.scriptStartup.active || b.scripts == nil || cmd == nil {
		t.Fatalf("retry state: active=%v host=%v cmd=%v", b.scriptStartup.active, b.scripts, cmd)
	}
	if len(b.commands) != 1 || b.commands[0].ID != "ok" {
		t.Fatalf("commands = %+v", b.commands)
	}
}

func TestScriptEditorCommandUsesShellEditorFallback(t *testing.T) {
	cmd := scriptEditorCommand("/board", "/board/.kbrd.lua")
	if cmd.Dir != "/board" || len(cmd.Args) < 5 || cmd.Args[len(cmd.Args)-1] != "/board/.kbrd.lua" {
		t.Fatalf("editor command = dir %q args %#v", cmd.Dir, cmd.Args)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "VISUAL") || !strings.Contains(strings.Join(cmd.Args, " "), "EDITOR") {
		t.Fatalf("editor fallback missing: %#v", cmd.Args)
	}
}

func TestGlobalInitFailureDoesNotBlockFolderScript(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	globalDir := filepath.Join(userConfigDir, config.AppDirName)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "init.lua"), []byte(`error("global boom")`), 0o644); err != nil {
		t.Fatal(err)
	}
	b := startupTestBoard(t, `kbrd.command("ok", "OK", function() end)`)
	_, cmd := b.Update(scriptInitRunMsg{})
	if b.scriptStartup.active || cmd == nil {
		t.Fatalf("global failure blocked startup: active=%v cmd=%v", b.scriptStartup.active, cmd)
	}
	if len(b.commandWarnings) == 0 || !strings.Contains(b.commandWarnings[0].Message, "global boom") {
		t.Fatalf("warnings = %+v", b.commandWarnings)
	}
}

func TestBoardSwitchUsesScriptPreflightGate(t *testing.T) {
	b := NewBoard(config.Config{Path: t.TempDir(), NotifyBackend: "none", Scripting: config.ScriptingConfig{Enabled: true}})
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, ".kbrd.lua"), []byte(`error("switch boom")`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd, err := b.session().loadBoard(target)
	if err != nil || cmd != nil {
		t.Fatalf("loadBoard = %v, %v", cmd, err)
	}
	if !b.scriptStartup.active || !b.scriptStartup.switching || len(b.columns) != 0 {
		t.Fatalf("switch gate = %+v, columns=%d", b.scriptStartup, len(b.columns))
	}
}

func TestStartupPreflightBypassesMissingScriptAndSafeMode(t *testing.T) {
	for _, tt := range []struct {
		name string
		safe bool
		body string
	}{
		{name: "missing script"},
		{name: "safe mode", safe: true, body: `error("must not run")`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.body != "" {
				if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(tt.body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cfg := config.Config{Path: dir, NotifyBackend: "none", Scripting: config.ScriptingConfig{Enabled: true}}
			b := NewBoardWithOptions(cfg, BoardOptions{Safe: tt.safe})
			_, cmd := b.Update(scriptInitRunMsg{})
			if b.scriptStartup.active || cmd == nil {
				t.Fatalf("startup bypass: active=%v cmd=%v", b.scriptStartup.active, cmd)
			}
		})
	}
}
