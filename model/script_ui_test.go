package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"kbrd/config"
	"kbrd/script"
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
	b.initRuntime()
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	return b, dir
}

// runMsg pumps a tea.Msg through Board.Update and recursively executes any
// returned tea.Cmd, so we end up with the steady-state model+side effects.
func runMsg(t *testing.T, b *Board, msg tea.Msg) {
	t.Helper()
	for range 50 {
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
	if b.scriptUI.selectOne.SelectedIndex() != 0 {
		t.Fatalf("expected first choice selected, got %d", b.scriptUI.selectOne.SelectedIndex())
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
	_, c := b.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if c == nil {
		t.Fatal("expected resume cmd from picker enter")
	}
	msg := c()
	rm, ok := msg.(scriptResumeMsg)
	if !ok {
		t.Fatalf("expected scriptResumeMsg, got %T %+v", msg, msg)
	}
	result, ok := rm.Result.(script.UIResult)
	if !ok || result.Value != "1" || !result.Submitted {
		t.Fatalf("expected submitted result ID '1', got %#v", rm.Result)
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
	b.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if b.scriptUI.selectOne.SelectedIndex() != 1 {
		t.Fatalf("expected selected=1 after down, got %d", b.scriptUI.selectOne.SelectedIndex())
	}
	// Press down again.
	b.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if b.scriptUI.selectOne.SelectedIndex() != 2 {
		t.Fatalf("expected selected=2, got %d", b.scriptUI.selectOne.SelectedIndex())
	}
	// Press up.
	b.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if b.scriptUI.selectOne.SelectedIndex() != 1 {
		t.Fatalf("expected selected=1 after up, got %d", b.scriptUI.selectOne.SelectedIndex())
	}
	// Press esc — should cancel.
	_, c := b.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if c == nil {
		t.Fatal("esc should produce a resume cmd")
	}
	msg := c()
	rm, ok := msg.(scriptResumeMsg)
	if !ok {
		t.Fatalf("expected scriptResumeMsg, got %T", msg)
	}
	result, ok := rm.Result.(script.UIResult)
	if !ok || !result.Cancelled {
		t.Fatalf("expected cancellation result, got %#v", rm.Result)
	}
}

func TestScriptUITextareaReturnsEditedValue(t *testing.T) {
	b, _ := makeBoard(t, `
kbrd.command("s", "Scratchpad", function()
	local result = kbrd.ui.textarea({initial="hello", actions={
		{id="save", label="Save", key="ctrl+s"},
	}})
	kbrd.notify(result.action .. ":" .. result.value)
end)`)
	b.Update(runCustomCommandMsg{Cmd: b.commands[0]})
	if b.scriptUI.kind != scriptUITextarea {
		t.Fatalf("kind = %v", b.scriptUI.kind)
	}
	b.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	_, cmd := b.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("textarea action did not produce resume command")
	}
	resume, ok := cmd().(scriptResumeMsg)
	if !ok {
		t.Fatal("textarea result was not scriptResumeMsg")
	}
	result := resume.Result.(script.UIResult)
	if result.Action != "save" || result.Value != "hello!" {
		t.Fatalf("result = %+v", result)
	}
	runMsg(t, b, resume)
	if b.scriptUI.Active() {
		t.Fatal("textarea remained active after resume")
	}
}

func TestScriptUIViewerActionResumesCommand(t *testing.T) {
	b, _ := makeBoard(t, `
kbrd.command("v", "View", function()
  local result = kbrd.ui.viewer({content="+change", format="diff", actions={
    {id="apply", label="Apply", key="ctrl+a"},
  }})
  kbrd.notify(result.action)
end)`)
	b.Update(runCustomCommandMsg{Cmd: b.commands[0]})
	if b.scriptUI.kind != scriptUIViewer {
		t.Fatalf("kind = %v", b.scriptUI.kind)
	}
	_, cmd := b.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("viewer action did not produce resume command")
	}
	resume := cmd().(scriptResumeMsg)
	result := resume.Result.(script.UIResult)
	if result.Action != "apply" || !result.Submitted {
		t.Fatalf("result = %+v", result)
	}
	runMsg(t, b, resume)
}

func TestScriptUIViewerReceivesMouseWheelFromModalRouter(t *testing.T) {
	content := strings.Repeat("line\n", 40)
	b, _ := makeBoard(t, `
kbrd.command("v", "View", function()
  kbrd.ui.viewer({content=[[`+content+`]]})
end)`)
	b.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	b.Update(runCustomCommandMsg{Cmd: b.commands[0]})
	if b.scriptUI.kind != scriptUIViewer {
		t.Fatalf("kind = %v", b.scriptUI.kind)
	}
	b.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if b.scriptUI.viewer.Offset() != 3 {
		t.Fatalf("viewer offset = %d", b.scriptUI.viewer.Offset())
	}
}

func TestScriptUIMultiSelectSubmit(t *testing.T) {
	b, _ := makeBoard(t, `
kbrd.command("m", "Multi", function()
  local result = kbrd.ui.multiselect({items={{id="ui",label="UI"},{id="data",label="Data"}}})
  if result.submitted then kbrd.notify(table.concat(result.ids, ","), "success") end
end)`)
	b.Update(runCustomCommandMsg{Cmd: b.commands[0], Vars: nil})
	if !b.scriptUI.Active() || b.scriptUI.kind != scriptUIMultiSelect {
		t.Fatalf("multiselect did not open: kind=%v", b.scriptUI.kind)
	}
	b.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	b.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	b.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	_, cmd := b.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did not produce resume command")
	}
	raw := cmd()
	msg, ok := raw.(scriptResumeMsg)
	if !ok {
		t.Fatalf("resume message = %T", raw)
	}
	result, ok := msg.Result.(script.UIResult)
	if !ok || !result.Submitted || len(result.IDs) != 2 || result.IDs[0] != "ui" || result.IDs[1] != "data" {
		t.Fatalf("result = %#v", msg.Result)
	}
}

