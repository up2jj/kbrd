// Package script embeds a Lua VM (via gopher-lua) to let users extend kbrd
// beyond shell-only custom commands.
//
// The package depends only on kbrd/events and kbrd/config — never on model/ —
// so scripting can be removed by deleting its wire-up in main.go without
// touching the rest of the codebase.
package script

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	lua "github.com/yuin/gopher-lua"

	"kbrd/config"
	"kbrd/events"
)

const (
	GlobalInitFile = "init.lua"
	FolderInitFile = ".kbrd.lua"
)

// Host owns the Lua VM and the registry of Lua-registered commands and hooks.
//
// Host is NOT safe for concurrent goroutines. It assumes a single caller
// (typically the Bubble Tea UI goroutine) — both because the Lua VM itself
// is single-threaded and because events fired while a script is running
// must be deferred to avoid re-entering the VM. The `running` flag tracks
// whether a script invocation is in progress; while it is, OnEvent enqueues
// events into `deferred` instead of firing hooks immediately. After the
// invocation returns, deferred events drain via OnEvent.
type Host struct {
	cfg    config.ScriptingConfig
	api    events.ScriptAPI
	nav    events.NavigationAPI
	pres   events.PresentationAPI
	logger events.Logger

	// instanceName identifies this running kbrd process (a machine-local name,
	// never sourced from the git-synced board config). Timers declared with an
	// `instance` option only schedule when the option matches this name, so the
	// same .kbrd.lua can route a repeating task to one box (e.g. an always-on
	// `serve`) without firing on every clone. Exposed to Lua as kbrd.instance.name.
	instanceName string

	L *lua.LState

	commands []luaCommand
	hooks    map[string][]*hookEntry

	// layers are declared by the folder-local .kbrd.lua. Calls made outside an
	// active layer remain base resources; activeOwner is set while a layer setup,
	// command, timer, or async callback is running so newly-created resources
	// inherit the correct lifecycle.
	layers        []layerDef
	layerByID     map[string]int
	activeLayerID string
	activeOwner   string
	loadingFolder bool
	stage         *layerStage

	// vcolFns holds the run closures for column-scoped (virtual-column) commands,
	// keyed by their dispatch ref ("vcol:<vid>:<cmdid>"). Kept separate from
	// `commands` so they never leak into the global command menu; RunVirtualCommand
	// resolves them. Cleared per-vid when a column is replaced or removed.
	vcolFns    map[string]ownedFn
	baseVCols  virtualColumns
	layerVCols virtualColumns

	pending  map[string]*pendingCoro
	hostID   uint64
	tokenSeq int

	running   bool
	uiAllowed bool
	deferred  []events.Event

	// emitDepth tracks how deeply fireHook is nested via the deferred-event
	// drain. A custom event (kbrd.emit) whose hook emits again recurses through
	// this drain; the depth cap (maxEmitDepth) is a runaway-loop backstop set far
	// above any real hook chain, so two scripts pinging each other terminate
	// instead of growing the Go stack without bound.
	emitDepth int

	timers        map[string]*timerEntry
	pendingTimers []TimerSchedule

	// pendingStatus holds messages set via kbrd.status; the model drains them
	// (PendingStatus), shows the latest in the status bar, and arms an expiry.
	pendingStatus []StatusMsg

	// pendingEditorOpen holds requests from kbrd.editor.open; the model drains
	// them (PendingEditorOpen) and opens the editor at the requested line.
	pendingEditorOpen []EditorOpenReq

	// asyncCallbacks holds the Lua callbacks registered via kbrd.async.run;
	// FireAsync looks them up by token and pops them after invocation.
	asyncCallbacks   map[string]ownedFn
	pendingAsyncCmds []AsyncCmd

	// httpCallbacks and pendingHTTPRequests are the outbound HTTP equivalent
	// of asyncCallbacks/pendingAsyncCmds. The scheduler performs network I/O;
	// only FireHTTP re-enters Lua on its owning goroutine.
	httpCallbacks       map[string]ownedFn
	pendingHTTPRequests []HTTPClientRequest

	jsonNull       *lua.LUserData
	jsonArrayMeta  *lua.LTable
	jsonObjectMeta *lua.LTable
	workCtx        context.Context
	workCancel     context.CancelFunc

	// inTimer is set while FireTimer is on the stack — including the
	// deferred event drain that follows. It blocks scripts from scheduling
	// new timers from inside a timer callback (or from a hook triggered by
	// that callback's side effects), which would otherwise let users build
	// exponentially-growing timer pyramids by mistake.
	inTimer bool

	// lastReturn holds the first return value of the most recently completed
	// command coroutine, recorded when driveResume reaches ResumeOK. Line
	// commands drain it via TakeReturn to learn what to splice into the editor
	// buffer. lastReturnSet is false when the script returned nothing or nil.
	// Reset at the top of every runDuringCall so a prior command's value never
	// leaks into one that returns nothing.
	lastReturn    string
	lastReturnSet bool

	// evalEnv is the environment in which Eval runs expression strings (e.g.
	// "indent(2)"). Functions registered via kbrd.register live here rather than
	// in _G, so they never collide with globals; the table's __index metamethod
	// falls back to _G so registered functions can still use string/math/kbrd.
	// evalNames tracks the registered names (in registration order) for later
	// listing — e.g. an editor expression completer.
	evalEnv   *lua.LTable
	evalNames []string
	// evalUsage holds the optional usage/signature hint for a registered name,
	// supplied via kbrd.register(name, fn, usage). Used for command-line hints.
	evalUsage map[string]string
}

