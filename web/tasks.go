package web

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"kbrd/boardops"
	"kbrd/config"
	"kbrd/events"
	kbrdfs "kbrd/fs"
	"kbrd/script"
	"kbrd/template"
)

// taskExecDisabledNote replaces a template's {{shell}} markers when a card is
// created from a template by a server task. The headless server never runs
// board-supplied shell, so markers are rendered inert rather than executed.
const taskExecDisabledNote = "> _template shell exec disabled on the web server_"

// boardTaskAPI is the events.ScriptAPI the web task scheduler hands to its Lua
// host. Unlike the TUI's boardScriptAPI it has no in-memory model: mutations go
// straight through the board package against files on disk, and each one is
// committed (and pushed) via the Syncer so a card a task creates propagates to
// other clones. Presentation and navigation capabilities are intentionally not
// implemented in this headless host; Notify logs.
//
// Only one goroutine — the taskScheduler's — ever calls these, mirroring the
// Lua host's single-threaded contract.
type boardTaskAPI struct {
	root      string // board directory (columns live here)
	boardName string // display name, for template VarContext
	ctx       context.Context
	sync      *Syncer // nil in no-sync mode; mutationService remains nil-safe
	mutations mutationService
	logger    *log.Logger
}

// mutate keeps a task's filesystem change and its git commit under the same
// lock used by HTTP handlers and the pull loop. A nil Syncer still runs the
// mutation, matching the rest of the web layer's no-sync behavior.
func (a boardTaskAPI) mutate(msg string, fn func() error) error {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.mutations.run(ctx, a.sync, msg, fn)
}

// resolvePath joins a board-relative path against the root; absolute paths pass
// through. Mirrors boardScriptAPI.resolve so kbrd.fs.* behaves identically.
func (a boardTaskAPI) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(a.root, path)
}

func (a boardTaskAPI) Notify(msg, level string) {
	logf(a.logger, "web: task notify [%s] %s", level, msg)
}

func (a boardTaskAPI) MoveItem(item events.ItemRef, toColumn string) error {
	return a.mutate(fmt.Sprintf("kbrd: move %s/%s → %s", item.Column, item.Name, toColumn), func() error {
		src, err := boardops.ResolveColumn(a.root, item.Column, false)
		if err != nil {
			return err
		}
		dst, err := boardops.ResolveColumn(a.root, toColumn, false)
		if err != nil {
			return err
		}
		_, err = boardops.MoveItem(src, dst, item.Name)
		return err
	})
}

func (a boardTaskAPI) CreateItem(column, name string) error {
	return a.mutate(fmt.Sprintf("kbrd: create %s/%s", column, name), func() error {
		col, err := boardops.ResolveColumn(a.root, column, false)
		if err != nil {
			return err
		}
		_, err = boardops.CreateItem(col, name, "")
		return err
	})
}

func (a boardTaskAPI) ListTemplates(column string) ([]events.TemplateInfo, error) {
	col, err := boardops.ResolveColumn(a.root, column, false)
	if err != nil {
		return nil, err
	}
	return boardops.ListTemplates(boardops.BoardContext{Root: a.root, Name: a.boardName}, col)
}

func (a boardTaskAPI) CreateItemFromTemplate(column, tmplName string, values map[string]any) error {
	return a.mutate(fmt.Sprintf("kbrd: create %s from template %s", column, tmplName), func() error {
		col, err := boardops.ResolveColumn(a.root, column, false)
		if err != nil {
			return err
		}
		_, err = boardops.CreateItemFromTemplate(
			boardops.BoardContext{Root: a.root, Name: a.boardName},
			col,
			tmplName,
			values,
			inertShellMarkers,
		)
		return err
	})
}

// inertShellMarkers replaces every {{shell}} marker in a freshly rendered
// template body with a disabled note, so a server task never runs board-supplied
// shell — the same posture as the TUI under `[template] exec = false`.
func inertShellMarkers(body string) string {
	for _, m := range template.ParseShellMarkers(body) {
		body = template.RewriteShellMarker(body, m.ID, taskExecDisabledNote)
	}
	return body
}

func (a boardTaskAPI) RenameItem(item events.ItemRef, newName string) error {
	return a.mutate(fmt.Sprintf("kbrd: rename %s/%s → %s", item.Column, item.Name, newName), func() error {
		col, err := boardops.ResolveColumn(a.root, item.Column, false)
		if err != nil {
			return err
		}
		_, err = boardops.RenameItem(col, item.Name, newName)
		return err
	})
}

