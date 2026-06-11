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
	mu         sync.Mutex
	root       string
	notifies   []string
	moves      []move
	moveErr    error
	creates    []string
	tmplInfos  []events.TemplateInfo
	tmplCalls  []tmplCreate
	tmplErr    error
	renames    []string
	deletes    []string
	refreshes  int
	columns    []string
	cellSets   []cellSet
	cellClear  []int
	cellWipes  int
	vcolSets   []vcolSet
	vcolClears []string
	vcolWipes  int
}

type cellSet struct {
	ID   int
	Opts events.CellOpts
}

type vcolSet struct {
	ID   string
	Spec events.VirtualColumnSpec
}

type move struct {
	From, To, Name string
}

type tmplCreate struct {
	Column, Template string
	Values           map[string]interface{}
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

func (f *fakeAPI) CreateItem(column, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates = append(f.creates, column+"/"+name)
	return nil
}

func (f *fakeAPI) ListTemplates(column string) ([]events.TemplateInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tmplInfos, f.tmplErr
}

func (f *fakeAPI) CreateItemFromTemplate(column, template string, values map[string]interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tmplCalls = append(f.tmplCalls, tmplCreate{Column: column, Template: template, Values: values})
	return f.tmplErr
}

func (f *fakeAPI) RenameItem(item events.ItemRef, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renames = append(f.renames, item.Column+"/"+item.Name+"->"+newName)
	return nil
}

func (f *fakeAPI) DeleteItem(item events.ItemRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, item.Column+"/"+item.Name)
	return nil
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

func (f *fakeAPI) CellSet(id int, opts events.CellOpts) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cellSets = append(f.cellSets, cellSet{ID: id, Opts: opts})
}

func (f *fakeAPI) CellClear(id int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cellClear = append(f.cellClear, id)
}

func (f *fakeAPI) CellClearAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cellWipes++
}

func (f *fakeAPI) VirtualColumnSet(id string, spec events.VirtualColumnSpec) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vcolSets = append(f.vcolSets, vcolSet{ID: id, Spec: spec})
}

func (f *fakeAPI) VirtualColumnClear(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vcolClears = append(f.vcolClears, id)
}

