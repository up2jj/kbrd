package model

import (
	"context"
	"path/filepath"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/board"
	"kbrd/config"
	"kbrd/events"
	"kbrd/shellcmd"
)

// hookRunner dispatches declarative YAML hooks (config.Hook) when board events
// fire. It implements events.Subscriber.
//
// Hooks run one at a time, in order, through a single FIFO queue: OnEvent
// renders the hooks matching an event and appends them; the Board drains the
// queue via collectHookCmd, running each as a tea.Cmd and starting the next
// when hookDoneMsg arrives. A "⚙ hooks" header indicator (derived in
// updateBuiltinCells) shows while the queue is non-empty. The serial queue is
// why only low-frequency "action" events are hookable (see events.IsHookable):
// a slow hook on a per-keystroke event would back the queue up indefinitely.
//
// The runner is independent of the Lua scripting subsystem — it works even when
// scripting is disabled. Its state is only touched on the Bubble Tea goroutine
// (OnEvent runs inside bus.Publish during Update; collectHookCmd and the
// hookDoneMsg handler also run there), so it needs no locking — the same
// contract as the rest of the model.
type hookRunner struct {
	byEvent   map[string][]config.Hook
	queue     []pendingHook
	running   bool
	boardPath string // for building VarContext; the runner stays free of *Board
	boardName string
}

type pendingHook struct {
	name  string // hook Name, for error reporting
	shell string // rendered command
}

// hookDoneMsg carries the result of one finished hook command back into Update,
// which clears the running flag so the next queued hook can start.
type hookDoneMsg struct {
	Name     string
	ExitCode int
	Err      string
}

// initHooks loads declarative hooks for the current board and subscribes the
// runner to the event bus. It runs independently of the Lua scripting subsystem
// (hooks work even when scripting is disabled) and MUST be called after
// initScripting, which resets b.bus and subscribes the Lua host. Idempotent:
// safe to call again on board switch (the runner is rebuilt for the new board).
func (b *Board) initHooks() {
	b.hooks = nil
	if !b.cfg.Hooks.Enabled {
		return
	}
	hooks, warnings, err := config.LoadHooks(b.cfg.Path)
	if err != nil {
		b.commandWarnings = append(b.commandWarnings,
			config.CommandLoadWarning{Source: "hooks", Message: err.Error()})
		return
	}
	b.commandWarnings = append(b.commandWarnings, warnings...)
	if len(hooks) == 0 {
		return
	}
	r := newHookRunner(b.cfg, hooks)
	b.hooks = r
	b.bus.Subscribe(r)
}

// newHookRunner builds a runner from the board's config. It depends only on
// config (which it already needs for config.Hook), not on *Board — the runner
// is a self-contained event subscriber. The Board-side glue (collectHookCmd /
// handleHookDone) reads timeout and working dir straight from b.cfg.
func newHookRunner(cfg config.Config, hooks []config.Hook) *hookRunner {
	byEvent := make(map[string][]config.Hook, len(hooks))
	for _, h := range hooks {
		byEvent[h.Event] = append(byEvent[h.Event], h)
	}
	return &hookRunner{
		byEvent:   byEvent,
		boardPath: cfg.Path,
		boardName: cfg.BoardName,
	}
}

// busy reports whether any hook is running or queued — drives the indicator.
func (r *hookRunner) busy() bool { return r != nil && (r.running || len(r.queue) > 0) }

// pending reports how many hooks are running + queued, for the indicator label.
func (r *hookRunner) pending() int {
	if r == nil {
		return 0
	}
	n := len(r.queue)
	if r.running {
		n++
	}
	return n
}

// OnEvent implements events.Subscriber: render the hooks bound to ev's event and
// append them to the FIFO queue. Runs on the Bubble Tea goroutine; it only
// mutates queue state and never blocks (the actual commands run later via
// collectHookCmd). Render failures are logged and the hook is skipped.
func (r *hookRunner) OnEvent(ev events.Event) {
	name, vars := r.hookVars(ev)
	if name == "" {
		return // not a hookable event
	}
	hooks := r.byEvent[name]
	for _, h := range hooks {
		rendered, err := h.Render(vars)
		if err != nil {
			scriptDebugf("hook %q render error: %v", h.Name, err)
			continue
		}
		r.queue = append(r.queue, pendingHook{name: h.Name, shell: rendered})
	}
}