// EvalCompletion is one autocomplete candidate for the editor's :lua line: a
// registered function name and its optional usage hint.
type EvalCompletion struct {
	Name  string
	Usage string
}

// InitError separates failures in the personal global init.lua from failures
// in the board's .kbrd.lua. The TUI blocks startup only for Folder errors.
type InitError struct {
	Global error
	Folder error
}

func (e *InitError) Error() string {
	switch {
	case e == nil:
		return ""
	case e.Global != nil && e.Folder != nil:
		return e.Global.Error() + "\n" + e.Folder.Error()
	case e.Folder != nil:
		return e.Folder.Error()
	default:
		return e.Global.Error()
	}
}

// InitErrors extracts scoped initialization failures from err. Older callers
// can continue treating the returned value as an ordinary error.
func InitErrors(err error) (global, folder error) {
	var initErr *InitError
	if errors.As(err, &initErr) {
		return initErr.Global, initErr.Folder
	}
	return nil, err
}

// EvalCompletions returns the registered eval functions (in registration order)
// with their usage hints, for the editor's command-line autocomplete.
func (h *Host) EvalCompletions() []EvalCompletion {
	if h == nil {
		return nil
	}
	out := make([]EvalCompletion, 0, len(h.evalNames))
	for _, n := range h.evalNames {
		out = append(out, EvalCompletion{Name: n, Usage: h.evalUsage[n]})
	}
	return out
}

// timerEntry holds a Lua callback function registered via kbrd.timer.every
// or kbrd.timer.after. Repeating timers re-enqueue themselves after firing.
// consecutiveErrors counts back-to-back failures so the host can auto-
// disable misbehaving timers (see cfg.ErrorThreshold).
type timerEntry struct {
	fn                *lua.LFunction
	interval          time.Duration
	repeat            bool
	consecutiveErrors int
	owner             string
}

// hookEntry wraps a registered hook function with its consecutive-error
// counter. A failing hook is removed from its event's slice once the
// counter reaches cfg.ErrorThreshold; the user sees a final "disabled
// after N errors" notification and the rest of the hooks keep firing.
type hookEntry struct {
	fn                *lua.LFunction
	consecutiveErrors int
}

// TimerSchedule is returned by PendingTimers and tells the model how long
// to wait before sending a scriptTimerMsg for Token.
type TimerSchedule struct {
	Token    string
	Duration time.Duration
	// Repeat tells the model to arm a wall-clock-aligned tea.Every (no
	// cumulative drift) rather than a one-shot tea.Tick.
	Repeat bool
}

// AsyncCmd describes a piece of background work the model should run on a
// worker goroutine. For v1 the work is always a shell command — bubble tea
// already runs each tea.Cmd in its own goroutine, so the only thing this
// type does is route the result back to the right Lua callback by Token.
type AsyncCmd struct {
	Token string
	Shell string
}

type ownedFn struct {
	fn    *lua.LFunction
	owner string
}

type luaCommand struct {
	Name        string
	ID          string
	Description string
	Scope       string // "files" (default) | "virtual" | "all"
	Ref         string
	fn          *lua.LFunction
	owner       string
}

// pendingCoro keeps a suspended coroutine alive until the model resumes it
// with a UI result.
type pendingCoro struct {
	co    *lua.LState
	name  string
	owner string
	kind  UIKind
}

var hostSequence atomic.Uint64

// New creates a Host, loads global (~/.config/kbrd/init.lua) and folder-local
// (./.kbrd.lua) init files if present, and registers the kbrd global.
// instanceName is this process's machine-local name (used to route
// instance-scoped timers and exposed as kbrd.instance.name); pass "" when no
// name is configured.
// Returns a Host even on partial failure — callers should always call Close.
// nil is returned only when scripting is disabled in config.
func New(cfg config.ScriptingConfig, api events.ScriptAPI, logger events.Logger, folderPath, instanceName string) (*Host, error) {
	return NewContext(context.Background(), cfg, api, logger, folderPath, instanceName)
}

// NewContext is New with cancellation propagated through initialization and
// remote module downloads.
func NewContext(ctx context.Context, cfg config.ScriptingConfig, api events.ScriptAPI, logger events.Logger, folderPath, instanceName string) (*Host, error) {
	nav, _ := api.(events.NavigationAPI)
	pres, _ := api.(events.PresentationAPI)
	return NewWithCapabilitiesContext(ctx, cfg, api, nav, pres, logger, folderPath, instanceName)
}

func NewWithCapabilities(cfg config.ScriptingConfig, api events.ScriptAPI, nav events.NavigationAPI, pres events.PresentationAPI, logger events.Logger, folderPath, instanceName string) (*Host, error) {
	return NewWithCapabilitiesContext(context.Background(), cfg, api, nav, pres, logger, folderPath, instanceName)
}