func (f *fakeAPI) VirtualColumnClearAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vcolWipes++
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
	h, err := New(cfg, &fakeAPI{}, nil, t.TempDir(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h != nil {
		t.Fatal("expected nil host when disabled")
	}
}

func TestHostNoInitFiles(t *testing.T) {
	h, err := New(defaultCfg(), &fakeAPI{}, nil, t.TempDir(), "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	cmds := h.Commands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if cmds[0].ID != "a" || cmds[0].Name != "Archive" || cmds[0].Source != config.SourceLua {
		t.Fatalf("unexpected command: %+v", cmds[0])
	}

	if _, err := h.RunCommand(cmds[0].LuaRef, map[string]string{"fileName": "foo.md"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "ran:foo.md") {
		t.Fatalf("notify not invoked: %v", api.notifies)
	}
}

func TestVirtualColumnSet(t *testing.T) {
	dir := writeInit(t, `
kbrd.column.set("tasks", {
  name = "Tasks",
  empty = "none",
  items = {
    { separator = true, title = "Group" },
    { id = "t1", title = "first", meta = "src", data = { path = "/a.md", line = 3 } },
  },
  commands = {
    { id = "done", name = "Mark done", key = "c", default = true,
      run = function(ctx) kbrd.notify("done:"..ctx.title..":"..ctx.data.path) end },
  },
})`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if len(api.vcolSets) != 1 {
		t.Fatalf("expected 1 VirtualColumnSet, got %d", len(api.vcolSets))
	}
	set := api.vcolSets[0]
	if set.ID != "tasks" || set.Spec.Name != "Tasks" || set.Spec.Empty != "none" {
		t.Fatalf("unexpected spec: %+v", set)
	}
	if len(set.Spec.Items) != 2 || !set.Spec.Items[0].Separator || set.Spec.Items[1].ID != "t1" {
		t.Fatalf("items not parsed: %+v", set.Spec.Items)
	}
	if set.Spec.Items[1].Data["path"] != "/a.md" {
		t.Fatalf("data not parsed: %+v", set.Spec.Items[1].Data)
	}
	if len(set.Spec.Commands) != 1 || set.Spec.Commands[0].Ref != "vcol:tasks:done" || !set.Spec.Commands[0].Default {
		t.Fatalf("commands not parsed: %+v", set.Spec.Commands)
	}

	// Dispatch the column command with a structured ctx — ctx.data round-trips.
	ref := set.Spec.Commands[0].Ref
	if _, err := h.RunVirtualCommand(ref, map[string]interface{}{
		"title": "first",
		"data":  map[string]interface{}{"path": "/a.md"},
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "done:first:/a.md") {
		t.Fatalf("virtual command did not run with data: %v", api.notifies)
	}

	// Clearing drops the run closure so the ref no longer resolves.
	h.clearVcolFns("tasks")
	_, err = h.RunVirtualCommand(ref, map[string]interface{}{"data": map[string]interface{}{"path": "/a.md"}})
	if err == nil || !strings.Contains(err.Error(), "unknown lua command") {
		t.Fatalf("expected unknown-command error after clear, got %v", err)
	}
}

func TestVirtualColumnSet_RequiresItemDefault(t *testing.T) {
	dir := writeInit(t, `
kbrd.column.set("tasks", {
  name = "Tasks",
  commands = {
    { id = "add",  name = "Add",  requiresItem = false, run = function() end },
    { id = "done", name = "Done", run = function() end },
    { id = "edit", name = "Edit", requiresItem = true,  run = function() end },
  },
})`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if len(api.vcolSets) != 1 {
		t.Fatalf("expected 1 VirtualColumnSet, got %d", len(api.vcolSets))
	}
	got := map[string]bool{}
	for _, c := range api.vcolSets[0].Spec.Commands {
		got[c.ID] = c.RequiresItem
	}
	if got["add"] {
		t.Error("add RequiresItem = true, want false (explicit)")
	}
	if !got["done"] {
		t.Error("done RequiresItem = false, want true (omitted defaults true)")
	}
	if !got["edit"] {
		t.Error("edit RequiresItem = false, want true (explicit)")
	}
}

func TestHasCommand(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("archive", "Archive", function() end)
if kbrd.has_command("archive") then kbrd.notify("yes-archive") end
if not kbrd.has_command("missing") then kbrd.notify("no-missing") end
if kbrd.has_command("archive") == false then kbrd.notify("wrong") end
`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if len(api.notifies) != 2 {
		t.Fatalf("expected 2 notifies, got %d: %v", len(api.notifies), api.notifies)
	}
	if !strings.Contains(api.notifies[0], "yes-archive") {
		t.Errorf("notify[0]: %q", api.notifies[0])
	}
	if !strings.Contains(api.notifies[1], "no-missing") {
		t.Errorf("notify[1]: %q", api.notifies[1])
	}
}

func TestHookFires(t *testing.T) {
	dir := writeInit(t, `kbrd.on("git_sync_done", function(evt) kbrd.notify("hook:"..tostring(evt.ok), "info") end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(cfg, api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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

func TestBoardCreateRenameDelete(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("c", "CRD", function(ctx)
  kbrd.board.create(ctx.columnName, "fresh")
  kbrd.board.rename({column = ctx.columnName, name = "fresh"}, "renamed")
  kbrd.board.delete({column = ctx.columnName, name = "renamed"})
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	cmds := h.Commands()
	if _, err := h.RunCommand(cmds[0].LuaRef, map[string]string{"columnName": "todo"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.creates) != 1 || api.creates[0] != "todo/fresh" {
		t.Errorf("creates: %+v", api.creates)
	}
	if len(api.renames) != 1 || api.renames[0] != "todo/fresh->renamed" {
		t.Errorf("renames: %+v", api.renames)
	}
	if len(api.deletes) != 1 || api.deletes[0] != "todo/renamed" {
		t.Errorf("deletes: %+v", api.deletes)
	}
}

func TestBoardTemplates(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("t", "Tmpl", function(ctx)
  local tmpls, err = kbrd.board.templates(ctx.columnName)
  kbrd.notify("count:"..#tmpls.." first:"..tmpls[1].name.."/"..tmpls[1].scope, "info")
  kbrd.board.createFromTemplate(ctx.columnName, "Bug report", {
    title = "Crash",
    areas = {"UI", "data"},
    regression = true,
  })
end)`)
	api := &fakeAPI{tmplInfos: []events.TemplateInfo{
		{Name: "Bug report", Scope: "column"},
		{Name: "Task", Scope: "board"},
	}}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	cmds := h.Commands()
	if _, err := h.RunCommand(cmds[0].LuaRef, map[string]string{"columnName": "todo"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.notifies) != 1 || api.notifies[0] != "info:count:2 first:Bug report/column" {
		t.Errorf("notifies: %+v", api.notifies)
	}
	if len(api.tmplCalls) != 1 {
		t.Fatalf("tmplCalls: %+v", api.tmplCalls)
	}
	call := api.tmplCalls[0]
	if call.Column != "todo" || call.Template != "Bug report" {
		t.Errorf("call: %+v", call)
	}
	if call.Values["title"] != "Crash" || call.Values["regression"] != true {
		t.Errorf("values: %#v", call.Values)
	}
	areas, ok := call.Values["areas"].([]interface{})
	if !ok || len(areas) != 2 || areas[0] != "UI" || areas[1] != "data" {
		t.Errorf("areas: %#v", call.Values["areas"])
	}
}

func TestBoardCreateFromTemplateError(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("t", "Tmpl", function(ctx)
  local ok, err = kbrd.board.createFromTemplate("todo", "Nope", {})
  if not ok then kbrd.notify("err:"..err, "error") end
end)`)
	api := &fakeAPI{tmplErr: errors.New("template \"Nope\" not found")}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	cmds := h.Commands()
	if _, err := h.RunCommand(cmds[0].LuaRef, map[string]string{"columnName": "todo"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.notifies) != 1 || api.notifies[0] != `error:err:template "Nope" not found` {
		t.Errorf("notifies: %+v", api.notifies)
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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

func TestCellAPI(t *testing.T) {
	dir := writeInit(t, `kbrd.command("c", "Cells", function()
  kbrd.cell.set(1, {text = "hi", fg = "#7fd962", bold = true})
  kbrd.cell.clear(2)
  kbrd.cell.clear_all()
end)`)
	api := &fakeAPI{root: dir}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(api.cellSets) != 1 || api.cellSets[0].ID != 1 {
		t.Fatalf("expected one cell set with id 1, got %+v", api.cellSets)
	}
	got := api.cellSets[0].Opts
	if got.Text != "hi" || got.FG != "#7fd962" || !got.Bold {
		t.Fatalf("unexpected opts: %+v", got)
	}
	if len(api.cellClear) != 1 || api.cellClear[0] != 2 {
		t.Fatalf("expected clear of id 2, got %v", api.cellClear)
	}
	if api.cellWipes != 1 {
		t.Fatalf("expected 1 clear_all, got %d", api.cellWipes)
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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

func TestTimerInstanceMatchSchedules(t *testing.T) {
	dir := writeInit(t, `kbrd.timer.every(150, function() end, { instance = "server" })`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "server")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if pending := h.PendingTimers(); len(pending) != 1 {
		t.Fatalf("instance match should schedule the timer, got %d pending", len(pending))
	}
}

func TestTimerInstanceMismatchSkips(t *testing.T) {
	dir := writeInit(t, `
local handle = kbrd.timer.every(150, function() end, { instance = "server" })
kbrd.notify("handle:"..handle, "info")
`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "laptop")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if pending := h.PendingTimers(); len(pending) != 0 {
		t.Fatalf("instance mismatch should not schedule, got %d pending", len(pending))
	}
	// A skipped timer still returns an inert handle so the script can store it.
	if len(api.notifies) != 1 || !strings.HasPrefix(api.notifies[0], "info:handle:co-") {
		t.Fatalf("expected an inert handle notify, got %v", api.notifies)
	}
}

func TestTimerNoInstanceRunsEverywhere(t *testing.T) {
	dir := writeInit(t, `kbrd.timer.every(150, function() end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "laptop")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if pending := h.PendingTimers(); len(pending) != 1 {
		t.Fatalf("an unscoped timer should schedule on any instance, got %d pending", len(pending))
	}
}

func TestInstanceNameExposed(t *testing.T) {
	dir := writeInit(t, `kbrd.notify("name:"..kbrd.instance.name, "info")`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "server")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if len(api.notifies) != 1 || api.notifies[0] != "info:name:server" {
		t.Fatalf("expected kbrd.instance.name to be \"server\", got %v", api.notifies)
	}
}

func TestTimerMinClamp(t *testing.T) {
	dir := writeInit(t, `kbrd.timer.after(5, function() end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
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

func TestTimerDurationString(t *testing.T) {
	dir := writeInit(t, `
kbrd.timer.every("1s", function() end)
kbrd.timer.after("250ms", function() end)
`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	pending := h.PendingTimers()
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending timers, got %d", len(pending))
	}
	if pending[0].Duration != time.Second || !pending[0].Repeat {
		t.Fatalf("every: got dur=%v repeat=%v", pending[0].Duration, pending[0].Repeat)
	}
	if pending[1].Duration != 250*time.Millisecond || pending[1].Repeat {
		t.Fatalf("after: got dur=%v repeat=%v", pending[1].Duration, pending[1].Repeat)
	}
}

func TestTimerInvalidDuration(t *testing.T) {
	dir := writeInit(t, `kbrd.timer.after("not-a-duration", function() end)`)
	if _, err := New(defaultCfg(), &fakeAPI{}, nil, dir, ""); err == nil {
		t.Fatalf("expected error for invalid duration string")
	}
}

func TestStatusPending(t *testing.T) {
	dir := writeInit(t, `
kbrd.status("first")
kbrd.status("second", "5s")
`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	got := h.PendingStatus()
	if len(got) != 2 || got[0].Text != "first" || got[1].Text != "second" {
		t.Fatalf("unexpected status queue: %v", got)
	}
	if got[0].TTL != 0 {
		t.Fatalf("first should use default TTL (0), got %v", got[0].TTL)
	}
	if got[1].TTL != 5*time.Second {
		t.Fatalf("second TTL: got %v", got[1].TTL)
	}
	// Drained — second call returns nothing.
	if rest := h.PendingStatus(); len(rest) != 0 {
		t.Fatalf("expected drained queue, got %v", rest)
	}
}

func TestTimerFireOnce(t *testing.T) {
	dir := writeInit(t, `kbrd.timer.after(100, function() kbrd.notify("fired", "info") end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(cfg, api, nil, dir, "")
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
	h, err := New(cfg, api, nil, dir, "")
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
	h, err := New(cfg, api, nil, dir, "")
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
	h, err := New(cfg, api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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
	h, err := New(defaultCfg(), api, nil, dir, "")
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

func TestItemSavedAndChangedHooks(t *testing.T) {
	dir := writeInit(t, `
kbrd.on("item_saved", function(evt) kbrd.notify("saved:"..evt.item.name..":"..evt.kind, "info") end)
kbrd.on("item_changed", function(evt) kbrd.notify("changed:"..evt.item.name, "info") end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	h.OnEvent(events.ItemSaved{Item: events.ItemRef{Column: "todo", Name: "x"}, Kind: "append"})
	h.OnEvent(events.ItemChanged{Item: events.ItemRef{Column: "todo", Name: "x"}})
	if len(api.notifies) != 2 {
		t.Fatalf("expected 2 notifies, got %v", api.notifies)
	}
	if !strings.Contains(api.notifies[0], "saved:x:append") ||
		!strings.Contains(api.notifies[1], "changed:x") {
		t.Fatalf("unexpected: %v", api.notifies)
	}
}

func TestAsyncSchedule(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("a", "Async", function()
  local h = kbrd.async.run("echo hi", function(r) kbrd.notify("got:"..r.out, "info") end)
  kbrd.notify("handle:"..h, "info")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	pending := h.PendingAsync()
	if len(pending) != 1 {
		t.Fatalf("expected 1 async job, got %d", len(pending))
	}
	if pending[0].Shell != "echo hi" {
		t.Fatalf("unexpected shell: %q", pending[0].Shell)
	}
	if len(api.notifies) != 1 || !strings.HasPrefix(api.notifies[0], "info:handle:") {
		t.Fatalf("expected handle notify, got %v", api.notifies)
	}
	// Simulate the goroutine finishing.
	if err := h.FireAsync(pending[0].Token, "hi\n", 0, ""); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if len(api.notifies) != 2 || !strings.Contains(api.notifies[1], "got:hi") {
		t.Fatalf("callback didn't fire correctly: %v", api.notifies)
	}
}

func TestAsyncCancel(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("a", "Cancel", function()
  local h = kbrd.async.run("echo never", function() kbrd.notify("should not run", "info") end)
  kbrd.async.cancel(h)
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	pending := h.PendingAsync()
	// Fire — should be a no-op because the callback was cancelled.
	if err := h.FireAsync(pending[0].Token, "never", 0, ""); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if len(api.notifies) != 0 {
		t.Fatalf("cancelled async should not invoke callback: %v", api.notifies)
	}
}

func TestAsyncReceivesError(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("a", "Failing", function()
  kbrd.async.run("false", function(r)
    kbrd.notify("exit:"..r.exitCode.." err:"..r.error, "info")
  end)
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	pending := h.PendingAsync()
	_ = h.FireAsync(pending[0].Token, "", 1, "")
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "exit:1") {
		t.Fatalf("expected exit:1 notify, got %v", api.notifies)
	}
}

func TestAsyncFromTimerRejected(t *testing.T) {
	dir := writeInit(t, `
kbrd.timer.after(100, function()
  local ok = pcall(function()
    kbrd.async.run("echo nope", function() end)
  end)
  kbrd.notify("from-timer:"..tostring(ok), "info")
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	token := h.PendingTimers()[0].Token
	_ = h.FireTimer(token)
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "from-timer:false") {
		t.Fatalf("async from timer should be rejected: %v", api.notifies)
	}
}

func TestAsyncChained(t *testing.T) {
	// First async schedules a second async from inside its callback —
	// should work, no nesting restriction for async callbacks.
	dir := writeInit(t, `
kbrd.command("a", "Chain", function()
  kbrd.async.run("step-1", function(r1)
    kbrd.async.run("step-2", function(r2)
      kbrd.notify("done:"..r1.out.."/"..r2.out, "info")
    end)
  end)
end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	_, _ = h.RunCommand(h.Commands()[0].LuaRef, nil)
	pending := h.PendingAsync()
	if len(pending) != 1 {
		t.Fatalf("expected 1 first-step pending, got %d", len(pending))
	}
	_ = h.FireAsync(pending[0].Token, "A", 0, "")
	// The callback should have scheduled the second async.
	pending2 := h.PendingAsync()
	if len(pending2) != 1 {
		t.Fatalf("expected 1 second-step pending, got %d", len(pending2))
	}
	_ = h.FireAsync(pending2[0].Token, "B", 0, "")
	if len(api.notifies) != 1 || !strings.Contains(api.notifies[0], "done:A/B") {
		t.Fatalf("chained async failed: %v", api.notifies)
	}
}

func TestParseError(t *testing.T) {
	dir := writeInit(t, `this is not valid lua @@@`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
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