func TestScriptUIFormOpenAndCancel(t *testing.T) {
	b, _ := makeBoard(t, `
kbrd.command("f", "Form", function()
  local result = kbrd.ui.form({title="Promote", fields={{id="title",type="input",label="Title",required=true}}})
  if result.cancelled then kbrd.notify("cancelled") end
end)`)
	b.Update(runCustomCommandMsg{Cmd: b.commands[0], Vars: nil})
	if !b.scriptUI.Active() || b.scriptUI.kind != scriptUIForm {
		t.Fatalf("form did not open: kind=%v", b.scriptUI.kind)
	}
	if view := b.scriptUI.View(); !strings.Contains(view, "Promote") || !strings.Contains(view, "Title") {
		t.Fatalf("unexpected form view: %q", view)
	}
	_, cmd := b.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("escape did not produce resume command")
	}
	raw := cmd()
	msg, ok := raw.(scriptResumeMsg)
	if !ok {
		t.Fatalf("resume message = %T", raw)
	}
	result, ok := msg.Result.(script.UIResult)
	if !ok || !result.Cancelled {
		t.Fatalf("result = %#v", msg.Result)
	}
}

func TestScriptUIFormSubmitResumesLuaWithTypedValues(t *testing.T) {
	b, dir := makeBoard(t, `
kbrd.command("f", "Form", function()
  local result = kbrd.ui.form({fields={
    {id="remove", type="checkbox", label="Remove", initial=true},
  }})
  kbrd.fs.write("FORM_RESULT", type(result.values.remove)..":"..tostring(result.values.remove))
end)`)

	_, initCmd := b.Update(runCustomCommandMsg{Cmd: b.commands[0], Vars: nil})
	pumpScriptUICmds(t, b, initCmd)
	if !b.scriptUI.Active() || b.scriptUI.kind != scriptUIForm {
		t.Fatalf("form did not open: kind=%v", b.scriptUI.kind)
	}

	_, submitCmd := b.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	pumpScriptUICmds(t, b, submitCmd)

	body, err := os.ReadFile(filepath.Join(dir, "FORM_RESULT"))
	if err != nil || string(body) != "boolean:true" {
		t.Fatalf("form result = %q, err=%v", body, err)
	}
	if b.scriptUI.Active() {
		t.Fatal("form remained active after submission")
	}
}

