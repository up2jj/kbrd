package hook

import (
	"context"
	"time"

	"kbrd/config"
	"kbrd/events"
	"kbrd/shellcmd"
)

// Dispatcher is the shared declarative-hook execution mechanism. It keeps
// hook loading, event rendering, ordering, timeouts, and shell execution out
// of both the TUI and headless command packages.
type Dispatcher struct {
	cfg     config.Config
	byEvent map[string][]config.Hook
}

// Task is a rendered hook command ready for execution.
type Task struct {
	Name  string
	Shell string
}

// Result records one hook's outcome. A non-zero ExitCode is a completed shell
// command, while Err represents rendering, startup, or timeout failures.
type Result struct {
	Name     string
	ExitCode int
	Err      error
}

// Load reads the configured global and folder-local declarative hooks and
// returns a dispatcher plus any non-fatal definition warnings.
func Load(cfg config.Config) (*Dispatcher, []config.CommandLoadWarning, error) {
	hooks, warnings, err := config.LoadHooks(cfg.Path)
	if err != nil {
		return nil, warnings, err
	}
	if len(hooks) == 0 {
		return nil, warnings, nil
	}
	return New(cfg, hooks), warnings, nil
}

// New builds a dispatcher from already-loaded hooks. It is useful to callers
// that manage reload warnings or test fixtures themselves.
func New(cfg config.Config, hooks []config.Hook) *Dispatcher {
	byEvent := make(map[string][]config.Hook, len(hooks))
	for _, h := range hooks {
		byEvent[h.Event] = append(byEvent[h.Event], h)
	}
	return &Dispatcher{cfg: cfg, byEvent: byEvent}
}

// Tasks renders the hooks matching ev, in definition order. Render failures
// are returned as results and do not stop later hooks from being prepared.
func (d *Dispatcher) Tasks(ev events.Event) ([]Task, []Result) {
	if d == nil {
		return nil, nil
	}
	eventName, vars := eventVars(d.cfg, ev)
	if eventName == "" {
		return nil, nil
	}

	tasks := make([]Task, 0, len(d.byEvent[eventName]))
	var results []Result
	for _, h := range d.byEvent[eventName] {
		rendered, err := h.Render(vars)
		if err != nil {
			results = append(results, Result{Name: h.Name, Err: err})
			continue
		}
		tasks = append(tasks, Task{Name: h.Name, Shell: rendered})
	}
	return tasks, results
}

// Dispatch synchronously runs all hooks matching ev in definition order. Each
// hook receives the configured timeout independently. Failures do not stop the
// chain; callers decide how to surface the returned results.
func (d *Dispatcher) Dispatch(ctx context.Context, ev events.Event) []Result {
	tasks, results := d.Tasks(ev)
	for _, task := range tasks {
		results = append(results, d.Execute(ctx, task))
	}
	return results
}

// Execute runs one already-rendered task with the configured timeout. TUI
// adapters use it to preserve their own scheduling while sharing all shell
// execution policy with synchronous callers.
func (d *Dispatcher) Execute(ctx context.Context, task Task) Result {
	hookCtx := ctx
	cancel := func() {}
	if d.cfg.Hooks.TimeoutMs > 0 {
		hookCtx, cancel = context.WithTimeout(ctx, time.Duration(d.cfg.Hooks.TimeoutMs)*time.Millisecond)
	}
	res, err := shellcmd.Run(hookCtx, d.cfg.Path, task.Shell)
	cancel()
	return Result{Name: task.Name, ExitCode: res.ExitCode, Err: err}
}
