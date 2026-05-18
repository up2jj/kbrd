package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/config"
)

// makeBoard builds a Board rooted at a tempdir, with one column and one item
// and the given Lua init file content.
func makeBoard(t *testing.T, luaInit string) (*Board, string) {
	t.Helper()
	dir := t.TempDir()
	col := filepath.Join(dir, "todo")
	if err := os.MkdirAll(col, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(col, "item.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if luaInit != "" {
		if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(luaInit), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Config{
		Path:          dir,
		ColumnWidth:   20,
		PreviewLines:  1,
		Theme:         "dark",
		NotifyBackend: "none",
		Scripting: config.ScriptingConfig{
			Enabled:          true,
			CommandTimeoutMs: 2000,
			HookTimeoutMs:    500,
			InstructionLimit: 10000000,
		},
	}
	b := NewBoard(cfg)
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	return b, dir
}

// runMsg pumps a tea.Msg through Board.Update and recursively executes any
// returned tea.Cmd, so we end up with the steady-state model+side effects.
func runMsg(t *testing.T, b *Board, msg tea.Msg) {
	t.Helper()
	for i := 0; i < 50; i++ {
		_, cmd := b.Update(msg)
		if cmd == nil {
			return
		}
		msg = cmd()
		if msg == nil {
			return
		}
	}
	t.Fatal("cmd loop did not converge")
}

func TestScriptCommandNoUI(t *testing.T) {
	b, _ := makeBoard(t, `kbrd.command("a", "Archive", function(ctx) end)`)
	if len(b.commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(b.commands))
	}
	cmd := b.commands[0]
	runMsg(t, b, runCustomCommandMsg{Cmd: cmd, Vars: map[string]string{"fileName": "item"}})
	if b.scriptUI.Active() {
		t.Fatal("scriptUI should not be active after a no-UI command")
	}
}

func TestScriptCommandPick(t *testing.T) {
	b, _ := makeBoard(t, `
kbrd.command("p", "Pick", function()
  local c = kbrd.ui.pick("Title", {"a", "b"})
  if c == nil then return end
end)`)
	cmd := b.commands[0]
	// Dispatch the run msg through the recursive runner — but pick yields,
	// so the loop terminates with scriptUI active.
	_, c := b.Update(runCustomCommandMsg{Cmd: cmd, Vars: nil})
	if c != nil {
		// Pick should not produce a follow-up cmd — it just opens the UI.
		if msg := c(); msg != nil {
			t.Fatalf("expected no follow-up msg after opening picker, got %T %+v", msg, msg)
		}
	}
	if !b.scriptUI.Active() {
		t.Fatal("scriptUI should be active after pick yield")
	}
	if b.scriptUI.kind != scriptUIPick {
		t.Fatalf("expected pick kind, got %v", b.scriptUI.kind)
	}
	if len(b.scriptUI.choices) != 2 {
		t.Fatalf("expected 2 choices, got %v", b.scriptUI.choices)
	}
}

func TestScriptUIPickEnter(t *testing.T) {
	b, _ := makeBoard(t, `
kbrd.command("p", "Pick", function()
  local c = kbrd.ui.pick("Title", {"a", "b"})
  if c ~= nil then kbrd.notify("got:"..c, "success") end
end)`)
	cmd := b.commands[0]
	b.Update(runCustomCommandMsg{Cmd: cmd, Vars: nil})
	if !b.scriptUI.Active() {
		t.Fatal("scriptUI not active")
	}
	// Press enter — should pick the first choice and complete the coroutine.
	_, c := b.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if c == nil {
		t.Fatal("expected resume cmd from picker enter")
	}
	msg := c()
	rm, ok := msg.(scriptResumeMsg)
	if !ok {
		t.Fatalf("expected scriptResumeMsg, got %T %+v", msg, msg)
	}
	if rm.Result != "a" {
		t.Fatalf("expected result 'a', got %v", rm.Result)
	}
	// Drive resume through Update.
	_, c2 := b.Update(rm)
	if c2 == nil {
		t.Fatal("expected follow-up cmd after resume")
	}
	follow := c2()
	if _, ok := follow.(customCommandFinishedMsg); !ok {
		t.Fatalf("expected customCommandFinishedMsg, got %T %+v", follow, follow)
	}
	if b.scriptUI.Active() {
		t.Fatal("scriptUI should be closed after pick")
	}
}

func TestScriptUIPickArrowsAndEsc(t *testing.T) {
	b, _ := makeBoard(t, `
kbrd.command("p", "Pick", function()
  local c = kbrd.ui.pick("T", {"a", "b", "c"})
  if c == nil then kbrd.notify("cancel", "info") else kbrd.notify("got:"..c, "info") end
end)`)
	cmd := b.commands[0]
	b.Update(runCustomCommandMsg{Cmd: cmd, Vars: nil})
	if !b.scriptUI.Active() {
		t.Fatal("scriptUI not active")
	}
	// Press down arrow — should move selection from 0 to 1.
	b.Update(tea.KeyMsg{Type: tea.KeyDown})
	if b.scriptUI.selected != 1 {
		t.Fatalf("expected selected=1 after down, got %d", b.scriptUI.selected)
	}
	// Press down again.
	b.Update(tea.KeyMsg{Type: tea.KeyDown})
	if b.scriptUI.selected != 2 {
		t.Fatalf("expected selected=2, got %d", b.scriptUI.selected)
	}
	// Press up.
	b.Update(tea.KeyMsg{Type: tea.KeyUp})
	if b.scriptUI.selected != 1 {
		t.Fatalf("expected selected=1 after up, got %d", b.scriptUI.selected)
	}
	// Press esc — should cancel.
	_, c := b.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if c == nil {
		t.Fatal("esc should produce a resume cmd")
	}
	msg := c()
	rm, ok := msg.(scriptResumeMsg)
	if !ok {
		t.Fatalf("expected scriptResumeMsg, got %T", msg)
	}
	if rm.Result != nil {
		t.Fatalf("expected nil result on cancel, got %v", rm.Result)
	}
}

// Regression: a hook that schedules a timer (e.g. on item_created) must
// have its pending timer drained by Update, not left dangling in the
// host's pendingTimers queue.
func TestHookTimerScheduled(t *testing.T) {
	b, _ := makeBoard(t, `
kbrd.on("item_created", function(evt)
  kbrd.timer.after(150, function() end)
end)`)
	// Drain any timers scheduled by init.lua so we measure only the hook's effect.
	_ = b.collectTimerCmds()
	_, cmd := b.Update(editorNewMsg{ColIndex: 0, FileName: "fresh"})
	if cmd == nil {
		t.Fatal("expected a tea.Cmd from create (notify + timer)")
	}
	// After Update completes, the host's pendingTimers queue must be empty —
	// the timer should have been converted into a tea.Tick already.
	if rest := b.scripts.PendingTimers(); len(rest) != 0 {
		t.Fatalf("hook-scheduled timer not drained by Update: %v", rest)
	}
}

// End-to-end async: a Lua command schedules a shell job; pump the resulting
// tea.Cmd to execute it; the dispatched scriptAsyncDoneMsg invokes the Lua
// callback. The callback writes to a sentinel file so we can assert it
// actually fired (more reliable than peeking at the toast notifier).
func TestScriptAsyncEndToEnd(t *testing.T) {
	b, dir := makeBoard(t, `
kbrd.command("a", "Async", function()
  kbrd.async.run("printf hello", function(r)
    kbrd.fs.write("CALLED", "out=" .. r.out .. " exit=" .. r.exitCode)
  end)
end)`)
	cmd := b.commands[0]
	_, c := b.Update(runCustomCommandMsg{Cmd: cmd, Vars: nil})
	if c == nil {
		t.Fatal("expected a tea.Cmd carrying the async exec")
	}
	// Pump cmds until we get the async-done back, then dispatch it.
	var asyncMsg scriptAsyncDoneMsg
	got := false
	for i := 0; i < 5 && !got; i++ {
		msg := c()
		switch m := msg.(type) {
		case scriptAsyncDoneMsg:
			asyncMsg = m
			got = true
		case tea.BatchMsg:
			for _, sub := range m {
				inner := sub()
				if am, ok := inner.(scriptAsyncDoneMsg); ok {
					asyncMsg = am
					got = true
					break
				}
			}
		}
	}
	if !got {
		t.Fatal("did not receive scriptAsyncDoneMsg")
	}
	if asyncMsg.Out != "hello" {
		t.Fatalf("unexpected output: %q", asyncMsg.Out)
	}
	b.Update(asyncMsg)
	body, err := os.ReadFile(filepath.Join(dir, "CALLED"))
	if err != nil {
		t.Fatalf("callback did not run — sentinel file missing: %v", err)
	}
	if !strings.Contains(string(body), "out=hello") {
		t.Fatalf("callback ran but with wrong args: %q", body)
	}
}

func TestScriptUIRendersView(t *testing.T) {
	b, _ := makeBoard(t, `kbrd.command("p", "Pick", function() kbrd.ui.pick("Hello", {"a", "b"}) end)`)
	b.termWidth = 80
	b.termHeight = 24
	cmd := b.commands[0]
	b.Update(runCustomCommandMsg{Cmd: cmd, Vars: nil})
	view := b.View()
	if !strings.Contains(view, "Hello") {
		t.Fatalf("view should contain picker title 'Hello'; got: %s", view)
	}
	if !strings.Contains(view, "a") || !strings.Contains(view, "b") {
		t.Fatalf("view should contain choices; got: %s", view)
	}
}