func (a boardTaskAPI) DeleteItem(item events.ItemRef) error {
	return a.mutate(fmt.Sprintf("kbrd: delete %s/%s", item.Column, item.Name), func() error {
		col, err := boardops.ResolveColumn(a.root, item.Column, false)
		if err != nil {
			return err
		}
		_, err = boardops.DeleteItem(col, item.Name)
		return err
	})
}

func (a boardTaskAPI) FSRead(path string) (string, error) {
	data, err := os.ReadFile(a.resolvePath(path))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a boardTaskAPI) FSWrite(path, body string) error {
	return a.mutate("kbrd: write "+path, func() error {
		return kbrdfs.WriteFileAtomicDurable(a.resolvePath(path), []byte(body), 0o644)
	})
}

func (a boardTaskAPI) FSExists(path string) bool {
	_, err := os.Stat(a.resolvePath(path))
	return err == nil
}

func (a boardTaskAPI) FSMkdir(path string) error {
	return a.mutate("kbrd: mkdir "+path, func() error {
		dir := a.resolvePath(path)
		if _, err := os.Stat(dir); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		// Git cannot retain an empty directory. Mirroring boardops.CreateColumn,
		// leave a marker so a task that only creates a directory still survives
		// the commit and push this mutation performs.
		return kbrdfs.WriteNewFileNoClobberDurable(filepath.Join(dir, ".gitkeep"), nil, 0o644)
	})
}

func (a boardTaskAPI) FSGlob(pattern string) ([]string, error) {
	return filepath.Glob(a.resolvePath(pattern))
}

func (a boardTaskAPI) Refresh() error {
	// No in-memory board to reload; mutations already hit disk directly.
	return nil
}

func (a boardTaskAPI) CreateColumn(name string) error {
	return a.mutate("kbrd: create column "+name, func() error {
		_, err := boardops.CreateColumn(a.root, name)
		return err
	})
}

// colDir resolves a filesystem column name to its directory for the column
// config store. The headless server has no virtual columns, so any resolvable
// column is disk-backed.
func (a boardTaskAPI) colRef(column string) (boardops.ColumnRef, error) {
	return boardops.ResolveColumn(a.root, column, false)
}

func (a boardTaskAPI) ColumnConfigGet(column, key string) (any, bool, error) {
	col, err := a.colRef(column)
	if err != nil {
		return nil, false, err
	}
	return boardops.ColumnConfigGet(col, key)
}

func (a boardTaskAPI) ColumnConfigSet(column, key string, value any) error {
	return a.mutate(fmt.Sprintf("kbrd: set column config %s.%s", column, key), func() error {
		col, err := a.colRef(column)
		if err != nil {
			return err
		}
		return boardops.ColumnConfigSet(col, key, value)
	})
}

func (a boardTaskAPI) ColumnConfigAll(column string) (map[string]any, error) {
	col, err := a.colRef(column)
	if err != nil {
		return nil, err
	}
	return boardops.ColumnConfigAll(col)
}

func (a boardTaskAPI) ColumnConfigDelete(column, key string) error {
	return a.mutate(fmt.Sprintf("kbrd: delete column config %s.%s", column, key), func() error {
		col, err := a.colRef(column)
		if err != nil {
			return err
		}
		return boardops.ColumnConfigDelete(col, key)
	})
}

// taskScheduler owns a Lua host and drives its timers in a headless server.
// The Lua VM is single-threaded, so exactly one goroutine (run) ever touches
// the host: time.AfterFunc callbacks only hand a fired token back over a
// channel, never call into the host themselves. HTTP middleware likewise never
// touches the host directly — it posts an httpEval over `eval` and blocks on the
// reply channel, so request evaluations serialize through run alongside timers.
type taskScheduler struct {
	host   *script.Host
	logger *log.Logger
	fire   chan string   // tokens whose timer elapsed, delivered to run
	eval   chan httpEval // request/response hook evaluations from HTTP middleware
}

// httpEval is one request- or response-hook evaluation the web middleware asks
// the scheduler goroutine to run on its behalf. reply carries the verdict back.
type httpEval struct {
	kind  string // "request" | "response"
	req   script.HTTPRequestData
	resp  script.HTTPResponseData
	reply chan httpEvalResult
}

type httpEvalResult struct {
	reqVerdict  script.HTTPRequestVerdict
	respVerdict script.HTTPResponseVerdict
}

