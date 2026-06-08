// Package script embeds a Lua VM (via gopher-lua) to let users extend kbrd
// beyond shell-only custom commands.
//
// The package depends only on kbrd/events and kbrd/config — never on model/ —
// so scripting can be removed by deleting its wire-up in main.go without
// touching the rest of the codebase.
package script

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	lua "github.com/yuin/gopher-lua"

	"kbrd/config"
	"kbrd/events"
)

const (
	GlobalInitFile = "init.lua"
	FolderInitFile = ".kbrd.lua"
)

// UIRequest is what a command's coroutine yields when it calls kbrd.ui.pick /
// prompt / confirm. The host hands this to the model; the model opens the
// matching UI and resumes via Host.ResumeWith(Token, result).
type UIRequest struct {
	Token   string
	Kind    string // "pick" | "prompt" | "confirm"
	Title   string
	Choices []string
	Default string
}

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
	api    events.BoardAPI
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

	// vcolFns holds the run closures for column-scoped (virtual-column) commands,
	// keyed by their dispatch ref ("vcol:<vid>:<cmdid>"). Kept separate from
	// `commands` so they never leak into the global command menu; RunVirtualCommand
	// resolves them. Cleared per-vid when a column is replaced or removed.
	vcolFns map[string]*lua.LFunction

	pending  map[string]*pendingCoro
	tokenSeq int

	running  bool
	deferred []events.Event

	timers        map[string]*timerEntry
	pendingTimers []TimerSchedule

	// pendingStatus holds messages set via kbrd.status; the model drains them
	// (PendingStatus), shows the latest in the status bar, and arms an expiry.
	pendingStatus []StatusMsg

	// asyncCallbacks holds the Lua callbacks registered via kbrd.async.run;
	// FireAsync looks them up by token and pops them after invocation.
	asyncCallbacks   map[string]*lua.LFunction
	pendingAsyncCmds []AsyncCmd

	// inTimer is set while FireTimer is on the stack — including the
	// deferred event drain that follows. It blocks scripts from scheduling
	// new timers from inside a timer callback (or from a hook triggered by
	// that callback's side effects), which would otherwise let users build
	// exponentially-growing timer pyramids by mistake.
	inTimer bool
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

type luaCommand struct {
	Name        string
	ID          string
	Description string
	Scope       string // "files" (default) | "virtual" | "all"
	Ref         string
	fn          *lua.LFunction
}

// pendingCoro keeps a suspended coroutine alive until the model resumes it
// with a UI result.
type pendingCoro struct {
	co   *lua.LState
	name string
}

// New creates a Host, loads global (~/.config/kbrd/init.lua) and folder-local
// (./.kbrd.lua) init files if present, and registers the kbrd global.
// instanceName is this process's machine-local name (used to route
// instance-scoped timers and exposed as kbrd.instance.name); pass "" when no
// name is configured.
// Returns a Host even on partial failure — callers should always call Close.
// nil is returned only when scripting is disabled in config.
func New(cfg config.ScriptingConfig, api events.BoardAPI, logger events.Logger, folderPath, instanceName string) (*Host, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if logger == nil {
		logger = events.NopLogger{}
	}

	L := lua.NewState(lua.Options{SkipOpenLibs: false})
	h := &Host{
		cfg:            cfg,
		api:            api,
		logger:         logger,
		instanceName:   instanceName,
		L:              L,
		hooks:          make(map[string][]*hookEntry),
		pending:        make(map[string]*pendingCoro),
		timers:         make(map[string]*timerEntry),
		asyncCallbacks: make(map[string]*lua.LFunction),
		vcolFns:        make(map[string]*lua.LFunction),
	}
	h.installAPI()

	globalDir, _ := os.UserConfigDir()
	candidates := []string{
		filepath.Join(globalDir, config.AppDirName, GlobalInitFile),
	}
	if folderPath != "" {
		candidates = append(candidates, filepath.Join(folderPath, FolderInitFile))
	}

	var firstErr error
	any := false
	for _, p := range candidates {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		any = true
		if err := h.doFile(p); err != nil {
			h.logger.Log("error", p, err.Error())
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", filepath.Base(p), err)
			}
		}
	}
	if !any {
		L.Close()
		return nil, nil
	}
	return h, firstErr
}

