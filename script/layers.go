package script

import (
	"context"
	"fmt"
	"slices"
	"time"

	lua "github.com/yuin/gopher-lua"

	"kbrd/events"
)

// LayerInfo is the user-facing metadata for one runtime layer declared in
// .kbrd.lua. Layer order matches declaration order.
type LayerInfo struct {
	ID          string
	Name        string
	Description string
	Default     bool
}

type layerDef struct {
	LayerInfo
	setup *lua.LFunction
}

type layerStage struct {
	commands       []luaCommand
	timers         map[string]*timerEntry
	pendingTimers  []TimerSchedule
	asyncCallbacks map[string]ownedFn
	pendingAsync   []AsyncCmd
	httpCallbacks  map[string]ownedFn
	pendingHTTP    []HTTPClientRequest
	vcols          virtualColumns
}

type virtualColumnState struct {
	spec events.VirtualColumnSpec
	fns  map[string]ownedFn
}

type virtualColumns struct {
	order []string
	byID  map[string]virtualColumnState
}

func newVirtualColumns() virtualColumns {
	return virtualColumns{byID: make(map[string]virtualColumnState)}
}

func (v *virtualColumns) set(id string, state virtualColumnState) {
	if v.byID == nil {
		v.byID = make(map[string]virtualColumnState)
	}
	if _, ok := v.byID[id]; !ok {
		v.order = append(v.order, id)
	}
	v.byID[id] = state
}

func (v *virtualColumns) clear(id string) {
	delete(v.byID, id)
	v.order = slices.DeleteFunc(v.order, func(candidate string) bool { return candidate == id })
}

func (v *virtualColumns) clearAll() {
	v.order = nil
	v.byID = make(map[string]virtualColumnState)
}

// Layers returns a copy of the valid layer catalog in declaration order.
func (h *Host) Layers() []LayerInfo {
	if h == nil {
		return nil
	}
	out := make([]LayerInfo, 0, len(h.layers))
	for _, layer := range h.layers {
		out = append(out, layer.LayerInfo)
	}
	return out
}

// ActiveLayer returns the selected layer, or false when the script declares no
// layers or its default setup failed.
func (h *Host) ActiveLayer() (LayerInfo, bool) {
	if h == nil || h.activeLayerID == "" {
		return LayerInfo{}, false
	}
	i, ok := h.layerByID[h.activeLayerID]
	if !ok {
		return LayerInfo{}, false
	}
	return h.layers[i].LayerInfo, true
}

func (h *Host) defaultLayerID() (string, error) {
	defaults := 0
	defaultID := ""
	for _, layer := range h.layers {
		if layer.Default {
			defaults++
			defaultID = layer.ID
		}
	}
	if defaults != 1 {
		return "", fmt.Errorf("kbrd.layer: exactly one layer must set default=true (found %d)", defaults)
	}
	return defaultID, nil
}

// ActivateLayer stages the target setup and commits it only after the setup
// succeeds. The previous layer therefore remains intact on errors.
func (h *Host) ActivateLayer(id string) error {
	if h == nil || h.L == nil {
		return nil
	}
	i, ok := h.layerByID[id]
	if !ok {
		return fmt.Errorf("unknown layer %q", id)
	}
	layer := h.layers[i]
	stage := &layerStage{
		timers:         make(map[string]*timerEntry),
		asyncCallbacks: make(map[string]ownedFn),
		httpCallbacks:  make(map[string]ownedFn),
		vcols:          newVirtualColumns(),
	}

	prevOwner, prevStage, wasRunning := h.activeOwner, h.stage, h.running
	h.activeOwner, h.stage, h.running = id, stage, true
	err := h.callLayerSetup(layer)
	h.activeOwner, h.stage, h.running = prevOwner, prevStage, wasRunning
	if !wasRunning {
		pending := h.deferred
		h.deferred = nil
		for _, ev := range pending {
			h.OnEvent(ev)
		}
	}
	if err != nil {
		return fmt.Errorf("activate layer %q: %w", id, err)
	}

	h.unloadActiveLayer()
	h.commands = append(h.commands, stage.commands...)
	for token, entry := range stage.timers {
		h.timers[token] = entry
	}
	h.pendingTimers = append(h.pendingTimers, stage.pendingTimers...)
	for token, callback := range stage.asyncCallbacks {
		h.asyncCallbacks[token] = callback
	}
	h.pendingAsyncCmds = append(h.pendingAsyncCmds, stage.pendingAsync...)
	for token, callback := range stage.httpCallbacks {
		h.httpCallbacks[token] = callback
	}
	h.pendingHTTPRequests = append(h.pendingHTTPRequests, stage.pendingHTTP...)
	h.layerVCols = stage.vcols
	h.activeLayerID = id
	h.reconcileVirtualColumns()
	return nil
}