// startTaskScheduler builds a Lua host for the board and, if any init files
// registered timers, runs them until ctx is cancelled. Returns nil (no
// scheduler) when scripting is disabled or no init files exist. The caller need
// not stop it explicitly — ctx cancellation tears it down and closes the host.
func startTaskScheduler(ctx context.Context, root, boardName, instanceName string, sc config.ScriptingConfig, sync *Syncer, logger *log.Logger) (*taskScheduler, error) {
	if !sc.Enabled {
		return nil, nil
	}
	api := boardTaskAPI{root: root, boardName: boardName, ctx: ctx, sync: sync, logger: logger}
	host, err := script.New(sc, api, nil, root, instanceName)
	if err != nil && host != nil {
		// Partial init failure: some files loaded. Log and keep the host.
		logf(logger, "web: scripting init: %v", err)
	} else if err != nil {
		return nil, err
	}
	if host == nil {
		// Scripting enabled but no init.lua / .kbrd.lua present.
		return nil, nil
	}
	ts := &taskScheduler{
		host:   host,
		logger: logger,
		fire:   make(chan string, 16),
		// Unbuffered: a request blocks here until run is free, which is the
		// single-threaded-VM serialization contract made explicit.
		eval: make(chan httpEval),
	}
	go ts.run(ctx)
	return ts, nil
}

// HasHook reports whether the host has a hook registered for the named event.
// Nil-safe so the middleware can call it whether or not scripting is enabled.
func (ts *taskScheduler) HasHook(name string) bool {
	return ts != nil && ts.host.HasHook(name)
}

// EvalRequest runs the http_request hook(s) on the scheduler goroutine and
// returns the verdict. ok is false when the evaluation could not run (scripting
// off, or ctx cancelled — client disconnect / shutdown); the caller then fails
// open and lets the request through the normal chain.
func (ts *taskScheduler) EvalRequest(ctx context.Context, data script.HTTPRequestData) (script.HTTPRequestVerdict, bool) {
	if ts == nil {
		return script.HTTPRequestVerdict{}, false
	}
	reply := make(chan httpEvalResult, 1)
	select {
	case ts.eval <- httpEval{kind: "request", req: data, reply: reply}:
	case <-ctx.Done():
		return script.HTTPRequestVerdict{}, false
	}
	select {
	case res := <-reply:
		return res.reqVerdict, true
	case <-ctx.Done():
		return script.HTTPRequestVerdict{}, false
	}
}

// EvalResponse runs the http_response hook(s) on the scheduler goroutine. Same
// fail-open contract as EvalRequest.
func (ts *taskScheduler) EvalResponse(ctx context.Context, data script.HTTPResponseData) (script.HTTPResponseVerdict, bool) {
	if ts == nil {
		return script.HTTPResponseVerdict{}, false
	}
	reply := make(chan httpEvalResult, 1)
	select {
	case ts.eval <- httpEval{kind: "response", resp: data, reply: reply}:
	case <-ctx.Done():
		return script.HTTPResponseVerdict{}, false
	}
	select {
	case res := <-reply:
		return res.respVerdict, true
	case <-ctx.Done():
		return script.HTTPResponseVerdict{}, false
	}
}

// run is the single goroutine that owns the host. It arms a one-shot Go timer
// per pending schedule; when one elapses it fires the host callback and re-arms
// whatever the host re-queued (a repeating timer re-appends itself on fire, so a
// uniform one-shot loop suffices and never compounds).
func (ts *taskScheduler) run(ctx context.Context) {
	defer ts.host.Close()
	ts.arm(ctx) // schedule timers registered during init
	for {
		select {
		case <-ctx.Done():
			return
		case token := <-ts.fire:
			if err := ts.host.FireTimer(token); err != nil {
				logf(ts.logger, "web: task timer %s: %v", token, err)
			}
			ts.arm(ctx)
		case ev := <-ts.eval:
			// Runs on the same goroutine as timer fires, so a request hook
			// never re-enters the VM mid-timer; they simply queue.
			var res httpEvalResult
			switch ev.kind {
			case "request":
				res.reqVerdict = ts.host.FireHTTPRequest(ev.req)
			case "response":
				res.respVerdict = ts.host.FireHTTPResponse(ev.resp)
			}
			ev.reply <- res
		}
	}
}

// arm drains the host's pending timer schedules and starts a one-shot Go timer
// for each. The Repeat flag is irrelevant here: FireTimer re-queues a repeating
// timer when it fires, so each elapse schedules exactly one successor.
func (ts *taskScheduler) arm(ctx context.Context) {
	for _, t := range ts.host.PendingTimers() {
		token := t.Token
		time.AfterFunc(t.Duration, func() {
			select {
			case ts.fire <- token:
			case <-ctx.Done():
			}
		})
	}
}
