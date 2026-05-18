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

	L *lua.LState

	commands []luaCommand
	hooks    map[string][]*lua.LFunction

	pending  map[string]*pendingCoro
	tokenSeq int

	running  bool
	deferred []events.Event
}

type luaCommand struct {
	Name        string
	Shortcut    string
	Description string
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
// Returns a Host even on partial failure — callers should always call Close.
// nil is returned only when scripting is disabled in config.
func New(cfg config.ScriptingConfig, api events.BoardAPI, logger events.Logger, folderPath string) (*Host, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if logger == nil {
		logger = events.NopLogger{}
	}

	L := lua.NewState(lua.Options{SkipOpenLibs: false})
	h := &Host{
		cfg:     cfg,
		api:     api,
		logger:  logger,
		L:       L,
		hooks:   make(map[string][]*lua.LFunction),
		pending: make(map[string]*pendingCoro),
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

// Close releases the underlying Lua VM.
func (h *Host) Close() {
	if h == nil {
		return
	}
	if h.L != nil {
		h.L.Close()
		h.L = nil
	}
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
			Shortcut:    c.Shortcut,
			Description: c.Description,
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
	for _, c := range h.commands {
		if c.Ref == ref {
			co, _ := h.L.NewThread()
			arg := toLValue(h.L, ctx)
			req, err := h.runDuringCall(co, c.Name, c.fn, []lua.LValue{arg})
			return req, err
		}
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
		h.fireHook("git_sync_done", map[string]interface{}{
			"ok":    e.OK,
			"stage": e.Stage,
			"error": e.Err,
		})
	case events.ItemMoved:
		h.fireHook("item_moved", map[string]interface{}{
			"item": map[string]interface{}{"column": e.Item.Column, "name": e.Item.Name},
			"from": e.From,
			"to":   e.To,
		})
	case events.BoardLoad:
		h.fireHook("board_load", map[string]interface{}{})
	}
}

func (h *Host) fireHook(name string, payload map[string]interface{}) {
	fns := h.hooks[name]
	if len(fns) == 0 {
		return
	}
	// Hooks run via PCall; their bodies may publish events. Mark the host
	// as running so those events queue rather than re-entering OnEvent
	// while we're mid-invocation.
	h.running = true
	defer func() {
		h.running = false
		// Drain any events queued by hook bodies.
		pending := h.deferred
		h.deferred = nil
		for _, ev := range pending {
			h.OnEvent(ev)
		}
	}()
	for _, fn := range fns {
		if err := h.invokeHook(fn, payload); err != nil {
			h.logger.Log("error", "hook "+name, err.Error())
			h.api.Notify("hook "+name+": "+err.Error(), "error")
		}
	}
}

// invokeHook runs a hook function via PCall (no coroutine).
func (h *Host) invokeHook(fn *lua.LFunction, arg interface{}) error {
	if h.L == nil {
		return fmt.Errorf("lua VM closed")
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

	err := func() (retErr error) {
		defer func() {
			if r := recover(); r != nil {
				retErr = fmt.Errorf("lua panic: %v", r)
			}
		}()
		h.L.Push(fn)
		h.L.Push(toLValue(h.L, arg))
		return h.L.PCall(1, 0, nil)
	}()
	return err
}

func (h *Host) doFile(path string) error {
	return h.L.DoFile(path)
}