// NewWithCapabilitiesContext is NewWithCapabilities with cancellation
// propagated through initialization and remote module downloads.
func NewWithCapabilitiesContext(ctx context.Context, cfg config.ScriptingConfig, api events.ScriptAPI, nav events.NavigationAPI, pres events.PresentationAPI, logger events.Logger, folderPath, instanceName string) (*Host, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = events.NopLogger{}
	}

	L := lua.NewState(lua.Options{SkipOpenLibs: false})
	workCtx, workCancel := context.WithCancel(context.Background())
	h := &Host{
		cfg:            cfg,
		api:            api,
		nav:            nav,
		pres:           pres,
		logger:         logger,
		instanceName:   instanceName,
		L:              L,
		hooks:          make(map[string][]*hookEntry),
		layerByID:      make(map[string]int),
		pending:        make(map[string]*pendingCoro),
		hostID:         hostSequence.Add(1),
		timers:         make(map[string]*timerEntry),
		asyncCallbacks: make(map[string]ownedFn),
		httpCallbacks:  make(map[string]ownedFn),
		vcolFns:        make(map[string]ownedFn),
		baseVCols:      newVirtualColumns(),
		layerVCols:     newVirtualColumns(),
		workCtx:        workCtx,
		workCancel:     workCancel,
	}
	h.installAPI()

	// evalEnv backs kbrd.register / Host.Eval. Its __index falls back to the real
	// globals so registered functions and evaled expressions still see string,
	// math, kbrd, etc.
	h.evalEnv = L.NewTable()
	envMeta := L.NewTable()
	envMeta.RawSetString("__index", L.Get(lua.GlobalsIndex))
	L.SetMetatable(h.evalEnv, envMeta)

	globalDir, _ := os.UserConfigDir()
	candidates := []string{
		filepath.Join(globalDir, config.AppDirName, GlobalInitFile),
	}
	if folderPath != "" {
		candidates = append(candidates, filepath.Join(folderPath, FolderInitFile))
	}

	var globalErr, folderErr error
	any := false
	localOK := true
	initCtx := ctx
	var initCancel context.CancelFunc
	if cfg.InitTimeoutMs > 0 {
		initCtx, initCancel = context.WithTimeout(ctx, time.Duration(cfg.InitTimeoutMs)*time.Millisecond)
		defer initCancel()
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		any = true
		h.loadingFolder = filepath.Base(p) == FolderInitFile && folderPath != ""
		err := h.doFile(initCtx, p)
		h.loadingFolder = false
		if err != nil {
			if filepath.Base(p) == FolderInitFile {
				localOK = false
			}
			h.logger.Log("error", p, err.Error())
			scoped := fmt.Errorf("%s: %w", filepath.Base(p), err)
			if filepath.Base(p) == FolderInitFile {
				folderErr = scoped
			} else {
				globalErr = scoped
			}
		}
	}
	if !localOK {
		h.layers = nil
		h.layerByID = make(map[string]int)
	} else if len(h.layers) > 0 {
		defaultID, validationErr := h.defaultLayerID()
		if validationErr != nil {
			h.layers = nil
			h.layerByID = make(map[string]int)
		}
		activationErr := validationErr
		if activationErr == nil {
			activationErr = h.ActivateLayer(defaultID)
		}
		if activationErr != nil {
			h.logger.Log("error", FolderInitFile, activationErr.Error())
			folderErr = fmt.Errorf("%s: %w", FolderInitFile, activationErr)
		}
	}
	if !any {
		workCancel()
		L.Close()
		return nil, nil
	}
	if globalErr != nil || folderErr != nil {
		return h, &InitError{Global: globalErr, Folder: folderErr}
	}
	return h, nil
}

// Close releases the underlying Lua VM and drops all registered callbacks.
// After Close, the host returns nil/no-op for all operations. Safe to call
// twice. Called by initScripting before re-creating the host on board switch.
func (h *Host) Close() {
	if h == nil {
		return
	}
	h.CancelPending()
	if h.workCancel != nil {
		h.workCancel()
		h.workCancel = nil
	}
	if h.L != nil {
		h.L.Close()
		h.L = nil
	}
	if c, ok := h.logger.(interface{ Close() }); ok {
		c.Close()
	}
	// Drop references so any tea.Ticks still in flight find nothing to do
	// and the GC can reclaim closures/payloads promptly.
	h.commands = nil
	h.layers = nil
	h.layerByID = nil
	h.hooks = nil
	h.pending = nil
	h.timers = nil
	h.pendingTimers = nil
	h.pendingStatus = nil
	h.pendingEditorOpen = nil
	h.asyncCallbacks = nil
	h.pendingAsyncCmds = nil
	h.httpCallbacks = nil
	h.pendingHTTPRequests = nil
	h.jsonNull = nil
	h.jsonArrayMeta = nil
	h.jsonObjectMeta = nil
	h.workCtx = nil
	h.deferred = nil
	h.vcolFns = nil
	h.baseVCols = virtualColumns{}
	h.layerVCols = virtualColumns{}
	h.evalEnv = nil
	h.evalNames = nil
}

