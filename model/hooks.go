package model

import (
	"context"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"kbrd/config"
	"kbrd/events"
	"kbrd/hook"
)

// hookRunner dispatches declarative YAML hooks (config.Hook) when board events
// fire. It implements events.Subscriber.
//
// Hooks run one at a time, in order, through a single FIFO queue: OnEvent
// renders the hooks matching an event and appends them; the Board drains the
// queue via boardHooks.collectCmd, running each as a tea.Cmd and starting the next
// when hookDoneMsg arrives. A "⚙ hooks" header indicator (derived in
// updateBuiltinCells) shows while the queue is non-empty. The serial queue is
// why only low-frequency "action" events are hookable (see events.IsHookable):
// a slow hook on a per-keystroke event would back the queue up indefinitely.
//
// The runner is independent of the Lua scripting subsystem — it works even when
// scripting is disabled. Its state is only touched on the Bubble Tea goroutine
// (OnEvent runs inside bus.Publish during Update; collectCmd and the
// hookDoneMsg handler also run there), so it needs no locking — the same
// contract as the rest of the model.
type hookRunner struct {
	dispatcher *hook.Dispatcher
	queue      []pendingHook
	running    bool
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

type boardHooks struct {
	board *Board
}

// init loads declarative hooks for the current board and subscribes the
// runner to the event bus. It runs independently of the Lua scripting subsystem
// (hooks work even when scripting is disabled) and MUST be called after
// initScripting, which resets b.bus and subscribes the Lua host. Idempotent:
// safe to call again on board switch (the runner is rebuilt for the new board).
func (h boardHooks) init() {
	b := h.board
	b.hooks = nil
	if !b.cfg.Hooks.Enabled {
		return
	}
	dispatcher, warnings, err := hook.Load(b.cfg)
	if err != nil {
		b.commandWarnings = append(b.commandWarnings,
			config.CommandLoadWarning{Source: "hooks", Message: err.Error()})
		return
	}
	b.commandWarnings = append(b.commandWarnings, warnings...)
	if dispatcher == nil {
		return
	}
	r := newHookRunner(dispatcher)
	b.hooks = r
	b.bus.Subscribe(r)
}

// newHookRunner adapts the shared dispatcher to the TUI's asynchronous queue.
func newHookRunner(dispatcher *hook.Dispatcher) *hookRunner {
	return &hookRunner{dispatcher: dispatcher}
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
// boardHooks.collectCmd). Render failures are logged and the hook is skipped.
func (r *hookRunner) OnEvent(ev events.Event) {
	tasks, results := r.dispatcher.Tasks(ev)
	for _, result := range results {
		scriptDebugf("hook %q render error: %v", result.Name, result.Err)
	}
	for _, task := range tasks {
		r.queue = append(r.queue, pendingHook{name: task.Name, shell: task.Shell})
	}
}

// collectCmd starts the head of the hook queue if nothing is running. It is
// called from the Update wrapper after each message, mirroring collectAsyncCmds
// — but it dispatches exactly one hook at a time (sequential), not a batch.
func (h boardHooks) collectCmd() tea.Cmd {
	b := h.board
	r := b.hooks
	if r == nil || r.running || len(r.queue) == 0 {
		return nil
	}
	head := r.queue[0]
	r.queue = r.queue[1:]
	r.running = true

	return func() tea.Msg {
		res := r.dispatcher.Execute(context.Background(), hook.Task{Name: head.name, Shell: head.shell})
		errStr := ""
		if res.Err != nil {
			errStr = res.Err.Error()
		}
		return hookDoneMsg{Name: res.Name, ExitCode: res.ExitCode, Err: errStr}
	}
}

// handleDone clears the running flag so the next queued hook can start (the
// Update wrapper re-drains via collectCmd) and surfaces failures as a toast.
// A failed hook does not abort the rest of the chain.
func (h boardHooks) handleDone(msg hookDoneMsg) (tea.Model, tea.Cmd) {
	b := h.board
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