func pumpScriptUICmds(t *testing.T, b *Board, initial tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{initial}
	for steps := 0; len(queue) > 0; steps++ {
		if steps >= 50 {
			t.Fatal("script UI command loop did not converge")
		}
		cmd := queue[0]
		queue = queue[1:]
		if cmd == nil {
			continue
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		if msg == nil {
			continue
		}
		_, next := b.Update(msg)
		if next != nil {
			queue = append(queue, next)
		}
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
	_, cmd := b.Update(editorNewMsg{Column: refForColumn(b.columns[0]), ColIndex: 0, FileName: "fresh"})
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
	view := b.View().Content
	if !strings.Contains(view, "Hello") {
		t.Fatalf("view should contain picker title 'Hello'; got: %s", view)
	}
	if !strings.Contains(view, "a") || !strings.Contains(view, "b") {
		t.Fatalf("view should contain choices; got: %s", view)
	}
}

func TestScriptUIConfirmEscapeCancelsCoroutine(t *testing.T) {
	b, _ := makeBoard(t, `
kbrd.command("c", "Confirm", function()
  local ok = kbrd.ui.confirm("Continue?")
  kbrd.fs.write("ANSWER", tostring(ok))
end)`)
	_, cmd := b.Update(runCustomCommandMsg{Cmd: b.commands[0]})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("opening confirm produced %T", msg)
		}
	}
	if b.dialog.active || b.scriptUI.kind != scriptUIConfirm {
		t.Fatalf("confirm routing incorrect: dialog=%v script=%v", b.dialog.active, b.scriptUI.kind)
	}
	_, cancelCmd := b.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cancelCmd == nil {
		t.Fatal("escape did not produce a cancellation command")
	}
	runMsg(t, b, cancelCmd())
	if b.scriptUI.Active() || b.dialog.active {
		t.Fatal("confirm remained active after cancellation")
	}
	body, err := os.ReadFile(filepath.Join(b.cfg.Path, "ANSWER"))
	if err != nil || string(body) != "false" {
		t.Fatalf("cancelled confirm answer = %q, err=%v", body, err)
	}
}

func TestScriptUIRoutesNonKeyMessages(t *testing.T) {
	b, _ := makeBoard(t, `kbrd.command("p", "Prompt", function() kbrd.ui.prompt("Name", "initial") end)`)
	b.Update(runCustomCommandMsg{Cmd: b.commands[0]})
	if !b.scriptUI.Active() {
		t.Fatal("prompt not active")
	}
	type internalWidgetMsg struct{}
	b.Update(internalWidgetMsg{})
	if !b.scriptUI.Active() || b.scriptUI.input.Value() != "initial" {
		t.Fatal("non-key message was not safely routed to the active scripted UI")
	}
}

func TestScriptUIStaleResumeCannotReachReloadedHost(t *testing.T) {
	b, _ := makeBoard(t, `kbrd.command("p", "Prompt", function() kbrd.ui.prompt("Name", "") end)`)
	b.Update(runCustomCommandMsg{Cmd: b.commands[0]})
	staleToken := b.scriptUI.token
	if err := b.initRuntime(); err != nil {
		t.Fatalf("reload runtime: %v", err)
	}
	b.Update(runCustomCommandMsg{Cmd: b.commands[0]})
	freshToken := b.scriptUI.token
	if staleToken == freshToken {
		t.Fatalf("host reload reused UI token %q", staleToken)
	}
	b.Update(scriptResumeMsg{
		Name:   "Prompt",
		Token:  staleToken,
		Result: script.UIResult{Submitted: true, Action: "submit", Value: "stale"},
	})
	if !b.scriptUI.Active() || b.scriptUI.token != freshToken {
		t.Fatal("stale response closed or resumed the newer scripted UI")
	}
}

