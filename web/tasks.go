package web

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"kbrd/board"
	"kbrd/colstore"
	"kbrd/config"
	"kbrd/events"
	"kbrd/script"
	"kbrd/template"
)

// taskExecDisabledNote replaces a template's {{shell}} markers when a card is
// created from a template by a server task. The headless server never runs
// board-supplied shell, so markers are rendered inert rather than executed.
const taskExecDisabledNote = "> _template shell exec disabled on the web server_"

// boardTaskAPI is the events.BoardAPI the web task scheduler hands to its Lua
// host. Unlike the TUI's boardScriptAPI it has no in-memory model: mutations go
// straight through the board package against files on disk, and each one is
// committed (and pushed) via the Syncer so a card a task creates propagates to
// other clones. Presentation-only methods (header cells, virtual columns) have
// no meaning in a headless server and are no-ops; Notify logs.
//
// Only one goroutine — the taskScheduler's — ever calls these, mirroring the
// Lua host's single-threaded contract.
type boardTaskAPI struct {
	root      string  // board directory (columns live here)
	boardName string  // display name, for template VarContext
	sync      *Syncer // nil in no-sync mode; CommitPush is nil-safe
}

// commit persists a mutation. A nil Syncer (not a git repo / no remote) makes
// this a no-op, matching the rest of the web layer's no-sync behavior.
func (a boardTaskAPI) commit(msg string) error {
	return a.sync.CommitPush(msg)
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
	log.Printf("web: task notify [%s] %s", level, msg)
}

func (a boardTaskAPI) MoveItem(item events.ItemRef, toColumn string) error {
	src, err := board.ResolveColumn(a.root, item.Column, false)
	if err != nil {
		return err
	}
	dst, err := board.ResolveColumn(a.root, toColumn, false)
	if err != nil {
		return err
	}
	if err := board.MoveItem(src, dst, item.Name); err != nil {
		return err
	}
	return a.commit(fmt.Sprintf("kbrd: move %s/%s → %s", item.Column, item.Name, toColumn))
}

func (a boardTaskAPI) CreateItem(column, name string) error {
	colPath, err := board.ResolveColumn(a.root, column, false)
	if err != nil {
		return err
	}
	if _, err := board.CreateItem(colPath, name, ""); err != nil {
		return err
	}
	return a.commit(fmt.Sprintf("kbrd: create %s/%s", column, name))
}

func (a boardTaskAPI) ListTemplates(column string) ([]events.TemplateInfo, error) {
	colPath, err := board.ResolveColumn(a.root, column, false)
	if err != nil {
		return nil, err
	}
	tmpls, _, err := template.List(a.root, colPath)
	if err != nil {
		return nil, err
	}
	infos := make([]events.TemplateInfo, 0, len(tmpls))
	for _, t := range tmpls {
		infos = append(infos, events.TemplateInfo{Name: t.Name, Scope: t.Scope})
	}
	return infos, nil
}

func (a boardTaskAPI) CreateItemFromTemplate(column, tmplName string, values map[string]any) error {
	colPath, err := board.ResolveColumn(a.root, column, false)
	if err != nil {
		return err
	}
	tmpls, _, err := template.List(a.root, colPath)
	if err != nil {
		return err
	}
	var tmpl *template.Template
	for i := range tmpls {
		if tmpls[i].Name == tmplName {
			tmpl = &tmpls[i]
			break
		}
	}
	if tmpl == nil {
		return fmt.Errorf("template %q not found in column %q", tmplName, column)
	}
	name, body, err := template.Instantiate(*tmpl, board.VarContext{
		BoardPath:  a.root,
		BoardName:  a.boardName,
		ColumnPath: colPath,
		ColumnName: column,
	}, values)
	if err != nil {
		return err
	}
	body = inertShellMarkers(body)
	if _, err := board.CreateItem(colPath, name, body); err != nil {
		return err
	}
	return a.commit(fmt.Sprintf("kbrd: create %s/%s from template %s", column, name, tmplName))
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
	colPath, err := board.ResolveColumn(a.root, item.Column, false)
	if err != nil {
		return err
	}
	if err := board.RenameItem(colPath, item.Name, newName); err != nil {
		return err
	}
	return a.commit(fmt.Sprintf("kbrd: rename %s/%s → %s", item.Column, item.Name, newName))
}

func (a boardTaskAPI) DeleteItem(item events.ItemRef) error {
	colPath, err := board.ResolveColumn(a.root, item.Column, false)
	if err != nil {
		return err
	}
	if err := board.DeleteItem(colPath, item.Name); err != nil {
		return err
	}
	return a.commit(fmt.Sprintf("kbrd: delete %s/%s", item.Column, item.Name))
}

// Navigation has no meaning in a headless server (there is no cursor to move),
// so focus/select are accepted and ignored — same posture as the presentation-
// only surfaces below.
func (a boardTaskAPI) FocusColumn(string) error        { return nil }
func (a boardTaskAPI) SelectItem(string, string) error { return nil }

