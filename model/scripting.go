package model

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/boardops"
	"kbrd/config"
	"kbrd/events"
	"kbrd/script"
	"kbrd/shellcmd"
)

// initScripting creates the Lua host (if enabled and init files exist) and
// subscribes it to the event bus. Idempotent: a second call closes the
// previous host first, which is what board-switching needs.
func (b *Board) initScripting() {
	if b.scripts != nil {
		b.scripts.Close()
		b.scripts = nil
	}
	b.bus = events.Bus{}

	if !b.cfg.Scripting.Enabled {
		return
	}
	logger := script.NewFileLogger()
	host, err := script.New(b.cfg.Scripting, boardScriptAPI{b: b}, logger, b.cfg.Path, b.cfg.InstanceName)
	if err != nil && host == nil {
		// Hard failure during init — surface but keep running.
		b.commandWarnings = append(b.commandWarnings, config.CommandLoadWarning{
			Source:  "init.lua",
			Message: err.Error(),
		})
		return
	}
	if host == nil {
		return
	}
	if err != nil {
		// Partial failure — some files loaded, others errored.
		b.commandWarnings = append(b.commandWarnings, config.CommandLoadWarning{
			Source:  "init.lua",
			Message: err.Error(),
		})
	}
	b.scripts = host
	b.bus.Subscribe(host)
}

// boardScriptAPI is the TUI capability implementation handed to the Lua host.
// It must remain safe to call while h.mu is held inside the host, so it never
// calls back into the host itself.
type boardScriptAPI struct {
	b *Board
}

// scriptTimerMsg fires when a tea.Tick scheduled for a Lua timer elapses.
// The Board routes it back into Host.FireTimer, which invokes the callback
// and possibly re-schedules.
type scriptTimerMsg struct {
	Token string
}

// scriptStatusExpireMsg clears a kbrd.status message once its TTL elapses.
// Seq guards against a stale tick wiping a newer message: the handler only
// clears when Seq still matches the board's current scriptStatusSeq.
type scriptStatusExpireMsg struct {
	Seq int
}

// scriptStatusTTL is the default lifetime of a kbrd.status message in the
// status bar; callers can override it with kbrd.status(msg, ttl).
const scriptStatusTTL = 3 * time.Second

// scriptAsyncDoneMsg carries the result of a backgrounded shell command
// (kbrd.async.run) back to the host so the Lua callback can be invoked.
type scriptAsyncDoneMsg struct {
	Token    string
	Out      string
	ExitCode int
	Err      string
}

// quitConfirmedMsg is dispatched when the user confirms quitting with unsaved
// editor changes; it discards the edit and proceeds with shutdown.
type quitConfirmedMsg struct{}

// scriptDebugf appends to /tmp/kbrd-script.log when KBRD_SCRIPT_DEBUG=1.
// Stays a no-op otherwise so production runs aren't affected.
func scriptDebugf(format string, args ...any) {
	if os.Getenv("KBRD_SCRIPT_DEBUG") == "" {
		return
	}
	f, err := os.OpenFile("/tmp/kbrd-script.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
}

// collectTimerCmds drains any timer schedules accumulated since the last
// call (during script init.lua execution, command runs, hook fires, or a
// just-fired timer that re-armed). Each becomes a tea.Every (repeating, snaps
// to wall-clock boundaries so the period doesn't drift) or a tea.Tick
// (one-shot) that produces scriptTimerMsg{Token} when the duration elapses.
func (b *Board) collectTimerCmds() tea.Cmd {
	if b.scripts == nil {
		return nil
	}
	pending := b.scripts.PendingTimers()
	if len(pending) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(pending))
	for _, t := range pending {
		token := t.Token
		dur := t.Duration
		fn := func(time.Time) tea.Msg { return scriptTimerMsg{Token: token} }
		if t.Repeat {
			cmds = append(cmds, tea.Every(dur, fn))
		} else {
			cmds = append(cmds, tea.Tick(dur, fn))
		}
	}
	return tea.Batch(cmds...)
}