// WorkContext is cancelled when the host closes, allowing schedulers to stop
// outbound work during board switches and shutdown.
func (h *Host) WorkContext() context.Context {
	if h == nil || h.workCtx == nil {
		return context.Background()
	}
	return h.workCtx
}

// Commands returns the Lua-registered commands as config.Command values,
// suitable for merging into the existing custom-commands menu.
func (h *Host) Commands() []config.Command {
	if h == nil {
		return nil
	}
	effective := h.effectiveCommands()
	out := make([]config.Command, 0, len(effective))
	for _, c := range effective {
		out = append(out, config.Command{
			Name:        c.Name,
			ID:          c.ID,
			Description: c.Description,
			Scope:       c.Scope,
			Source:      config.SourceLua,
			LuaRef:      c.Ref,
		})
	}
	return out
}

// RunCommand starts a Lua-registered command's coroutine. Returns:
//   - (nil, nil)   if it ran to completion successfully
//   - (req, nil)   if it yielded waiting on a UI primitive
//   - (nil, err)   if it errored / timed out
//
// When a UIRequest is returned, the caller is expected to open the matching
// UI and resume the coroutine via ResumeWith.
func (h *Host) RunCommand(ref string, ctx map[string]string) (*UIRequest, error) {
	if h == nil {
		return nil, nil
	}
	return h.runByRef(ref, toLValue(h.L, ctx))
}

// RunVirtualCommand dispatches a command (global or column-scoped) against a
// virtual-column item. ctx is a structured map — typically including a nested
// `data` table plus `path`/`title`/`columnName` — converted to a Lua table so
// the script can read ctx.data, ctx.path, etc. Same return contract as
// RunCommand.
func (h *Host) RunVirtualCommand(ref string, ctx map[string]any) (*UIRequest, error) {
	if h == nil {
		return nil, nil
	}
	return h.runByRef(ref, toLValue(h.L, ctx))
}

// runByRef resolves a dispatch ref to its run closure — first the global command
// registry, then the virtual-column registry — and runs it on a fresh coroutine.
func (h *Host) runByRef(ref string, arg lua.LValue) (*UIRequest, error) {
	if len(h.pending) > 0 {
		return nil, errors.New("another scripted UI request is already active")
	}
	for _, c := range h.effectiveCommands() {
		if c.Ref == ref {
			co, _ := h.L.NewThread()
			return h.runDuringCall(co, c.Name, c.fn, []lua.LValue{arg}, c.owner)
		}
	}
	if owned, ok := h.vcolFns[ref]; ok {
		co, _ := h.L.NewThread()
		return h.runDuringCall(co, ref, owned.fn, []lua.LValue{arg}, owned.owner)
	}
	return nil, fmt.Errorf("unknown lua command %q", ref)
}

// ResumeWith continues a suspended coroutine with result. Token must be
// one returned by a previous RunCommand or ResumeWith.
//
// Result types:
//   - string: pick choice or prompt text
//   - bool:   confirm answer
//   - nil:    user cancelled (pick/prompt -> nil; confirm -> false handled by caller)
func (h *Host) ResumeWith(token string, result any) (*UIRequest, error) {
	if h == nil {
		return nil, nil
	}
	p, ok := h.pending[token]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownUIToken, token)
	}
	delete(h.pending, token)
	typed, ok := result.(UIResult)
	if !ok {
		typed = legacyUIResult(result)
	}
	args := []lua.LValue{uiResultValue(h.L, typed)}
	return h.runDuringCall(p.co, p.name, nil, args, p.owner)
}

// TakeReturn reads and clears the return value captured from the most recently
// completed command coroutine. ok is false when the command returned nothing or
// nil (line commands treat that as "leave the line unchanged"). The model calls
// this right after a line command finishes (req == nil), so the value is the one
// from the just-completed run regardless of how many UI yields preceded it.
func (h *Host) TakeReturn() (out string, ok bool) {
	if h == nil {
		return "", false
	}
	out, ok = h.lastReturn, h.lastReturnSet
	h.lastReturn = ""
	h.lastReturnSet = false
	return out, ok
}

// Eval runs a Lua expression string (e.g. "indent(2)") against the functions
// registered via kbrd.register and returns its first result coerced to a string.
// ok is false when the expression returns nothing or nil. Name lookups resolve in
// evalEnv (registered functions) with a fallback to the real globals, so an
// expression can also call string/math/etc. The host's command timeout and
// instruction limit bound the run; a Lua error or panic is returned as err.
func (h *Host) Eval(expr string) (out string, ok bool, err error) {
	return h.EvalWithContext(expr, nil)
}

