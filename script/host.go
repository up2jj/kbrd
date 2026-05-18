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
	"sync"
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
// All access to the VM is serialized by mu so callers (the UI thread, event
// subscribers, etc.) cannot race.
type Host struct {
	cfg    config.ScriptingConfig
	api    events.BoardAPI
	logger events.Logger

	mu sync.Mutex
	L  *lua.LState

	commands []luaCommand
	hooks    map[string][]*lua.LFunction
}

type luaCommand struct {
	Name        string
	Shortcut    string
	Description string
	Ref         string
	fn          *lua.LFunction
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
		cfg:    cfg,
		api:    api,
		logger: logger,
		L:      L,
		hooks:  make(map[string][]*lua.LFunction),
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
		// No script files; tear down to avoid keeping a Lua VM around for nothing.
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
	h.mu.Lock()
	defer h.mu.Unlock()
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
	h.mu.Lock()
	defer h.mu.Unlock()
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

// RunCommand invokes a Lua-registered command by ref, passing ctx as a Lua
// table. Subject to the command-timeout watchdog.
func (h *Host) RunCommand(ref string, ctx map[string]string) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.commands {
		if c.Ref == ref {
			return h.invoke(c.fn, ctx, h.cfg.CommandTimeoutMs)
		}
	}
	return fmt.Errorf("unknown lua command %q", ref)
}

// OnEvent implements events.Subscriber. Translates bus events to Lua hook
// calls. Errors are logged but never propagated.
func (h *Host) OnEvent(ev events.Event) {
	if h == nil {
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
	h.mu.Lock()
	defer h.mu.Unlock()
	fns := h.hooks[name]
	if len(fns) == 0 {
		return
	}
	for _, fn := range fns {
		if err := h.invoke(fn, payload, h.cfg.HookTimeoutMs); err != nil {
			h.logger.Log("error", "hook "+name, err.Error())
			h.api.Notify("hook "+name+": "+err.Error(), "error")
		}
	}
}

// invoke calls fn with a single argument (arg as a Lua value), wrapped in
// pcall and watchdog. h.mu must be held by the caller.
func (h *Host) invoke(fn *lua.LFunction, arg interface{}, timeoutMs int) error {
	if h.L == nil {
		return fmt.Errorf("lua VM closed")
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
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
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.L.DoFile(path)
}