func TestScriptUIBoardSwitchDiscardsPendingCoroutine(t *testing.T) {
	b, oldBoard := makeBoard(t, `
kbrd.command("p", "Prompt", function()
  kbrd.ui.prompt("Name", "")
  kbrd.fs.write("OLD_MUTATION", "resumed")
end)`)
	t.Cleanup(b.Close)
	b.Update(runCustomCommandMsg{Cmd: b.commands[0]})
	if !b.scriptUI.Active() {
		t.Fatal("prompt not active before board switch")
	}
	staleToken := b.scriptUI.token

	newBoard := t.TempDir()
	if err := os.MkdirAll(filepath.Join(newBoard, "todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newBoard, config.FolderConfigFile), []byte(`
[notify]
backend = "none"
[scripting]
enabled = true
command_timeout_ms = 2000
hook_timeout_ms = 500
instruction_limit = 10000000
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newBoard, ".kbrd.lua"), []byte(`
kbrd.command("n", "New board command", function() end)
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := b.session().loadBoard(newBoard); err != nil {
		t.Fatalf("switch board: %v", err)
	}
	if b.scriptUI.Active() || b.dialog.active {
		t.Fatal("scripted modal remained active after board switch")
	}
	if b.scripts == nil {
		t.Fatal("new board script host was not initialized")
	}

	_, cmd := b.Update(scriptResumeMsg{
		Name:   "Prompt",
		Token:  staleToken,
		Result: script.UIResult{Submitted: true, Action: "submit", Value: "stale"},
	})
	if cmd != nil {
		t.Fatal("stale response produced a command after board switch")
	}
	for _, root := range []string{oldBoard, newBoard} {
		if _, err := os.Stat(filepath.Join(root, "OLD_MUTATION")); !os.IsNotExist(err) {
			t.Fatalf("cancelled old-board coroutine mutated %s: %v", root, err)
		}
	}
}

func TestScriptUITableWidgetsChainThroughSharedControls(t *testing.T) {
	b, dir := makeBoard(t, `
kbrd.command("w", "Widgets", function()
  local input = kbrd.ui.input({title="Input", initial="ok", required=true})
  local selected = kbrd.ui.select({title="Select", initial_id="b", items={
    {id="a", label="A"}, {id="b", label="B"},
  }})
  local confirmed = kbrd.ui.confirm({title="Confirm", default=true, confirm_label="Do it"})
  local action = kbrd.ui.actions({title="Actions", actions={{id="save", label="Save", key="ctrl+s"}}})
  kbrd.fs.write("WIDGETS", input.value..":"..selected.value..":"..tostring(confirmed.value)..":"..action.action)
end)`)
	b.Update(runCustomCommandMsg{Cmd: b.commands[0]})
	if b.scriptUI.kind != scriptUIInput {
		t.Fatalf("first widget kind = %v", b.scriptUI.kind)
	}
	resumeScriptWidget(t, b, tea.KeyPressMsg{Code: tea.KeyEnter})
	if b.scriptUI.kind != scriptUISelect {
		t.Fatalf("second widget kind = %v", b.scriptUI.kind)
	}
	resumeScriptWidget(t, b, tea.KeyPressMsg{Code: tea.KeyEnter})
	if b.scriptUI.kind != scriptUIConfirm || !strings.Contains(b.scriptUI.View(), "Do it") {
		t.Fatalf("confirm widget not opened with custom label: %q", b.scriptUI.View())
	}
	resumeScriptWidget(t, b, tea.KeyPressMsg{Code: tea.KeyEnter})
	if b.scriptUI.kind != scriptUIActions {
		t.Fatalf("fourth widget kind = %v", b.scriptUI.kind)
	}
	resumeScriptWidget(t, b, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	body, err := os.ReadFile(filepath.Join(dir, "WIDGETS"))
	if err != nil || string(body) != "ok:b:true:save" {
		t.Fatalf("widget chain result = %q, err=%v", body, err)
	}
}

func resumeScriptWidget(t *testing.T, b *Board, msg tea.Msg) {
	t.Helper()
	_, cmd := b.Update(msg)
	if cmd == nil {
		t.Fatalf("%T did not resolve active scripted widget", msg)
	}
	resume := cmd()
	if _, ok := resume.(scriptResumeMsg); !ok {
		t.Fatalf("widget command returned %T", resume)
	}
	_, follow := b.Update(resume)
	if follow != nil {
		for next := follow(); next != nil; {
			_, follow = b.Update(next)
			if follow == nil {
				break
			}
			next = follow()
		}
	}
}