// EvalWithContext is Eval with a `ctx` table injected into the eval environment
// (the editor's operand + board/file metadata for the in-editor `:lua` command).
// ctx is exposed as the global `ctx` for the duration of the call and removed
// afterward so a later bare Eval does not see stale context.
func (h *Host) EvalWithContext(expr string, evalCtx map[string]any) (out string, ok bool, err error) {
	if h == nil || h.L == nil {
		return "", false, nil
	}

	// Guard re-entrancy exactly like a command run (runDuringCall): mark the VM as
	// running so any event a registered function publishes mid-eval — kbrd.emit, or
	// a board API that bus.Publishes — is queued on h.deferred instead of firing a
	// hook that re-enters this same VM during the PCall below and corrupts its
	// state. Eval never yields for UI, so we drain unconditionally on return.
	// Registered first so this defer runs after the ctx-global restores below (LIFO),
	// meaning the drained hooks no longer see the editor's eval ctx. Preserve a prior
	// running flag in case this is ever reached from within an already-running VM.
	wasRunning := h.running
	h.running = true
	defer func() {
		if wasRunning {
			return
		}
		h.running = false
		pending := h.deferred
		h.deferred = nil
		for _, ev := range pending {
			h.OnEvent(ev)
		}
	}()

	fn, err := h.L.LoadString("return " + expr)
	if err != nil {
		return "", false, err
	}
	// Run the chunk in evalEnv so bare names like `indent` resolve to registered
	// functions (with _G as fallback via evalEnv's metatable).
	h.L.SetFEnv(fn, h.evalEnv)

	if evalCtx != nil {
		// Set ctx on the real globals (not just evalEnv) so it is visible both to
		// the eval chunk and to the registered functions it calls (whose own
		// environment resolves globals via _G). Cleared afterward so a later bare
		// Eval never sees stale context. The common operand fields are also
		// exposed as bare globals (line/lines/text) for convenience.
		prevCtx := h.L.GetGlobal("ctx")
		h.L.SetGlobal("ctx", toLValue(h.L, evalCtx))
		defer h.L.SetGlobal("ctx", prevCtx)
		for _, k := range []string{"line", "lines", "text"} {
			if v, ok := evalCtx[k]; ok {
				name := k
				prev := h.L.GetGlobal(name)
				h.L.SetGlobal(name, toLValue(h.L, v))
				defer h.L.SetGlobal(name, prev)
			}
		}
	}

	timeout := time.Duration(h.cfg.CommandTimeoutMs) * time.Millisecond
	var cancel context.CancelFunc
	ctx := context.Background()
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	h.L.SetContext(ctx)
	defer h.L.RemoveContext()

	ret := lua.LValue(lua.LNil)
	err = func() (retErr error) {
		defer func() {
			if r := recover(); r != nil {
				retErr = fmt.Errorf("lua panic: %v", r)
			}
		}()
		h.L.Push(fn)
		if err := h.L.PCall(0, 1, nil); err != nil {
			return err
		}
		ret = h.L.Get(-1)
		h.L.Pop(1)
		return nil
	}()
	if err != nil {
		return "", false, err
	}
	if ret == lua.LNil {
		return "", false, nil
	}
	return lua.LVAsString(ret), true, nil
}

// CancelPending drops a suspended coroutine without resuming it. Used when
// the host is torn down or a board switch happens with a UI still open.
func (h *Host) CancelPending() {
	if h == nil {
		return
	}
	h.pending = make(map[string]*pendingCoro)
	h.running = false
	h.uiAllowed = false
	h.activeOwner = ""
	h.deferred = nil
	h.lastReturn = ""
	h.lastReturnSet = false
}

// PendingTimers drains the queue of timer schedules accumulated since the
// last call. The model is expected to convert each into a tea.Tick (one-shot)
// or tea.Every (repeating) that produces a scriptTimerMsg{Token} when the
// duration elapses.
func (h *Host) PendingTimers() []TimerSchedule {
	if h == nil {
		return nil
	}
	out := h.pendingTimers
	h.pendingTimers = nil
	return out
}

// StatusMsg is a status-bar message set via kbrd.status. TTL is the caller's
// requested lifetime; a zero TTL means "use the model's default".
type StatusMsg struct {
	Text string
	TTL  time.Duration
}

// PendingStatus drains status-bar messages set via kbrd.status since the last
// call. The model shows the latest in the status bar and arms an expiry tick.
func (h *Host) PendingStatus() []StatusMsg {
	if h == nil {
		return nil
	}
	out := h.pendingStatus
	h.pendingStatus = nil
	return out
}

// EditorOpenReq is a request from kbrd.editor.open to open a card's editor,
// optionally at a specific line. The target is identified by Path, or by
// Column+Name; an empty target means the current selection. Line is 1-based
// (0 = top).
type EditorOpenReq struct {
	Path   string
	Column string
	Name   string
	Line   int
}

// PendingEditorOpen drains editor-open requests made via kbrd.editor.open since
// the last call. The model resolves the target and opens the editor at the line.
func (h *Host) PendingEditorOpen() []EditorOpenReq {
	if h == nil {
		return nil
	}
	out := h.pendingEditorOpen
	h.pendingEditorOpen = nil
	return out
}

// PendingAsync drains the queue of background work the script asked to be
// run on a worker goroutine. The model converts each into a tea.Cmd that
// performs the work and produces a scriptAsyncDoneMsg{Token, ...} when done.
func (h *Host) PendingAsync() []AsyncCmd {
	if h == nil {
		return nil
	}
	out := h.pendingAsyncCmds
	h.pendingAsyncCmds = nil
	return out
}