// hookVars maps a concrete event to its name and template variables. The base
// (board / column / item) is built through board.VarContext so the names match
// what custom commands and Lua expose; event-specific extras are layered on top.
// Returns "" for any event that is not declaratively hookable.
func (r *hookRunner) hookVars(ev events.Event) (string, map[string]string) {
	itemVars := func(column, name string) map[string]string {
		colPath := filepath.Join(r.boardPath, column)
		return board.VarContext{
			BoardPath:  r.boardPath,
			BoardName:  r.boardName,
			ColumnPath: colPath,
			ColumnName: column,
			FilePath:   filepath.Join(colPath, name+".md"),
			FileName:   name,
		}.Vars()
	}
	boardVars := func() map[string]string {
		return board.VarContext{BoardPath: r.boardPath, BoardName: r.boardName}.Vars()
	}

	switch e := ev.(type) {
	case events.ItemCreated:
		return events.NameItemCreated, itemVars(e.Item.Column, e.Item.Name)
	case events.ItemOpen:
		v := itemVars(e.Item.Column, e.Item.Name)
		v["kind"] = e.Kind
		return events.NameItemOpen, v
	case events.ItemSaved:
		v := itemVars(e.Item.Column, e.Item.Name)
		v["kind"] = e.Kind
		return events.NameItemSaved, v
	case events.ItemChanged:
		return events.NameItemChanged, itemVars(e.Item.Column, e.Item.Name)
	case events.ItemMoved:
		// After the move the item lives in To; build its context there.
		v := itemVars(e.To, e.Item.Name)
		v["fromColumn"] = e.From
		v["toColumn"] = e.To
		return events.NameItemMoved, v
	case events.ItemRenamed:
		// e.Item carries the post-rename column/name.
		v := itemVars(e.Item.Column, e.Item.Name)
		v["oldName"] = e.OldName
		return events.NameItemRenamed, v
	case events.ItemDeleted:
		// The file is gone; fileName/filePath still resolve to where it was.
		return events.NameItemDeleted, itemVars(e.Column, e.Name)
	case events.ColumnCreated:
		v := boardVars()
		v["columnName"] = e.Name
		v["columnPath"] = filepath.Join(r.boardPath, e.Name)
		return events.NameColumnCreated, v
	case events.GitSyncDone:
		v := boardVars()
		v["ok"] = strconv.FormatBool(e.OK)
		v["stage"] = e.Stage
		v["error"] = e.Err
		return events.NameGitSyncDone, v
	case events.BoardLoad:
		return events.NameBoardLoad, boardVars()
	}
	return "", nil
}

// collectHookCmd starts the head of the hook queue if nothing is running. It is
// called from the Update wrapper after each message, mirroring collectAsyncCmds
// — but it dispatches exactly one hook at a time (sequential), not a batch.
func (b *Board) collectHookCmd() tea.Cmd {
	r := b.hooks
	if r == nil || r.running || len(r.queue) == 0 {
		return nil
	}
	head := r.queue[0]
	r.queue = r.queue[1:]
	r.running = true

	dir := b.cfg.Path
	timeoutMs := b.cfg.Hooks.TimeoutMs
	return func() tea.Msg {
		ctx := context.Background()
		if timeoutMs > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
			defer cancel()
		}
		res, err := shellcmd.Run(ctx, dir, head.shell)
		errStr := ""
		if err != nil {
			// Includes shellcmd.ErrTimeout.
			errStr = err.Error()
		}
		return hookDoneMsg{Name: head.name, ExitCode: res.ExitCode, Err: errStr}
	}
}

// handleHookDone clears the running flag so the next queued hook can start (the
// Update wrapper re-drains via collectHookCmd) and surfaces failures as a toast.
// A failed hook does not abort the rest of the chain.
func (b *Board) handleHookDone(msg hookDoneMsg) (tea.Model, tea.Cmd) {
	if b.hooks != nil {
		b.hooks.running = false
	}
	switch {
	case msg.Err != "":
		return b, b.notifier.Error("hook " + msg.Name + ": " + msg.Err)
	case msg.ExitCode != 0:
		return b, b.notifier.Error("hook " + msg.Name + " exited " + strconv.Itoa(msg.ExitCode))
	}
	return b, nil
}
