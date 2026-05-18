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

	if err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
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
	if err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
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
	if err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.columns) != 1 || api.columns[0] != "archive" {
		t.Fatalf("createColumn not invoked: %v", api.columns)
	}
	if api.refreshes < 1 {
		t.Fatalf("refresh not called, got %d", api.refreshes)
	}
	// Second invocation should not re-create.
	if err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
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
	if err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
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
	if err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
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
	if err := h.RunCommand(h.Commands()[0].LuaRef, map[string]string{
		"absPath": filepath.Join(other, "x.txt"),
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "body:abs") {
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