// FireAsync invokes the Lua callback registered for the given token, passing
// the result of the background work (stdout, exit code, error string). Run
// as a hook — the callback cannot use kbrd.ui.* (no coroutine context), same
// rules as timers.
func (h *Host) FireAsync(token, out string, exitCode int, errStr string) error {
	if h == nil {
		return nil
	}
	owned, ok := h.asyncCallbacks[token]
	if !ok {
		// Cancelled or already fired — silently drop.
		return nil
	}
	delete(h.asyncCallbacks, token)

	prevOwner := h.activeOwner
	h.activeOwner = owned.owner
	defer func() { h.activeOwner = prevOwner }()
	h.running = true
	defer func() {
		h.running = false
		pending := h.deferred
		h.deferred = nil
		for _, ev := range pending {
			h.OnEvent(ev)
		}
	}()
	err := h.invokeHook(owned.fn, map[string]any{
		"out":      out,
		"exitCode": exitCode,
		"error":    errStr,
	})
	if err != nil {
		h.logger.Log("error", "async "+token, err.Error())
		h.api.Notify("async: "+err.Error(), "error")
	}
	return err
}

// FireTimer is called by the model when a tea.Tick scheduled by an earlier
// PendingTimers entry fires. It invokes the timer's Lua callback (as a
// hook — no coroutine, no UI) and, if the timer is repeating, schedules
// the next tick. Unknown tokens are silently ignored, which is how cancel
// works: we just drop the timer from the map and any in-flight tick becomes
// a no-op.
func (h *Host) FireTimer(token string) error {
	if h == nil {
		return nil
	}
	e, ok := h.timers[token]
	if !ok {
		return nil
	}
	// Run as a hook — timers may not use kbrd.ui.* (no coroutine).
	prevOwner := h.activeOwner
	h.activeOwner = e.owner
	defer func() { h.activeOwner = prevOwner }()
	h.running = true
	h.inTimer = true
	defer func() {
		h.running = false
		pending := h.deferred
		h.deferred = nil
		for _, ev := range pending {
			h.OnEvent(ev)
		}
		// Reset inTimer LAST so the deferred drain above (which fires hook
		// bodies for any side-effect events) is also blocked from
		// scheduling new timers.
		h.inTimer = false
	}()
	err := h.invokeHook(e.fn, map[string]any{"token": token})
	if err != nil {
		e.consecutiveErrors++
		h.logger.Log("error", "timer "+token, err.Error())
		h.api.Notify("timer: "+err.Error(), "error")
		if h.cfg.ErrorThreshold > 0 && e.consecutiveErrors >= h.cfg.ErrorThreshold {
			delete(h.timers, token)
			h.api.Notify(fmt.Sprintf("timer disabled after %d errors", e.consecutiveErrors), "error")
			return err
		}
	} else {
		e.consecutiveErrors = 0
	}
	if e.repeat {
		// Re-arm. If the timer was cancelled during its own callback (or
		// auto-disabled above), the map entry is gone and we shouldn't
		// reschedule.
		if _, still := h.timers[token]; still {
			h.pendingTimers = append(h.pendingTimers, TimerSchedule{Token: token, Duration: e.interval, Repeat: true})
		}
	} else {
		delete(h.timers, token)
	}
	return err
}

// runDuringCall wraps driveResume with the running flag and a deferred-event
// drain. While running is true, any event published synchronously by Lua
// (e.g. via boardScriptAPI.MoveItem → bus.Publish → OnEvent) is enqueued
// instead of firing hooks immediately. Hooks firing inside a Resume on the
// same VM would corrupt VM state; deferring them is the safe choice.
func (h *Host) runDuringCall(co *lua.LState, name string, fn *lua.LFunction, args []lua.LValue, owner string) (*UIRequest, error) {
	// Clear any captured return from a previous command so a command that
	// returns nothing doesn't inherit a stale value (line-command apply path).
	h.lastReturn = ""
	h.lastReturnSet = false
	prevOwner := h.activeOwner
	prevUIAllowed := h.uiAllowed
	h.activeOwner = owner
	h.uiAllowed = true
	h.running = true
	req, err := h.driveResume(co, name, fn, args)
	// If the script yielded (req != nil), it's suspended waiting for UI —
	// stay in "running" mode so events fired by the model in between
	// (e.g. from concurrent git syncs) are also deferred until the script
	// finishes for real. The ResumeWith path will end with req == nil.
	if req != nil {
		h.activeOwner = prevOwner
		h.uiAllowed = prevUIAllowed
		return req, err
	}
	h.running = false
	h.activeOwner = prevOwner
	h.uiAllowed = prevUIAllowed
	pending := h.deferred
	h.deferred = nil
	for _, ev := range pending {
		h.OnEvent(ev)
	}
	return req, err
}