// collectStatusCmd drains kbrd.status messages accumulated since the last
// call, shows the most recent one in the status bar, and returns a tea.Tick
// that clears it after scriptStatusTTL. The seq counter ensures a later
// message's expiry doesn't get cut short by an earlier one's stale tick.
func (b *Board) collectStatusCmd() tea.Cmd {
	if b.scripts == nil {
		return nil
	}
	pending := b.scripts.PendingStatus()
	if len(pending) == 0 {
		return nil
	}
	latest := pending[len(pending)-1]
	b.scriptStatus = latest.Text
	b.scriptStatusSeq++
	seq := b.scriptStatusSeq
	ttl := latest.TTL
	if ttl <= 0 {
		ttl = scriptStatusTTL
	}
	return tea.Tick(ttl, func(time.Time) tea.Msg {
		return scriptStatusExpireMsg{Seq: seq}
	})
}

// collectEditorOpenCmd drains kbrd.editor.open requests and opens the editor for
// the resolved target at the requested line (focusing its column/item).
func (b *Board) collectEditorOpenCmd() tea.Cmd {
	if b.scripts == nil {
		return nil
	}
	reqs := b.scripts.PendingEditorOpen()
	if len(reqs) == 0 {
		return nil
	}
	req := reqs[len(reqs)-1] // the last request wins (only one editor)
	// Refuse to replace an editor that has unsaved changes — opening another file
	// would silently discard them (a plausible path: kbrd.editor.open from inside
	// :lua, drained right after the eval while the dirty editor is still open). The
	// request is dropped; the user saves (:w) or discards (:q!) and re-issues it.
	if b.editor.state != editorNone && b.editor.IsDirty() {
		return b.notifier.Error("editor.open: unsaved changes — save or discard first")
	}
	colIdx, item := b.resolveEditorTarget(req)
	if item == nil {
		return b.notifier.Error("editor.open: item not found")
	}
	b.selectedCol = colIdx
	b.columns[colIdx].SelectByName(item.Name)
	cmd := b.editor.OpenEdit(colIdx, b.columns[colIdx].Path, item.Name, item.FullPath)
	b.editor.GoToLine(req.Line)
	return cmd
}

// resolveEditorTarget finds the column index and item for an editor-open request:
// by path (specific current-board path or bare basename/name), by column+name,
// or — when the target is empty — the current selection.
func (b *Board) resolveEditorTarget(req script.EditorOpenReq) (int, *Item) {
	if req.Path != "" {
		if editorOpenSpecificPath(req.Path) {
			paths := []string{req.Path}
			if !filepath.IsAbs(req.Path) {
				paths = append(paths, filepath.Join(b.cfg.Path, req.Path))
			}
			for _, path := range paths {
				for ci, col := range b.columns {
					if it := col.ItemByPath(path); it != nil {
						return ci, it
					}
				}
			}
			return -1, nil
		}
		for ci, col := range b.columns {
			if it := col.ItemByPath(req.Path); it != nil {
				return ci, it
			}
			name := strings.TrimSuffix(filepath.Base(req.Path), ".md")
			if it := col.ItemByName(name); it != nil {
				return ci, it
			}
		}
		return -1, nil
	}
	if req.Column != "" || req.Name != "" {
		for ci, col := range b.columns {
			if req.Column != "" && col.Name != req.Column {
				continue
			}
			if it := col.ItemByName(req.Name); it != nil {
				return ci, it
			}
		}
		return -1, nil
	}
	if b.selectedCol >= 0 && b.selectedCol < len(b.columns) {
		col := b.columns[b.selectedCol]
		if col.HasSelectedItem() {
			return b.selectedCol, col.SelectedItem()
		}
	}
	return -1, nil
}