// Close releases the underlying Lua VM and drops all registered callbacks.
// After Close, the host returns nil/no-op for all operations. Safe to call
// twice. Called by initScripting before re-creating the host on board switch.
func (h *Host) Close() {
	if h == nil {
		return
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
	h.hooks = nil
	h.pending = nil
	h.timers = nil
	h.pendingTimers = nil
	h.pendingStatus = nil
	h.asyncCallbacks = nil
	h.pendingAsyncCmds = nil
	h.deferred = nil
	h.vcolFns = nil
}

// Commands returns the Lua-registered commands as config.Command values,
// suitable for merging into the existing custom-commands menu.
func (h *Host) Commands() []config.Command {
	if h == nil {
		return nil
	}
	out := make([]config.Command, 0, len(h.commands))
	for _, c := range h.commands {
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
func (h *Host) RunVirtualCommand(ref string, ctx map[string]interface{}) (*UIRequest, error) {
	if h == nil {
		return nil, nil
	}
	return h.runByRef(ref, toLValue(h.L, ctx))
}

// runByRef resolves a dispatch ref to its run closure — first the global command
// registry, then the virtual-column registry — and runs it on a fresh coroutine.
func (h *Host) runByRef(ref string, arg lua.LValue) (*UIRequest, error) {
	for _, c := range h.commands {
		if c.Ref == ref {
			co, _ := h.L.NewThread()
			return h.runDuringCall(co, c.Name, c.fn, []lua.LValue{arg})
		}
	}
	if fn, ok := h.vcolFns[ref]; ok {
		co, _ := h.L.NewThread()
		return h.runDuringCall(co, ref, fn, []lua.LValue{arg})
	}
	return nil, fmt.Errorf("unknown lua command %q", ref)
}

// ResumeWith continues a suspended coroutine with `result` as the value
// returned from kbrd.ui.pick / prompt / confirm inside Lua. Token must be
// one returned by a previous RunCommand or ResumeWith.
//
// Result types:
//   - string: pick choice or prompt text
//   - bool:   confirm answer
//   - nil:    user cancelled (pick/prompt -> nil; confirm -> false handled by caller)
func (h *Host) ResumeWith(token string, result interface{}) (*UIRequest, error) {
	if h == nil {
		return nil, nil
	}
	p, ok := h.pending[token]
	if !ok {
		return nil, fmt.Errorf("unknown token %q", token)
	}
	delete(h.pending, token)
	args := []lua.LValue{toLValue(h.L, result)}
	return h.runDuringCall(p.co, p.name, nil, args)
}

// CancelPending drops a suspended coroutine without resuming it. Used when
// the host is torn down or a board switch happens with a UI still open.
func (h *Host) CancelPending() {
	if h == nil {
		return
	}
	h.pending = make(map[string]*pendingCoro)
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
	fn, ok := h.asyncCallbacks[token]
	if !ok {
		// Cancelled or already fired — silently drop.
		return nil
	}
	delete(h.asyncCallbacks, token)

	h.running = true
	defer func() {
		h.running = false
		pending := h.deferred
		h.deferred = nil
		for _, ev := range pending {
			h.OnEvent(ev)
		}
	}()
	err := h.invokeHook(fn, map[string]interface{}{
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
	err := h.invokeHook(e.fn, map[string]interface{}{"token": token})
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
func (h *Host) runDuringCall(co *lua.LState, name string, fn *lua.LFunction, args []lua.LValue) (*UIRequest, error) {
	h.running = true
	req, err := h.driveResume(co, name, fn, args)
	// If the script yielded (req != nil), it's suspended waiting for UI —
	// stay in "running" mode so events fired by the model in between
	// (e.g. from concurrent git syncs) are also deferred until the script
	// finishes for real. The ResumeWith path will end with req == nil.
	if req != nil {
		return req, err
	}
	h.running = false
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
	if h.cfg.InstructionLimit > 0 {
		co.SetMx(h.cfg.InstructionLimit / 1000)
	}

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
		return nil, nil
	}
	// ResumeYield
	req := parseUIRequest(rets)
	if req == nil {
		// Bare yield with no recognized request — treat as a clean finish
		// rather than hanging indefinitely.
		return nil, nil
	}
	token := h.allocToken()
	req.Token = token
	h.pending[token] = &pendingCoro{co: co, name: name}
	return req, nil
}

func (h *Host) allocToken() string {
	h.tokenSeq++
	return "co-" + strconv.Itoa(h.tokenSeq)
}

// parseUIRequest decodes the table yielded by kbrd.ui.* wrappers.
func parseUIRequest(vals []lua.LValue) *UIRequest {
	if len(vals) == 0 {
		return nil
	}
	t, ok := vals[0].(*lua.LTable)
	if !ok {
		return nil
	}
	if lua.LVAsBool(t.RawGetString("_uiReq")) != true {
		return nil
	}
	req := &UIRequest{
		Kind:    lua.LVAsString(t.RawGetString("kind")),
		Title:   lua.LVAsString(t.RawGetString("title")),
		Default: lua.LVAsString(t.RawGetString("default")),
	}
	if choices, ok := t.RawGetString("choices").(*lua.LTable); ok {
		n := choices.Len()
		for i := 1; i <= n; i++ {
			req.Choices = append(req.Choices, lua.LVAsString(choices.RawGetInt(i)))
		}
	}
	return req
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
		h.fireHook(events.NameGitSyncDone, map[string]interface{}{
			"ok":    e.OK,
			"stage": e.Stage,
			"error": e.Err,
		})
	case events.ItemMoved:
		h.fireHook(events.NameItemMoved, map[string]interface{}{
			"item": map[string]interface{}{"column": e.Item.Column, "name": e.Item.Name},
			"from": e.From,
			"to":   e.To,
		})
	case events.BoardLoad:
		h.fireHook(events.NameBoardLoad, map[string]interface{}{})
	case events.BoardRefresh:
		h.fireHook(events.NameBoardRefresh, map[string]interface{}{"reason": e.Reason})
	case events.ItemSelect:
		h.fireHook(events.NameItemSelect, map[string]interface{}{
			"item": map[string]interface{}{"column": e.Item.Column, "name": e.Item.Name},
			"prev": map[string]interface{}{"column": e.Prev.Column, "name": e.Prev.Name},
		})
	case events.ColumnChange:
		h.fireHook(events.NameColumnChange, map[string]interface{}{
			"column": e.Column,
			"prev":   e.Prev,
		})
	case events.ItemOpen:
		h.fireHook(events.NameItemOpen, map[string]interface{}{
			"item": map[string]interface{}{"column": e.Item.Column, "name": e.Item.Name},
			"kind": e.Kind,
		})
	case events.ItemSaved:
		h.fireHook(events.NameItemSaved, map[string]interface{}{
			"item": map[string]interface{}{"column": e.Item.Column, "name": e.Item.Name},
			"kind": e.Kind,
		})
	case events.ItemChanged:
		h.fireHook(events.NameItemChanged, map[string]interface{}{
			"item": map[string]interface{}{"column": e.Item.Column, "name": e.Item.Name},
		})
	case events.ItemCreated:
		h.fireHook(events.NameItemCreated, map[string]interface{}{
			"item": map[string]interface{}{"column": e.Item.Column, "name": e.Item.Name},
		})
	case events.ItemRenamed:
		h.fireHook(events.NameItemRenamed, map[string]interface{}{
			"item":    map[string]interface{}{"column": e.Item.Column, "name": e.Item.Name},
			"oldName": e.OldName,
		})
	case events.ItemDeleted:
		h.fireHook(events.NameItemDeleted, map[string]interface{}{
			"column": e.Column,
			"name":   e.Name,
		})
	}
}

func (h *Host) fireHook(name string, payload map[string]interface{}) {
	entries := h.hooks[name]
	if len(entries) == 0 {
		return
	}
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
func (h *Host) invokeHook(fn *lua.LFunction, arg interface{}) error {
	_, err := h.callHook(fn, arg, 0)
	return err
}

// invokeHookValue runs a hook function via PCall and returns its single return
// value. Used by transform hooks (column_items) where the script's return is
// the result, not just a side effect.
func (h *Host) invokeHookValue(fn *lua.LFunction, arg interface{}) (lua.LValue, error) {
	return h.callHook(fn, arg, 1)
}

// callHook is the shared PCall core behind invokeHook/invokeHookValue. nret is
// 0 (fire-and-forget) or 1 (collect one return value).
func (h *Host) callHook(fn *lua.LFunction, arg interface{}, nret int) (lua.LValue, error) {
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
	if h.cfg.InstructionLimit > 0 {
		h.L.SetMx(h.cfg.InstructionLimit / 1000)
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
		h.L.Push(toLValue(h.L, arg))
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

func (h *Host) doFile(path string) error {
	return h.L.DoFile(path)
}