// driveResume calls L.Resume on co and turns the result into either
// completion, a UIRequest, or an error.
func (h *Host) driveResume(co *lua.LState, name string, fn *lua.LFunction, args []lua.LValue) (*UIRequest, error) {
	if h.L == nil {
		return nil, fmt.Errorf("lua VM closed")
	}

	// Each resume gets its own wall-clock budget; time spent suspended
	// waiting for the user doesn't count against the script.
	timeout := time.Duration(h.cfg.CommandTimeoutMs) * time.Millisecond
	var cancel context.CancelFunc
	ctx := context.Background()
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	co.SetContext(ctx)
	defer co.RemoveContext()
	var (
		st   lua.ResumeState
		rets []lua.LValue
		err  error
	)
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("lua panic: %v", r)
		}
	}()
	st, err, rets = h.L.Resume(co, fn, args...)

	if err != nil {
		return nil, err
	}
	switch st {
	case lua.ResumeError:
		return nil, fmt.Errorf("lua error in %s", name)
	case lua.ResumeOK:
		// Record the coroutine's first return value for the line-command apply
		// path (drained via TakeReturn). A nil/absent return means "no change".
		if len(rets) > 0 && rets[0] != lua.LNil {
			h.lastReturn = lua.LVAsString(rets[0])
			h.lastReturnSet = true
		}
		return nil, nil
	}
	// ResumeYield
	req, isUI, decodeErr := decodeUIRequest(rets)
	if decodeErr != nil {
		return nil, fmt.Errorf("invalid UI request in %s: %w", name, decodeErr)
	}
	if !isUI {
		// Bare yield with no recognized request — treat as a clean finish
		// rather than hanging indefinitely.
		return nil, nil
	}
	token := h.allocToken()
	req.Token = token
	h.pending[token] = &pendingCoro{co: co, name: name, owner: h.activeOwner, kind: req.Kind}
	return req, nil
}

func legacyUIResult(value any) UIResult {
	if value == nil {
		return UIResult{Action: "cancel", Cancelled: true}
	}
	return UIResult{Action: "submit", Submitted: true, Value: value}
}

func (h *Host) allocToken() string {
	h.tokenSeq++
	return "co-" + strconv.FormatUint(h.hostID, 10) + "-" + strconv.Itoa(h.tokenSeq)
}

// maxEmitDepth caps how deeply custom events may chain (a kbrd.emit whose hook
// emits, whose hook emits, ...). It is a safety backstop against accidental
// ping-pong loops, not a feature limit — set well beyond any sane hook chain.
const maxEmitDepth = 32

// Emit publishes a custom script event (kbrd.emit), invoking every hook
// registered via kbrd.on(name, ...). Reserved built-in names are rejected so a
// script cannot spoof an engine event.
//
// Emit is normally called from inside a running script (a command, timer, hook,
// or async callback), where h.running is true: the event is queued on h.deferred
// and fired after the current invocation returns, exactly like a bus-published
// event — never re-entering the VM mid-run. The rare not-running case (e.g. a
// top-level emit in init.lua) fires immediately.
func (h *Host) Emit(name string, data map[string]any) error {
	if h == nil {
		return nil
	}
	if name == "" {
		return fmt.Errorf("event name is required")
	}
	if events.IsReserved(name) {
		return fmt.Errorf("event name %q is reserved", name)
	}
	if h.running {
		h.deferred = append(h.deferred, events.Custom{Name: name, Data: data})
		return nil
	}
	h.OnEvent(events.Custom{Name: name, Data: data})
	return nil
}

// OnEvent implements events.Subscriber. Hooks run via PCall (not coroutine);
// they cannot use kbrd.ui.* — a yield from a hook is dropped with a log line.
//
// If a script is currently running, the event is queued and dispatched after
// the script returns. This prevents re-entering the Lua VM (which would
// corrupt the running coroutine).
func (h *Host) OnEvent(ev events.Event) {
	if h == nil {
		return
	}
	if h.running {
		h.deferred = append(h.deferred, ev)
		return
	}
	switch e := ev.(type) {
	case events.GitSyncDone:
		h.fireHook(events.NameGitSyncDone, map[string]any{
			"ok":    e.OK,
			"stage": e.Stage,
			"error": e.Err,
		})
	case events.ItemMoved:
		h.fireHook(events.NameItemMoved, map[string]any{
			"item": map[string]any{"column": e.Item.Column, "name": e.Item.Name},
			"from": e.From,
			"to":   e.To,
		})
	case events.BoardLoad:
		h.fireHook(events.NameBoardLoad, map[string]any{})
	case events.BoardRefresh:
		h.fireHook(events.NameBoardRefresh, map[string]any{"reason": e.Reason})
	case events.ItemSelect:
		h.fireHook(events.NameItemSelect, map[string]any{
			"item": map[string]any{"column": e.Item.Column, "name": e.Item.Name},
			"prev": map[string]any{"column": e.Prev.Column, "name": e.Prev.Name},
		})
	case events.ColumnChange:
		h.fireHook(events.NameColumnChange, map[string]any{
			"column": e.Column,
			"prev":   e.Prev,
		})
	case events.ItemOpen:
		h.fireHook(events.NameItemOpen, map[string]any{
			"item": map[string]any{"column": e.Item.Column, "name": e.Item.Name},
			"kind": e.Kind,
		})
	case events.ItemSaved:
		h.fireHook(events.NameItemSaved, map[string]any{
			"item": map[string]any{"column": e.Item.Column, "name": e.Item.Name},
			"kind": e.Kind,
		})
	case events.ItemChanged:
		h.fireHook(events.NameItemChanged, map[string]any{
			"item": map[string]any{"column": e.Item.Column, "name": e.Item.Name},
		})
	case events.ItemCreated:
		h.fireHook(events.NameItemCreated, map[string]any{
			"item": map[string]any{"column": e.Item.Column, "name": e.Item.Name},
		})
	case events.ItemRenamed:
		h.fireHook(events.NameItemRenamed, map[string]any{
			"item":    map[string]any{"column": e.Item.Column, "name": e.Item.Name},
			"oldName": e.OldName,
		})
	case events.ItemDeleted:
		h.fireHook(events.NameItemDeleted, map[string]any{
			"column": e.Column,
			"name":   e.Name,
		})
	case events.Custom:
		h.fireHook(e.Name, e.Data)
	}
}