func (a boardTaskAPI) FSRead(path string) (string, error) {
	data, err := os.ReadFile(a.resolvePath(path))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a boardTaskAPI) FSWrite(path, body string) error {
	if err := os.WriteFile(a.resolvePath(path), []byte(body), 0o644); err != nil {
		return err
	}
	return a.commit("kbrd: write " + path)
}

func (a boardTaskAPI) FSExists(path string) bool {
	_, err := os.Stat(a.resolvePath(path))
	return err == nil
}

func (a boardTaskAPI) FSMkdir(path string) error {
	return os.MkdirAll(a.resolvePath(path), 0o755)
}

func (a boardTaskAPI) FSGlob(pattern string) ([]string, error) {
	return filepath.Glob(a.resolvePath(pattern))
}

func (a boardTaskAPI) Refresh() error {
	// No in-memory board to reload; mutations already hit disk directly.
	return nil
}

func (a boardTaskAPI) CreateColumn(name string) error {
	clean, err := board.SanitizeFolder(name)
	if err != nil {
		return err
	}
	dir := filepath.Join(a.root, clean)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("column %q already exists", name)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	// A directory alone is not a tracked change; create a .gitkeep so the empty
	// column survives a clone, then commit.
	_ = os.WriteFile(filepath.Join(dir, ".gitkeep"), nil, 0o644)
	return a.commit("kbrd: create column " + clean)
}

// Presentation-only surfaces have no meaning in a headless server.
func (a boardTaskAPI) CellSet(int, events.CellOpts)                      {}
func (a boardTaskAPI) CellClear(int)                                     {}
func (a boardTaskAPI) CellClearAll()                                     {}
func (a boardTaskAPI) VirtualColumnSet(string, events.VirtualColumnSpec) {}
func (a boardTaskAPI) VirtualColumnClear(string)                         {}
func (a boardTaskAPI) VirtualColumnClearAll()                            {}

func (a boardTaskAPI) ColumnIndicatorSet(string, events.ColumnIndicatorOpts) {}
func (a boardTaskAPI) ColumnIndicatorClear(string)                           {}
func (a boardTaskAPI) ColumnIndicatorClearAll()                              {}

// colDir resolves a filesystem column name to its directory for the column
// config store. The headless server has no virtual columns, so any resolvable
// column is disk-backed.
func (a boardTaskAPI) colDir(column string) (string, error) {
	return board.ResolveColumn(a.root, column, false)
}

func (a boardTaskAPI) ColumnConfigGet(column, key string) (interface{}, bool, error) {
	dir, err := a.colDir(column)
	if err != nil {
		return nil, false, err
	}
	s, err := colstore.Read(dir)
	if err != nil {
		return nil, false, err
	}
	v, ok := s.Get(key)
	return v, ok, nil
}

func (a boardTaskAPI) ColumnConfigSet(column, key string, value interface{}) error {
	dir, err := a.colDir(column)
	if err != nil {
		return err
	}
	return colstore.Update(dir, func(s *colstore.Store) error {
		s.Set(key, value)
		return nil
	})
}

func (a boardTaskAPI) ColumnConfigAll(column string) (map[string]interface{}, error) {
	dir, err := a.colDir(column)
	if err != nil {
		return nil, err
	}
	s, err := colstore.Read(dir)
	if err != nil {
		return nil, err
	}
	return s.All(), nil
}

func (a boardTaskAPI) ColumnConfigDelete(column, key string) error {
	dir, err := a.colDir(column)
	if err != nil {
		return err
	}
	return colstore.Update(dir, func(s *colstore.Store) error {
		s.Delete(key)
		return nil
	})
}

// taskScheduler owns a Lua host and drives its timers in a headless server.
// The Lua VM is single-threaded, so exactly one goroutine (run) ever touches
// the host: time.AfterFunc callbacks only hand a fired token back over a
// channel, never call into the host themselves. HTTP middleware likewise never
// touches the host directly — it posts an httpEval over `eval` and blocks on the
// reply channel, so request evaluations serialize through run alongside timers.
type taskScheduler struct {
	host *script.Host
	fire chan string   // tokens whose timer elapsed, delivered to run
	eval chan httpEval // request/response hook evaluations from HTTP middleware
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
func startTaskScheduler(ctx context.Context, root, boardName, instanceName string, sc config.ScriptingConfig, sync *Syncer) (*taskScheduler, error) {
	if !sc.Enabled {
		return nil, nil
	}
	api := boardTaskAPI{root: root, boardName: boardName, sync: sync}
	host, err := script.New(sc, api, nil, root, instanceName)
	if err != nil && host != nil {
		// Partial init failure: some files loaded. Log and keep the host.
		log.Printf("web: scripting init: %v", err)
	} else if err != nil {
		return nil, err
	}
	if host == nil {
		// Scripting enabled but no init.lua / .kbrd.lua present.
		return nil, nil
	}
	ts := &taskScheduler{
		host: host,
		fire: make(chan string, 16),
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
				log.Printf("web: task timer %s: %v", token, err)
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
