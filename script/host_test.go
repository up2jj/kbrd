package script

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"kbrd/config"
	"kbrd/events"
)

// fakeAPI captures BoardAPI calls so tests can assert on them without
// spinning up a real Board. FS ops are routed to a real tempdir so they
// exercise the same os/filepath plumbing the production implementation does.
type fakeAPI struct {
	mu        sync.Mutex
	root      string
	notifies  []string
	moves     []move
	moveErr   error
	refreshes int
	columns   []string
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

func (f *fakeAPI) resolve(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(f.root, p)
}

func (f *fakeAPI) FSRead(p string) (string, error) {
	b, err := os.ReadFile(f.resolve(p))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (f *fakeAPI) FSWrite(p, body string) error {
	return os.WriteFile(f.resolve(p), []byte(body), 0o644)
}

func (f *fakeAPI) FSExists(p string) bool {
	_, err := os.Stat(f.resolve(p))
	return err == nil
}

func (f *fakeAPI) FSMkdir(p string) error {
	return os.MkdirAll(f.resolve(p), 0o755)
}

func (f *fakeAPI) FSGlob(pattern string) ([]string, error) {
	return filepath.Glob(f.resolve(pattern))
}

func (f *fakeAPI) Refresh() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes++
	return nil
}

func (f *fakeAPI) CreateColumn(name string) error {
	if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return errors.New("invalid column name")
	}
	dir := filepath.Join(f.root, name)
	if _, err := os.Stat(dir); err == nil {
		return errors.New("already exists")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f.mu.Lock()
	f.columns = append(f.columns, name)
	f.mu.Unlock()
	return f.Refresh()
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

	if _, err := h.RunCommand(cmds[0].LuaRef, map[string]string{"fileName": "foo.md"}); err != nil {
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
	_, err = h.RunCommand(cmds[0].LuaRef, nil)
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
	if _, err := h.RunCommand(cmds[0].LuaRef, map[string]string{
		"fileName":   "task.md",
		"columnName": "todo",
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.moves) != 1 || api.moves[0] != (move{From: "todo", To: "done", Name: "task.md"}) {
		t.Fatalf("unexpected moves: %+v", api.moves)
	}
}

// Regression: boardScriptAPI.MoveItem (in the real model) publishes an
// ItemMoved event synchronously, which routes back to Host.OnEvent. If
// running scripts re-entered the VM, this would deadlock or corrupt the
// coroutine. We simulate that with a fakeAPI that calls bus.Publish from
// inside MoveItem and a Lua script that hooks item_moved.
func TestNoDeadlockOnInScriptEvent(t *testing.T) {
	dir := writeInit(t, `
local moves = 0
kbrd.on("item_moved", function(evt)
  moves = moves + 1
  kbrd.notify("hooked:"..evt.to, "info")
end)
kbrd.command("m", "Move", function(ctx)
  kbrd.board.move({column = "todo", name = "x"}, "done")
  kbrd.notify("after:"..tostring(moves), "info")
end)
`)
	api := &fakeAPIWithBus{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	api.host = h // back-reference so MoveItem can publish events

	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	// Two notifies: "after:0" (hook deferred until script returns), then
	// "hooked:done" (drained after the script completed).
	if len(api.notifies) != 2 {
		t.Fatalf("expected 2 notifies, got %v", api.notifies)
	}
	if !strings.Contains(api.notifies[0], "after:0") {
		t.Fatalf("first notify should be after:0 (hook deferred); got %s", api.notifies[0])
	}
	if !strings.Contains(api.notifies[1], "hooked:done") {
		t.Fatalf("second notify should be the drained hook; got %s", api.notifies[1])
	}
}

// fakeAPIWithBus is a fakeAPI whose MoveItem publishes an ItemMoved event
// synchronously through the Host's OnEvent — mirroring how the real
// boardScriptAPI.MoveItem behaves.
type fakeAPIWithBus struct {
	fakeAPI
	host *Host
}

func (f *fakeAPIWithBus) MoveItem(item events.ItemRef, toColumn string) error {
	_ = f.fakeAPI.MoveItem(item, toColumn)
	if f.host != nil {
		f.host.OnEvent(events.ItemMoved{Item: item, From: item.Column, To: toColumn})
	}
	return nil
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
	_, _ = h.RunCommand(cmds[0].LuaRef, nil)
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "err:nope") {
		t.Fatalf("expected error notify, got %v", api.notifies)
	}
}

func TestFSRoundTrip(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("w", "Write", function()
  local ok, err = kbrd.fs.write("note.md", "hello")
  if not ok then kbrd.notify("write err:"..err, "error"); return end
  if not kbrd.fs.exists("note.md") then kbrd.notify("missing", "error"); return end
  local body = kbrd.fs.read("note.md")
  kbrd.notify("got:"..body, "info")
end)`)
	api := &fakeAPI{root: dir}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "got:hello") {
		t.Fatalf("unexpected notifies: %v", api.notifies)
	}
}

func TestFSMkdirAndGlob(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("g", "Glob", function()
  kbrd.fs.mkdir("nested/sub")
  kbrd.fs.write("nested/a.md", "")
  kbrd.fs.write("nested/b.md", "")
  local hits = kbrd.fs.glob("nested/*.md")
  kbrd.notify("count:"..#hits, "info")
end)`)
	api := &fakeAPI{root: dir}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "count:2") {
		t.Fatalf("unexpected notifies: %v", api.notifies)
	}
	// directory really exists
	if _, err := os.Stat(filepath.Join(dir, "nested", "sub")); err != nil {
		t.Fatal("nested/sub not created")
	}
}

func TestBoardCreateColumn(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("a", "Archive", function()
  if not kbrd.fs.exists("archive") then
    local ok, err = kbrd.board.createColumn("archive")
    if not ok then kbrd.notify("err:"..err, "error"); return end
  end
  kbrd.notify("ok", "success")
end)`)
	api := &fakeAPI{root: dir}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.columns) != 1 || api.columns[0] != "archive" {
		t.Fatalf("createColumn not invoked: %v", api.columns)
	}
	if api.refreshes < 1 {
		t.Fatalf("refresh not called, got %d", api.refreshes)
	}
	// Second invocation should not re-create.
	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run2: %v", err)
	}
	if len(api.columns) != 1 {
		t.Fatalf("expected single create, got %v", api.columns)
	}
}

func TestBoardCreateColumnBadName(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("a", "Bad", function()
  local ok, err = kbrd.board.createColumn("bad/name")
  if not ok then kbrd.notify("err:"..err, "error"); return end
  kbrd.notify("ok", "success")
end)`)
	api := &fakeAPI{root: dir}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.notifies) != 1 || !strings.HasPrefix(api.notifies[0], "error:err:") {
		t.Fatalf("expected error notify, got %v", api.notifies)
	}
}

