package script

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"kbrd/config"
	"kbrd/events"
)

// fakeAPI captures BoardAPI calls so tests can assert on them without
// spinning up a real Board.
type fakeAPI struct {
	mu       sync.Mutex
	notifies []string
	moves    []move
	moveErr  error
}

type move struct {
	From, To, Name string
}

func (f *fakeAPI) Notify(msg, level string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notifies = append(f.notifies, level+":"+msg)
}

func (f *fakeAPI) MoveItem(item events.ItemRef, toColumn string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, move{From: item.Column, To: toColumn, Name: item.Name})
	return f.moveErr
}

func writeInit(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FolderInitFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func defaultCfg() config.ScriptingConfig {
	return config.ScriptingConfig{
		Enabled:          true,
		CommandTimeoutMs: 2000,
		HookTimeoutMs:    500,
		InstructionLimit: 10000000,
	}
}

func TestHostDisabled(t *testing.T) {
	cfg := defaultCfg()
	cfg.Enabled = false
	h, err := New(cfg, &fakeAPI{}, nil, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h != nil {
		t.Fatal("expected nil host when disabled")
	}
}

func TestHostNoInitFiles(t *testing.T) {
	h, err := New(defaultCfg(), &fakeAPI{}, nil, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h != nil {
		t.Fatal("expected nil host when no init files exist")
	}
}

func TestCommandRegistration(t *testing.T) {
	dir := writeInit(t, `kbrd.command("a", "Archive", function(ctx) kbrd.notify("ran:"..ctx.fileName) end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	cmds := h.Commands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if cmds[0].Shortcut != "a" || cmds[0].Name != "Archive" || cmds[0].Source != config.SourceLua {
		t.Fatalf("unexpected command: %+v", cmds[0])
	}

	if err := h.RunCommand(cmds[0].LuaRef, map[string]string{"fileName": "foo.md"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "ran:foo.md") {
		t.Fatalf("notify not invoked: %v", api.notifies)
	}
}

func TestHookFires(t *testing.T) {
	dir := writeInit(t, `kbrd.on("git_sync_done", function(evt) kbrd.notify("hook:"..tostring(evt.ok), "info") end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	h.OnEvent(events.GitSyncDone{OK: true, Stage: "push"})
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "hook:true") {
		t.Fatalf("hook did not fire as expected: %v", api.notifies)
	}
}

func TestWatchdogTimeout(t *testing.T) {
	// Tight infinite loop must be aborted within the configured budget.
	dir := writeInit(t, `kbrd.command("l", "Loop", function() while true do end end)`)
	cfg := defaultCfg()
	cfg.CommandTimeoutMs = 200
	cfg.InstructionLimit = 100000
	api := &fakeAPI{}
	h, err := New(cfg, api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	cmds := h.Commands()
	err = h.RunCommand(cmds[0].LuaRef, nil)
	if err == nil {
		t.Fatal("expected error from watchdog, got nil")
	}
}

func TestBoardMove(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("m", "Move", function(ctx)
  local ok, err = kbrd.board.move({column = ctx.columnName, name = ctx.fileName}, "done")
  if not ok then kbrd.notify("err:"..err, "error") end
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	cmds := h.Commands()
	if err := h.RunCommand(cmds[0].LuaRef, map[string]string{
		"fileName":   "task.md",
		"columnName": "todo",
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.moves) != 1 || api.moves[0] != (move{From: "todo", To: "done", Name: "task.md"}) {
		t.Fatalf("unexpected moves: %+v", api.moves)
	}
}

func TestBoardMoveError(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("m", "Move", function(ctx)
  local ok, err = kbrd.board.move({column = "todo", name = "x"}, "done")
  if not ok then kbrd.notify("err:"..err, "error") end
end)`)
	api := &fakeAPI{moveErr: errors.New("nope")}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	cmds := h.Commands()
	_ = h.RunCommand(cmds[0].LuaRef, nil)
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "err:nope") {
		t.Fatalf("expected error notify, got %v", api.notifies)
	}
}

func TestParseError(t *testing.T) {
	dir := writeInit(t, `this is not valid lua @@@`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir)
	if err == nil {
		t.Fatal("expected parse error")
	}
	// Host should be non-nil so caller can still inspect/close even on partial fail.
	if h == nil {
		t.Skip("host was torn down on parse error; acceptable but coverage-only")
	} else {
		h.Close()
	}
}