func (h *Host) callLayerSetup(layer layerDef) (err error) {
	timeout := time.Duration(h.cfg.CommandTimeoutMs) * time.Millisecond
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if h.cfg.InstructionLimit > 0 {
		h.L.SetMx(h.cfg.InstructionLimit / 1000)
	}
	h.L.SetContext(ctx)
	defer h.L.RemoveContext()
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("lua panic: %v", recovered)
		}
	}()
	return h.L.CallByParam(lua.P{Fn: layer.setup, NRet: 0, Protect: true})
}

func (h *Host) unloadActiveLayer() {
	old := h.activeLayerID
	if old == "" {
		return
	}
	h.commands = slices.DeleteFunc(h.commands, func(c luaCommand) bool { return c.owner == old })
	for token, timer := range h.timers {
		if timer.owner == old {
			delete(h.timers, token)
		}
	}
	h.pendingTimers = slices.DeleteFunc(h.pendingTimers, func(schedule TimerSchedule) bool {
		_, exists := h.timers[schedule.Token]
		return !exists
	})
	for token, callback := range h.asyncCallbacks {
		if callback.owner == old {
			delete(h.asyncCallbacks, token)
		}
	}
	h.pendingAsyncCmds = slices.DeleteFunc(h.pendingAsyncCmds, func(cmd AsyncCmd) bool {
		_, exists := h.asyncCallbacks[cmd.Token]
		return !exists
	})
	for token, callback := range h.httpCallbacks {
		if callback.owner == old {
			delete(h.httpCallbacks, token)
		}
	}
	h.pendingHTTPRequests = slices.DeleteFunc(h.pendingHTTPRequests, func(req HTTPClientRequest) bool {
		_, exists := h.httpCallbacks[req.Token]
		return !exists
	})
	for token, pending := range h.pending {
		if pending.owner == old {
			delete(h.pending, token)
		}
	}
	h.layerVCols.clearAll()
	h.activeLayerID = ""
}

func (h *Host) effectiveCommands() []luaCommand {
	if h == nil {
		return nil
	}
	base := make([]luaCommand, 0, len(h.commands))
	active := make([]luaCommand, 0, len(h.commands))
	activeIDs := make(map[string]bool)
	for _, command := range h.commands {
		if command.owner == h.activeLayerID && h.activeLayerID != "" {
			active = append(active, command)
			activeIDs[command.ID] = true
		}
	}
	for _, command := range h.commands {
		if command.owner == "" && !activeIDs[command.ID] {
			base = append(base, command)
		}
	}
	return append(base, active...)
}

func (h *Host) reconcileVirtualColumns() {
	h.vcolFns = make(map[string]ownedFn)
	if h.pres != nil {
		h.pres.VirtualColumnClearAll()
	}
	for _, id := range h.baseVCols.order {
		if _, shadowed := h.layerVCols.byID[id]; shadowed {
			continue
		}
		h.publishVirtualColumn(id, h.baseVCols.byID[id])
	}
	for _, id := range h.layerVCols.order {
		h.publishVirtualColumn(id, h.layerVCols.byID[id])
	}
}

func (h *Host) publishVirtualColumn(id string, state virtualColumnState) {
	for ref, fn := range state.fns {
		h.vcolFns[ref] = fn
	}
	if h.pres != nil {
		h.pres.VirtualColumnSet(id, state.spec)
	}
}