func editorOpenSpecificPath(path string) bool {
	return filepath.IsAbs(path) || strings.ContainsAny(path, `/\`)
}

// handleScriptStatusExpire clears the status-bar message if no newer one has
// replaced it since this expiry tick was armed.
func (b *Board) handleScriptStatusExpire(msg scriptStatusExpireMsg) (tea.Model, tea.Cmd) {
	if msg.Seq == b.scriptStatusSeq {
		b.scriptStatus = ""
	}
	return b, nil
}

// handleScriptTimer is the dispatch target for scriptTimerMsg. Re-arms any
// repeating timers via the same collectTimerCmds drain path.
func (b *Board) handleScriptTimer(msg scriptTimerMsg) (tea.Model, tea.Cmd) {
	if b.scripts == nil {
		return b, nil
	}
	if err := b.scripts.FireTimer(msg.Token); err != nil {
		_ = err
	}
	return b, b.collectTimerCmds()
}

// collectAsyncCmds drains the queue of pending background work and returns
// a tea.Batch of tea.Cmds that exec each shell command in its own goroutine
// (Bubble Tea already runs each tea.Cmd in a goroutine), then dispatches
// scriptAsyncDoneMsg{...} when the command finishes.
func (b *Board) collectAsyncCmds() tea.Cmd {
	if b.scripts == nil {
		return nil
	}
	pending := b.scripts.PendingAsync()
	if len(pending) == 0 {
		return nil
	}
	scriptDebugf("collectAsyncCmds drained=%d", len(pending))
	b.asyncInflight += len(pending)
	dir := b.cfg.Path
	timeoutMs := b.cfg.Scripting.CommandTimeoutMs
	cmds := make([]tea.Cmd, 0, len(pending))
	for _, a := range pending {
		token := a.Token
		shellCmd := a.Shell
		cmds = append(cmds, func() tea.Msg {
			scriptDebugf("async-cmd start token=%s shell=%q", token, shellCmd)
			ctx := context.Background()
			if timeoutMs > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
				defer cancel()
			}
			res, err := shellcmd.Run(ctx, dir, shellCmd)
			errStr := ""
			if err != nil {
				// Includes shellcmd.ErrTimeout, so a hung command no longer
				// pins asyncInflight forever.
				errStr = err.Error()
			}
			scriptDebugf("async-cmd done token=%s exit=%d errStr=%q outLen=%d", token, res.ExitCode, errStr, len(res.Output))
			return scriptAsyncDoneMsg{
				Token: token, Out: res.Output, ExitCode: res.ExitCode, Err: errStr,
			}
		})
	}
	return tea.Batch(cmds...)
}

// handleScriptAsyncDone routes the async result back into the Lua callback.
func (b *Board) handleScriptAsyncDone(msg scriptAsyncDoneMsg) (tea.Model, tea.Cmd) {
	scriptDebugf("handleScriptAsyncDone token=%s exit=%d err=%q outLen=%d", msg.Token, msg.ExitCode, msg.Err, len(msg.Out))
	if b.asyncInflight > 0 {
		b.asyncInflight--
	}
	if b.scripts == nil {
		scriptDebugf("handleScriptAsyncDone: scripts is nil!")
		return b, nil
	}
	if err := b.scripts.FireAsync(msg.Token, msg.Out, msg.ExitCode, msg.Err); err != nil {
		scriptDebugf("FireAsync returned err: %v", err)
	}
	// The callback may itself schedule a timer or another async job — drain
	// both queues now. The outer Update wrapper would also drain, but doing
	// it here keeps the code symmetric with handleScriptTimer.
	tCmd := b.collectTimerCmds()
	aCmd := b.collectAsyncCmds()
	switch {
	case tCmd == nil:
		return b, aCmd
	case aCmd == nil:
		return b, tCmd
	default:
		return b, tea.Batch(tCmd, aCmd)
	}
}

// handleScriptResult turns the (req, err) tuple from a Lua command/resume
// call into a tea.Cmd: open the matching UI on a yield, fire a finished msg
// on completion or error. Also drains pending timer + async work scheduled
// during execution.
func (b *Board) handleScriptResult(name string, req *script.UIRequest, err error) tea.Cmd {
	timerCmd := b.collectTimerCmds()
	asyncCmd := b.collectAsyncCmds()
	var resultCmd tea.Cmd
	if err != nil {
		b.lineApplyPending = false // command died; nothing to splice
		resultCmd = func() tea.Msg {
			return customCommandFinishedMsg{Name: name, Err: err}
		}
	} else if req == nil {
		// Coroutine finished. A line command (possibly after kbrd.ui.* yields)
		// applies its return value to the editor here — the single completion
		// chokepoint both the synchronous and resume paths funnel through.
		if b.lineApplyPending {
			b.lineApplyPending = false
			resultCmd = b.lineCommands().applyReturn()
		} else {
			resultCmd = func() tea.Msg {
				return customCommandFinishedMsg{Name: name, Err: nil}
			}
		}
	} else {
		resultCmd = b.openScriptUI(name, req)
	}
	cmds := make([]tea.Cmd, 0, 3)
	if timerCmd != nil {
		cmds = append(cmds, timerCmd)
	}
	if asyncCmd != nil {
		cmds = append(cmds, asyncCmd)
	}
	if resultCmd != nil {
		cmds = append(cmds, resultCmd)
	}
	switch len(cmds) {
	case 0:
		return nil
	case 1:
		return cmds[0]
	default:
		return tea.Batch(cmds...)
	}
}

// openScriptUI installs the appropriate UI state for a yielded UI request.
// Confirms reuse the existing Dialog primitive; pick and prompt use ScriptUI.
func (b *Board) openScriptUI(name string, req *script.UIRequest) tea.Cmd {
	switch req.Kind {
	case "pick":
		b.scriptUI.OpenPicker(name, req.Token, req.Title, req.Choices)
		return nil
	case "prompt":
		b.scriptUI.OpenPrompt(name, req.Token, req.Title, req.Default)
		return nil
	case "confirm":
		title := req.Title
		if title == "" {
			title = "Confirm?"
		}
		b.dialog.Open(DialogOptions{
			Title: title,
			Buttons: []DialogButton{
				{Label: "Yes", Kind: ButtonPrimary,
					Msg: scriptResumeMsg{Name: name, Token: req.Token, Result: true}},
				{Label: "No",
					Msg: scriptResumeMsg{Name: name, Token: req.Token, Result: false}},
			},
			DefaultIndex: 0,
		})
		return nil
	}
	// Unknown UI kind — best-effort: resume with nil so the script doesn't hang.
	return func() tea.Msg {
		return scriptResumeMsg{Name: name, Token: req.Token, Result: nil}
	}
}

// handleScriptResume re-enters a suspended coroutine with the user's answer.
// If it yields again (chained UI calls), open the next UI; if it finishes,
// emit a customCommandFinished.
func (b *Board) handleScriptResume(msg scriptResumeMsg) (tea.Model, tea.Cmd) {
	req, err := b.scripts.ResumeWith(msg.Token, msg.Result)
	return b, b.handleScriptResult(msg.Name, req, err)
}

func (a boardScriptAPI) Notify(msg, level string) {
	scriptDebugf("Notify level=%s msg=%q", level, msg)
	sev := notifySuccess
	if level == "error" {
		sev = notifyError
	}
	a.b.notifier.fire(msg, sev)
}

// resolve returns path as-is if absolute, otherwise joined against the
// board root. All kbrd.fs.* methods funnel through here so behavior is
// consistent and predictable for scripts that pass in short names.
func (a boardScriptAPI) resolve(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(a.b.cfg.Path, path)
}

func (a boardScriptAPI) FSRead(path string) (string, error) {
	data, err := os.ReadFile(a.resolve(path))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a boardScriptAPI) FSWrite(path, body string) error {
	return os.WriteFile(a.resolve(path), []byte(body), 0o644)
}

func (a boardScriptAPI) FSExists(path string) bool {
	_, err := os.Stat(a.resolve(path))
	return err == nil
}

func (a boardScriptAPI) FSMkdir(path string) error {
	return os.MkdirAll(a.resolve(path), 0o755)
}

func (a boardScriptAPI) FSGlob(pattern string) ([]string, error) {
	return filepath.Glob(a.resolve(pattern))
}

func (a boardScriptAPI) Refresh() error {
	if err := a.b.loadColumns(); err != nil {
		return err
	}
	// The calling script is still running, so this marks the transform
	// pending; drainColumnTransform applies it once the script finishes.
	a.b.applyColumnTransforms()
	a.b.git.RefreshStatsNow()
	a.b.bus.Publish(events.BoardRefresh{Reason: "command"})
	return nil
}

func (a boardScriptAPI) CreateColumn(name string) error {
	col, err := boardops.CreateColumn(a.b.cfg.Path, name)
	if err != nil {
		return err
	}
	if err := a.Refresh(); err != nil {
		return err
	}
	a.b.bus.Publish(events.ColumnCreated{Name: col.Name})
	return nil
}

// snapshotSelection captures the current (column, item) cursor position so
// Update can compare against post-update state and publish item_select /
// column_change events.
func (b *Board) snapshotSelection() (string, string) {
	if b.selectedCol < 0 || b.selectedCol >= len(b.columns) {
		return "", ""
	}
	col := b.columns[b.selectedCol]
	item := ""
	if col.HasSelectedItem() {
		item = col.SelectedItem().Name
	}
	return col.Name, item
}

// emitSelectionChanges fires column_change and item_select events when the
// position has changed since the snapshot taken at the top of Update. No-op
// if subscribers are absent — bus.Publish on an empty subscriber list is free.
func (b *Board) emitSelectionChanges(prevCol, prevItem string) {
	newCol, newItem := b.snapshotSelection()
	if newCol != prevCol {
		b.bus.Publish(events.ColumnChange{Column: newCol, Prev: prevCol})
	}
	if newItem != prevItem || newCol != prevCol {
		b.bus.Publish(events.ItemSelect{
			Item: events.ItemRef{Column: newCol, Name: newItem},
			Prev: events.ItemRef{Column: prevCol, Name: prevItem},
		})
	}
}

// column finds a loaded column by name, or nil.
func (a boardScriptAPI) column(name string) *Column {
	for _, c := range a.b.columns {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (a boardScriptAPI) MoveItem(item events.ItemRef, toColumn string) error {
	src := a.column(item.Column)
	if src == nil {
		return fmt.Errorf("source column %q not found", item.Column)
	}
	dst := a.column(toColumn)
	if dst == nil {
		return fmt.Errorf("destination column %q not found", toColumn)
	}
	if src.Virtual || dst.Virtual {
		return errVirtualColumn
	}
	if _, err := boardops.MoveItem(boardops.ColumnRef{Name: src.Name, Path: src.Path}, boardops.ColumnRef{Name: dst.Name, Path: dst.Path}, item.Name); err != nil {
		return err
	}
	a.b.bus.Publish(events.ItemMoved{
		Item: events.ItemRef{Column: src.Name, Name: item.Name},
		From: src.Name,
		To:   dst.Name,
	})
	a.b.reloadColumnAfterMutation(src)
	a.b.reloadColumnAfterMutation(dst)
	return nil
}

func (a boardScriptAPI) CreateItem(column, name string) error {
	col := a.column(column)
	if col == nil {
		return fmt.Errorf("column %q not found", column)
	}
	if col.Virtual {
		return errVirtualColumn
	}
	res, err := boardops.CreateItem(boardops.ColumnRef{Name: col.Name, Path: col.Path}, name, "")
	if err != nil {
		return err
	}
	a.b.bus.Publish(events.ItemCreated{Item: events.ItemRef{Column: col.Name, Name: res.Item.Name}})
	a.b.reloadColumnAfterMutation(col)
	col.SelectByName(res.Item.Name)
	return nil
}

func (a boardScriptAPI) ListTemplates(column string) ([]events.TemplateInfo, error) {
	col := a.column(column)
	if col == nil {
		return nil, fmt.Errorf("column %q not found", column)
	}
	if col.Virtual {
		return nil, errVirtualColumn
	}
	return boardops.ListTemplates(
		boardops.BoardContext{Root: a.b.cfg.Path, Name: a.b.cfg.BoardName},
		boardops.ColumnRef{Name: col.Name, Path: col.Path},
	)
}

func (a boardScriptAPI) CreateItemFromTemplate(column, tmplName string, values map[string]any) error {
	col := a.column(column)
	if col == nil {
		return fmt.Errorf("column %q not found", column)
	}
	if col.Virtual {
		return errVirtualColumn
	}
	// {{shell}} is never auto-run on the Lua path: dispatch with exec disabled
	// rewrites any markers to the inert note. A script that wants async work
	// calls kbrd.async.run itself.
	policy := func(body string) string {
		// boardops resolves the final filename after rendering. The disabled
		// dispatch path only needs a stable board root and inert rewrite; the
		// cardPath is used for template shell context, which is intentionally
		// unavailable when exec is disabled.
		body, _ = a.b.templateExec.dispatch("", body, a.b.cfg.Path, config.TemplateConfig{Exec: false})
		return body
	}
	res, err := boardops.CreateItemFromTemplate(
		boardops.BoardContext{Root: a.b.cfg.Path, Name: a.b.cfg.BoardName},
		boardops.ColumnRef{Name: col.Name, Path: col.Path},
		tmplName,
		values,
		policy,
	)
	if err != nil {
		return err
	}
	a.b.bus.Publish(events.ItemCreated{Item: events.ItemRef{Column: col.Name, Name: res.Item.Name}})
	a.b.reloadColumnAfterMutation(col)
	col.SelectByName(res.Item.Name)
	return nil
}

func (a boardScriptAPI) RenameItem(item events.ItemRef, newName string) error {
	col := a.column(item.Column)
	if col == nil {
		return fmt.Errorf("column %q not found", item.Column)
	}
	if col.Virtual {
		return errVirtualColumn
	}
	res, err := boardops.RenameItem(boardops.ColumnRef{Name: col.Name, Path: col.Path}, item.Name, newName)
	if err != nil {
		return err
	}
	a.b.bus.Publish(events.ItemRenamed{
		Item:    events.ItemRef{Column: col.Name, Name: res.Item.Name},
		OldName: item.Name,
	})
	a.b.reloadColumnAfterMutation(col)
	col.SelectByName(res.Item.Name)
	return nil
}

func (a boardScriptAPI) DeleteItem(item events.ItemRef) error {
	col := a.column(item.Column)
	if col == nil {
		return fmt.Errorf("column %q not found", item.Column)
	}
	if col.Virtual {
		return errVirtualColumn
	}
	if _, err := boardops.DeleteItem(boardops.ColumnRef{Name: col.Name, Path: col.Path}, item.Name); err != nil {
		return err
	}
	a.b.bus.Publish(events.ItemDeleted{Column: col.Name, Name: item.Name})
	a.b.reloadColumnAfterMutation(col)
	return nil
}

// FocusColumn moves the board's focus to the named column. It only mutates
// b.selectedCol; the Update wrapper's emitSelectionChanges diff turns the move
// into a single column_change/item_select after the script returns, so this
// deliberately publishes nothing itself.
func (a boardScriptAPI) FocusColumn(column string) error {
	for i, c := range a.b.columns {
		if c.Name == column {
			a.b.selectedCol = i
			return nil
		}
	}
	return fmt.Errorf("column %q not found", column)
}

// SelectItem focuses the named column and places its cursor on the named item.
// Like FocusColumn it only mutates selection state and leaves event emission to
// the Update wrapper's selection diff. The item is looked up first so a missing
// name returns a clear error rather than silently leaving the cursor put.
func (a boardScriptAPI) SelectItem(column, name string) error {
	for i, c := range a.b.columns {
		if c.Name != column {
			continue
		}
		for _, it := range c.Items {
			if it.Name == name {
				a.b.selectedCol = i
				c.SelectByName(name)
				// An explicit scripted selection is "show me this item", so
				// open a collapsed column for real rather than leaving it to
				// re-collapse on the next keypress.
				c.Expand()
				return nil
			}
		}
		return fmt.Errorf("item %q not found in column %q", name, column)
	}
	return fmt.Errorf("column %q not found", column)
}

// CellSet/CellClear/CellClearAll mutate the header cell registry directly. Like
// the other boardScriptAPI methods they run on the Bubble Tea goroutine and
// never call back into the host, so direct map mutation is safe. The next
// render picks up the change (a timer callback that calls CellSet thus animates).
func (a boardScriptAPI) CellSet(id int, o events.CellOpts) {
	a.b.cells.Set(Cell{ID: id, Text: o.Text, FG: o.FG, BG: o.BG, Bold: o.Bold})
}

func (a boardScriptAPI) CellClear(id int) { a.b.cells.Clear(id) }

func (a boardScriptAPI) CellClearAll() { a.b.cells.ClearAll() }

// VirtualColumnSet/Clear/ClearAll mutate the virtual-column registry directly,
// on the Bubble Tea goroutine, just like the cell methods — the next render
// shows the change.
func (a boardScriptAPI) VirtualColumnSet(id string, spec events.VirtualColumnSpec) {
	a.b.setVirtualColumn(id, spec)
}

func (a boardScriptAPI) VirtualColumnClear(id string) { a.b.clearVirtualColumn(id) }

func (a boardScriptAPI) VirtualColumnClearAll() { a.b.clearAllVirtualColumns() }

// colDir resolves a filesystem column name to its directory for the column
// config store, rejecting unknown and virtual columns (the latter have no disk
// backing).
func (a boardScriptAPI) colDir(name string) (string, error) {
	col := a.column(name)
	if col == nil {
		return "", fmt.Errorf("column %q not found", name)
	}
	if col.Virtual {
		return "", errVirtualColumn
	}
	return col.Path, nil
}

func (a boardScriptAPI) ColumnConfigGet(column, key string) (any, bool, error) {
	dir, err := a.colDir(column)
	if err != nil {
		return nil, false, err
	}
	return boardops.ColumnConfigGet(boardops.ColumnRef{Name: column, Path: dir}, key)
}

func (a boardScriptAPI) ColumnConfigSet(column, key string, value any) error {
	dir, err := a.colDir(column)
	if err != nil {
		return err
	}
	return boardops.ColumnConfigSet(boardops.ColumnRef{Name: column, Path: dir}, key, value)
}

func (a boardScriptAPI) ColumnConfigAll(column string) (map[string]any, error) {
	dir, err := a.colDir(column)
	if err != nil {
		return nil, err
	}
	return boardops.ColumnConfigAll(boardops.ColumnRef{Name: column, Path: dir})
}

func (a boardScriptAPI) ColumnConfigDelete(column, key string) error {
	dir, err := a.colDir(column)
	if err != nil {
		return err
	}
	return boardops.ColumnConfigDelete(boardops.ColumnRef{Name: column, Path: dir}, key)
}

// ColumnIndicatorSet/Clear/ClearAll mutate the per-column indicator registry
// directly, on the Bubble Tea goroutine — the next render reads it back by
// column name, so no reload or re-projection is needed (mirrors the cells).
func (a boardScriptAPI) ColumnIndicatorSet(column string, o events.ColumnIndicatorOpts) {
	a.b.indicators.set(column, colIndicator{Text: o.Text, FG: o.FG, Bold: o.Bold})
}

func (a boardScriptAPI) ColumnIndicatorClear(column string) { a.b.indicators.clear(column) }

func (a boardScriptAPI) ColumnIndicatorClearAll() { a.b.indicators.clearAll() }