func TestBoardRefresh(t *testing.T) {
	dir := writeInit(t, `kbrd.command("r", "Refresh", function() kbrd.board.refresh() end)`)
	api := &fakeAPI{root: dir}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if api.refreshes != 1 {
		t.Fatalf("expected 1 refresh, got %d", api.refreshes)
	}
}

func TestFSAbsolutePath(t *testing.T) {
	// Absolute paths should not be re-rooted under boardPath — they go through as-is.
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "x.txt"), []byte("abs"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := writeInit(t, `
kbrd.command("r", "Read", function(ctx)
  local body = kbrd.fs.read(ctx.absPath)
  kbrd.notify("body:"..body, "info")
end)`)
	api := &fakeAPI{root: dir}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if _, err := h.RunCommand(h.Commands()[0].LuaRef, map[string]string{
		"absPath": filepath.Join(other, "x.txt"),
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "body:abs") {
		t.Fatalf("unexpected: %v", api.notifies)
	}
}

func TestUIPick(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("p", "Pick", function()
  local choice = kbrd.ui.pick("Priority", {"P0", "P1", "P2"})
  if choice == nil then kbrd.notify("cancelled", "info"); return end
  kbrd.notify("chose:"..choice, "success")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	req, err := h.RunCommand(h.Commands()[0].LuaRef, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if req == nil {
		t.Fatal("expected UI request, got nil")
	}
	if req.Kind != "pick" || req.Title != "Priority" || len(req.Choices) != 3 {
		t.Fatalf("unexpected req: %+v", req)
	}

	req2, err := h.ResumeWith(req.Token, "P1")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if req2 != nil {
		t.Fatalf("expected completion, got another req: %+v", req2)
	}
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "chose:P1") {
		t.Fatalf("unexpected notifies: %v", api.notifies)
	}
}

func TestUIPickCancel(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("p", "Pick", function()
  local choice = kbrd.ui.pick("Pick", {"a", "b"})
  if choice == nil then kbrd.notify("cancelled", "info"); return end
  kbrd.notify("chose:"..choice, "success")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	req, err := h.RunCommand(h.Commands()[0].LuaRef, nil)
	if err != nil || req == nil {
		t.Fatalf("expected req, got req=%v err=%v", req, err)
	}
	if _, err := h.ResumeWith(req.Token, nil); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "cancelled") {
		t.Fatalf("expected cancel branch, got %v", api.notifies)
	}
}

func TestUIPrompt(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("r", "Rename", function()
  local name = kbrd.ui.prompt("New name", "default")
  kbrd.notify("got:"..tostring(name), "info")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	req, err := h.RunCommand(h.Commands()[0].LuaRef, nil)
	if err != nil || req == nil {
		t.Fatalf("expected req, got req=%v err=%v", req, err)
	}
	if req.Kind != "prompt" || req.Default != "default" {
		t.Fatalf("unexpected req: %+v", req)
	}
	if _, err := h.ResumeWith(req.Token, "hello"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !strings.Contains(api.notifies[0], "got:hello") {
		t.Fatalf("unexpected: %v", api.notifies)
	}
}

func TestUIConfirm(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("c", "Confirm", function()
  local ok = kbrd.ui.confirm("Sure?")
  kbrd.notify("answered:"..tostring(ok), "info")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	req, err := h.RunCommand(h.Commands()[0].LuaRef, nil)
	if err != nil || req == nil {
		t.Fatalf("expected req, got req=%v err=%v", req, err)
	}
	if req.Kind != "confirm" {
		t.Fatalf("expected confirm, got %s", req.Kind)
	}
	if _, err := h.ResumeWith(req.Token, true); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !strings.Contains(api.notifies[0], "answered:true") {
		t.Fatalf("unexpected: %v", api.notifies)
	}
}

func TestUIChained(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("c", "Chain", function()
  local a = kbrd.ui.pick("First", {"x", "y"})
  local b = kbrd.ui.prompt("Second", "")
  kbrd.notify("got:"..a..","..b, "info")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	req, err := h.RunCommand(h.Commands()[0].LuaRef, nil)
	if err != nil || req == nil || req.Kind != "pick" {
		t.Fatalf("expected pick, got req=%+v err=%v", req, err)
	}
	req2, err := h.ResumeWith(req.Token, "x")
	if err != nil || req2 == nil || req2.Kind != "prompt" {
		t.Fatalf("expected prompt, got req=%+v err=%v", req2, err)
	}
	if _, err := h.ResumeWith(req2.Token, "world"); err != nil {
		t.Fatalf("final resume: %v", err)
	}
	if !strings.Contains(api.notifies[0], "got:x,world") {
		t.Fatalf("unexpected: %v", api.notifies)
	}
}

func TestTimerSchedule(t *testing.T) {
	dir := writeInit(t, `
local handle = kbrd.timer.every(150, function() kbrd.notify("tick", "info") end)
kbrd.notify("handle:"..handle, "info")
`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	pending := h.PendingTimers()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending timer, got %d", len(pending))
	}
	if pending[0].Duration != 150*time.Millisecond {
		t.Fatalf("unexpected duration: %v", pending[0].Duration)
	}
	// Notify should already record the handle.
	if len(api.notifies) != 1 || !strings.HasPrefix(api.notifies[0], "info:handle:co-") {
		t.Fatalf("expected handle notify, got %v", api.notifies)
	}
}

func TestTimerMinClamp(t *testing.T) {
	dir := writeInit(t, `kbrd.timer.after(5, function() end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	pending := h.PendingTimers()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending timer")
	}
	if pending[0].Duration != 100*time.Millisecond {
		t.Fatalf("expected duration clamped to 100ms, got %v", pending[0].Duration)
	}
}

func TestTimerFireOnce(t *testing.T) {
	dir := writeInit(t, `kbrd.timer.after(100, function() kbrd.notify("fired", "info") end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	pending := h.PendingTimers()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending")
	}
	if err := h.FireTimer(pending[0].Token); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "fired") {
		t.Fatalf("unexpected: %v", api.notifies)
	}
	// One-shot — should not reschedule.
	if rest := h.PendingTimers(); len(rest) != 0 {
		t.Fatalf("one-shot timer should not reschedule, got %d", len(rest))
	}
}

func TestTimerFireEveryReschedules(t *testing.T) {
	dir := writeInit(t, `
local n = 0
kbrd.timer.every(120, function()
  n = n + 1
  kbrd.notify("n:"..n, "info")
end)
`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	pending := h.PendingTimers()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending")
	}
	token := pending[0].Token
	_ = h.FireTimer(token)
	_ = h.FireTimer(token)
	// Each fire re-arms — drain again.
	again := h.PendingTimers()
	if len(again) != 2 {
		t.Fatalf("every-timer should re-arm; got %d", len(again))
	}
	if len(api.notifies) != 2 {
		t.Fatalf("expected 2 notifies, got %v", api.notifies)
	}
}

func TestTimerNestedScheduleRejected(t *testing.T) {
	dir := writeInit(t, `
kbrd.timer.after(100, function()
  local ok, err = pcall(function() kbrd.timer.after(100, function() end) end)
  kbrd.notify("nested:"..tostring(ok), "info")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	pending := h.PendingTimers()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if err := h.FireTimer(pending[0].Token); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "nested:false") {
		t.Fatalf("nested schedule should have been rejected: %v", api.notifies)
	}
	// No new timer should have been added.
	if rest := h.PendingTimers(); len(rest) != 0 {
		t.Fatalf("rejected timer should not be queued, got %v", rest)
	}
}

func TestTimerNestedViaHookRejected(t *testing.T) {
	// Timer body publishes ItemMoved; hook on item_moved tries to schedule.
	// inTimer flag must still be set during the deferred drain.
	dir := writeInit(t, `
kbrd.on("item_moved", function()
  local ok = pcall(function() kbrd.timer.after(100, function() end) end)
  kbrd.notify("hook-nested:"..tostring(ok), "info")
end)
kbrd.timer.after(100, function()
  kbrd.board.move({column = "todo", name = "x"}, "done")
end)`)
	api := &fakeAPIWithBus{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	api.host = h
	pending := h.PendingTimers()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	_ = h.FireTimer(pending[0].Token)
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "hook-nested:false") {
		t.Fatalf("hook scheduled from timer side-effect should be rejected: %v", api.notifies)
	}
}

func TestTimerRepeatStillWorksDespiteNestedBlock(t *testing.T) {
	// The host re-arms repeating timers internally (not via Lua), so the
	// inTimer block must not break that.
	dir := writeInit(t, `kbrd.timer.every(100, function() kbrd.notify("tick", "info") end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	token := h.PendingTimers()[0].Token
	_ = h.FireTimer(token)
	_ = h.FireTimer(token)
	if len(api.notifies) != 2 {
		t.Fatalf("expected 2 ticks, got %v", api.notifies)
	}
	if rest := h.PendingTimers(); len(rest) != 2 {
		t.Fatalf("repeats should re-arm; got %d", len(rest))
	}
}

func TestTimerAutoDisableAfterErrors(t *testing.T) {
	dir := writeInit(t, `
kbrd.timer.every(100, function() error("boom") end)
`)
	cfg := defaultCfg()
	cfg.ErrorThreshold = 3
	api := &fakeAPI{}
	h, err := New(cfg, api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	token := h.PendingTimers()[0].Token
	// Fire three times — each errors, third time we hit the threshold.
	_ = h.FireTimer(token)
	_ = h.FireTimer(token)
	_ = h.FireTimer(token)
	if _, still := h.timers[token]; still {
		t.Fatal("timer should be disabled after 3 errors")
	}
	// Notifies: 3 error toasts + 1 "disabled after 3 errors".
	if len(api.notifies) != 4 {
		t.Fatalf("expected 4 notifies, got %v", api.notifies)
	}
	if !strings.Contains(api.notifies[3], "disabled after 3 errors") {
		t.Fatalf("final notify should be the disable message; got %v", api.notifies)
	}
	// Further fires are no-ops.
	_ = h.FireTimer(token)
	if len(api.notifies) != 4 {
		t.Fatalf("disabled timer should not fire again, got %v", api.notifies)
	}
}

func TestTimerThresholdZeroNeverDisables(t *testing.T) {
	dir := writeInit(t, `kbrd.timer.every(100, function() error("boom") end)`)
	cfg := defaultCfg()
	cfg.ErrorThreshold = 0 // "never auto-disable"
	api := &fakeAPI{}
	h, err := New(cfg, api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	token := h.PendingTimers()[0].Token
	for i := 0; i < 10; i++ {
		_ = h.FireTimer(token)
	}
	if _, still := h.timers[token]; !still {
		t.Fatal("timer should still be registered with threshold=0")
	}
	if len(api.notifies) != 10 {
		t.Fatalf("expected 10 error notifies, got %d", len(api.notifies))
	}
}

func TestTimerErrorResetsOnSuccess(t *testing.T) {
	// First two calls fail, third succeeds — counter should reset, so the
	// timer isn't disabled even though we hit 2 errors out of 3.
	dir := writeInit(t, `
local n = 0
kbrd.timer.every(100, function()
  n = n + 1
  if n < 3 then error("flaky") end
  kbrd.notify("ok", "info")
end)`)
	cfg := defaultCfg()
	cfg.ErrorThreshold = 3
	api := &fakeAPI{}
	h, err := New(cfg, api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	token := h.PendingTimers()[0].Token
	_ = h.FireTimer(token) // err 1
	_ = h.FireTimer(token) // err 2
	_ = h.FireTimer(token) // success — counter resets
	_ = h.FireTimer(token) // err 1 again, not 3
	if _, still := h.timers[token]; !still {
		t.Fatal("counter should have reset on success; timer should still be live")
	}
}

func TestHookAutoDisableAfterErrors(t *testing.T) {
	dir := writeInit(t, `kbrd.on("git_sync_done", function() error("boom") end)`)
	cfg := defaultCfg()
	cfg.ErrorThreshold = 2
	api := &fakeAPI{}
	h, err := New(cfg, api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	h.OnEvent(events.GitSyncDone{OK: true})
	h.OnEvent(events.GitSyncDone{OK: true})
	if len(h.hooks["git_sync_done"]) != 0 {
		t.Fatalf("hook should be disabled after 2 errors")
	}
	// Notifies: 2 error toasts + 1 disabled.
	if len(api.notifies) != 3 {
		t.Fatalf("expected 3 notifies, got %v", api.notifies)
	}
	if !strings.Contains(api.notifies[2], "disabled after 2 errors") {
		t.Fatalf("final notify should announce disable; got %v", api.notifies)
	}
	// Further events for this hook are silent — the hook is gone.
	h.OnEvent(events.GitSyncDone{OK: true})
	if len(api.notifies) != 3 {
		t.Fatalf("disabled hook should not fire; got %v", api.notifies)
	}
}

func TestTimerCannotOpenUI(t *testing.T) {
	dir := writeInit(t, `
kbrd.timer.after(100, function()
  local ok, err = pcall(function() kbrd.ui.pick("X", {"a"}) end)
  kbrd.notify("ui:"..tostring(ok)..":"..tostring(err), "info")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	_ = h.FireTimer(h.PendingTimers()[0].Token)
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "ui:false") {
		t.Fatalf("ui from timer should be rejected: %v", api.notifies)
	}
	if !strings.Contains(api.notifies[0], "cannot be used from a timer") {
		t.Fatalf("error message should mention timer; got %v", api.notifies)
	}
}

func TestOsExitDisabled(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("e", "Exit", function()
  local ok, err = pcall(os.exit, 1)
  kbrd.notify("exit:"..tostring(ok)..":"..tostring(err), "info")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "exit:false") {
		t.Fatalf("os.exit should be disabled: %v", api.notifies)
	}
	if !strings.Contains(api.notifies[0], "disabled in kbrd scripts") {
		t.Fatalf("error message should mention disabled; got %v", api.notifies)
	}
}

func TestTimerCannotRegisterCommand(t *testing.T) {
	dir := writeInit(t, `
kbrd.timer.after(100, function()
  local ok = pcall(function() kbrd.command("x", "X", function() end) end)
  kbrd.notify("cmd:"..tostring(ok), "info")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	_ = h.FireTimer(h.PendingTimers()[0].Token)
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "cmd:false") {
		t.Fatalf("command registration from timer should be rejected: %v", api.notifies)
	}
	if len(h.Commands()) != 0 {
		t.Fatal("no command should have been registered")
	}
}

func TestTimerCannotRegisterHook(t *testing.T) {
	dir := writeInit(t, `
kbrd.timer.after(100, function()
  local ok = pcall(function() kbrd.on("item_moved", function() end) end)
  kbrd.notify("hook:"..tostring(ok), "info")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	_ = h.FireTimer(h.PendingTimers()[0].Token)
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "hook:false") {
		t.Fatalf("hook registration from timer should be rejected: %v", api.notifies)
	}
}

func TestTimerCancel(t *testing.T) {
	dir := writeInit(t, `
local h = kbrd.timer.every(100, function() kbrd.notify("tick", "info") end)
kbrd.timer.cancel(h)
`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	pending := h.PendingTimers()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending (cancellation is lazy)")
	}
	// Firing a cancelled timer is a no-op.
	if err := h.FireTimer(pending[0].Token); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if len(api.notifies) != 0 {
		t.Fatalf("cancelled timer should not invoke callback, got %v", api.notifies)
	}
}

func TestItemSelectHook(t *testing.T) {
	dir := writeInit(t, `
kbrd.on("item_select", function(evt)
  kbrd.notify("sel:"..evt.item.name.." prev:"..evt.prev.name, "info")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	h.OnEvent(events.ItemSelect{
		Item: events.ItemRef{Column: "todo", Name: "a"},
		Prev: events.ItemRef{Column: "todo", Name: "b"},
	})
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "sel:a prev:b") {
		t.Fatalf("unexpected: %v", api.notifies)
	}
}

func TestItemCreatedHook(t *testing.T) {
	dir := writeInit(t, `
kbrd.on("item_created", function(evt) kbrd.notify("created:"..evt.item.name, "info") end)
kbrd.on("item_deleted", function(evt) kbrd.notify("deleted:"..evt.name, "info") end)
kbrd.on("board_refresh", function(evt) kbrd.notify("refresh:"..evt.reason, "info") end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	h.OnEvent(events.ItemCreated{Item: events.ItemRef{Column: "todo", Name: "x"}})
	h.OnEvent(events.ItemDeleted{Column: "todo", Name: "x"})
	h.OnEvent(events.BoardRefresh{Reason: "watcher"})
	if len(api.notifies) != 3 {
		t.Fatalf("expected 3 notifies, got %v", api.notifies)
	}
	if !strings.Contains(api.notifies[0], "created:x") ||
		!strings.Contains(api.notifies[1], "deleted:x") ||
		!strings.Contains(api.notifies[2], "refresh:watcher") {
		t.Fatalf("unexpected: %v", api.notifies)
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