func (h *Host) fireHook(name string, payload map[string]any) {
	entries := h.hooks[name]
	if len(entries) == 0 {
		return
	}
	// Runaway-loop backstop: the deferred drain below re-enters fireHook for any
	// event a hook emits, so a kbrd.emit ping-pong would recurse unbounded. Stop
	// once the nesting passes maxEmitDepth (kept incremented across the drain).
	if h.emitDepth >= maxEmitDepth {
		h.logger.Log("error", "emit "+name, "event chain too deep; dropping (possible emit loop)")
		return
	}
	h.emitDepth++
	// Hooks run via PCall; their bodies may publish events. Mark the host
	// as running so those events queue rather than re-entering OnEvent
	// while we're mid-invocation.
	h.running = true
	defer func() {
		h.running = false
		pending := h.deferred
		h.deferred = nil
		for _, ev := range pending {
			h.OnEvent(ev)
		}
		// Decrement last: nested fireHook calls in the drain above see the
		// incremented depth, so a self-feeding emit chain trips the cap.
		h.emitDepth--
	}()
	// Track which entries to drop after the iteration (we can't mutate the
	// slice mid-loop and keep behavior obvious). Indices are into entries.
	var disable []int
	for i, e := range entries {
		err := h.invokeHook(e.fn, payload)
		if err != nil {
			e.consecutiveErrors++
			h.logger.Log("error", "hook "+name, err.Error())
			h.api.Notify("hook "+name+": "+err.Error(), "error")
			if h.cfg.ErrorThreshold > 0 && e.consecutiveErrors >= h.cfg.ErrorThreshold {
				disable = append(disable, i)
			}
		} else {
			e.consecutiveErrors = 0
		}
	}
	h.pruneHooks(name, disable)
}

// pruneHooks removes the hook entries at the given (ascending) indices from the
// named event's slice, notifying the user once per disabled hook. No-op for an
// empty disable list.
func (h *Host) pruneHooks(name string, disable []int) {
	if len(disable) == 0 {
		return
	}
	entries := h.hooks[name]
	kept := make([]*hookEntry, 0, len(entries)-len(disable))
	j := 0
	for i, e := range entries {
		if j < len(disable) && disable[j] == i {
			h.api.Notify(fmt.Sprintf("hook %s disabled after %d errors", name, e.consecutiveErrors), "error")
			j++
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == 0 {
		delete(h.hooks, name)
	} else {
		h.hooks[name] = kept
	}
}

// invokeHook runs a hook function via PCall (no coroutine).
func (h *Host) invokeHook(fn *lua.LFunction, arg any) error {
	_, err := h.callHook(fn, arg, 0)
	return err
}

// invokeHookValue runs a hook function via PCall and returns its single return
// value. Used by transform hooks (column_items) where the script's return is
// the result, not just a side effect.
func (h *Host) invokeHookValue(fn *lua.LFunction, arg any) (lua.LValue, error) {
	return h.callHook(fn, arg, 1)
}

// callHook is the shared PCall core behind invokeHook/invokeHookValue. nret is
// 0 (fire-and-forget) or 1 (collect one return value).
func (h *Host) callHook(fn *lua.LFunction, arg any, nret int) (lua.LValue, error) {
	if h.L == nil {
		return lua.LNil, fmt.Errorf("lua VM closed")
	}
	return h.callHookLValue(fn, toLValue(h.L, arg), nret)
}

func (h *Host) callHookLValue(fn *lua.LFunction, arg lua.LValue, nret int) (lua.LValue, error) {
	if h.L == nil {
		return lua.LNil, fmt.Errorf("lua VM closed")
	}

	timeout := time.Duration(h.cfg.HookTimeoutMs) * time.Millisecond
	var cancel context.CancelFunc
	ctx := context.Background()
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	h.L.SetContext(ctx)
	defer h.L.RemoveContext()

	ret := lua.LValue(lua.LNil)
	err := func() (retErr error) {
		defer func() {
			if r := recover(); r != nil {
				retErr = fmt.Errorf("lua panic: %v", r)
			}
		}()
		h.L.Push(fn)
		h.L.Push(arg)
		if err := h.L.PCall(1, nret, nil); err != nil {
			return err
		}
		if nret > 0 {
			ret = h.L.Get(-1)
			h.L.Pop(1)
		}
		return nil
	}()
	return ret, err
}

func (h *Host) doFile(ctx context.Context, path string) error {
	h.L.SetContext(ctx)
	defer h.L.RemoveContext()
	return h.L.DoFile(path)
}
