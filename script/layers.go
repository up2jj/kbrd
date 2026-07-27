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
	cancelTimers   map[string]struct{}
	asyncCallbacks map[string]ownedFn
	pendingAsync   []AsyncCmd
	cancelAsync    map[string]struct{}
	httpCallbacks  map[string]ownedFn
	pendingHTTP    []HTTPClientRequest
	hooks          map[string][]*hookEntry
	eval           evalRegistrations
	vcols          virtualColumns
}

type evalRegistration struct {
	fn    *lua.LFunction
	usage string
}

type evalRegistrations struct {
	order  []string
	byName map[string]evalRegistration
}

func newEvalRegistrations() evalRegistrations {
	return evalRegistrations{byName: make(map[string]evalRegistration)}
}

func (r *evalRegistrations) set(name string, registration evalRegistration) {
	if r.byName == nil {
		r.byName = make(map[string]evalRegistration)
	}
	if _, exists := r.byName[name]; !exists {
		r.order = append(r.order, name)
	}
	r.byName[name] = registration
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
		cancelTimers:   make(map[string]struct{}),
		asyncCallbacks: make(map[string]ownedFn),
		cancelAsync:    make(map[string]struct{}),
		httpCallbacks:  make(map[string]ownedFn),
		hooks:          make(map[string][]*hookEntry),
		eval:           newEvalRegistrations(),
		vcols:          newVirtualColumns(),
	}

	prevOwner, prevStage, wasRunning := h.activeOwner, h.stage, h.running
	h.activeOwner, h.stage, h.running = id, stage, true
	err := h.callLayerSetup(layer)
	h.activeOwner, h.stage, h.running = prevOwner, prevStage, wasRunning
	if err != nil {
		h.drainDeferredIfIdle(wasRunning)
		return fmt.Errorf("activate layer %q: %w", id, err)
	}

	h.unloadActiveLayer()
	for token := range stage.cancelTimers {
		delete(h.timers, token)
	}
	for token := range stage.cancelAsync {
		delete(h.asyncCallbacks, token)
	}
	h.prunePendingWork()
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
	for event, entries := range stage.hooks {
		h.hooks[event] = append(h.hooks[event], entries...)
	}
	h.layerEval = stage.eval
	h.layerVCols = stage.vcols
	h.activeLayerID = id
	h.reconcileEvalRegistrations()
	h.reconcileVirtualColumns()
	h.drainDeferredIfIdle(wasRunning)
	return nil
}

func (h *Host) drainDeferredIfIdle(wasRunning bool) {
	if wasRunning {
		return
	}
	pending := h.deferred
	h.deferred = nil
	for _, ev := range pending {
		h.OnEvent(ev)
	}
}

func (h *Host) prunePendingWork() {
	h.pendingTimers = slices.DeleteFunc(h.pendingTimers, func(schedule TimerSchedule) bool {
		_, exists := h.timers[schedule.Token]
		return !exists
	})
	h.pendingAsyncCmds = slices.DeleteFunc(h.pendingAsyncCmds, func(cmd AsyncCmd) bool {
		_, exists := h.asyncCallbacks[cmd.Token]
		return !exists
	})
	h.pendingHTTPRequests = slices.DeleteFunc(h.pendingHTTPRequests, func(req HTTPClientRequest) bool {
		_, exists := h.httpCallbacks[req.Token]
		return !exists
	})
}

func (h *Host) callLayerSetup(layer layerDef) (err error) {
	timeout := time.Duration(h.cfg.CommandTimeoutMs) * time.Millisecond
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
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
	for token, callback := range h.asyncCallbacks {
		if callback.owner == old {
			delete(h.asyncCallbacks, token)
		}
	}
	for token, callback := range h.httpCallbacks {
		if callback.owner == old {
			delete(h.httpCallbacks, token)
		}
	}
	for event, entries := range h.hooks {
		entries = slices.DeleteFunc(entries, func(entry *hookEntry) bool { return entry.owner == old })
		if len(entries) == 0 {
			delete(h.hooks, event)
		} else {
			h.hooks[event] = entries
		}
	}
	for token, pending := range h.pending {
		if pending.owner == old {
			delete(h.pending, token)
		}
	}
	h.layerVCols.clearAll()
	h.layerEval = newEvalRegistrations()
	h.prunePendingWork()
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
	desired := newVirtualColumns()
	for _, id := range h.baseVCols.order {
		if _, shadowed := h.layerVCols.byID[id]; shadowed {
			continue
		}
		desired.set(id, h.baseVCols.byID[id])
	}
	for _, id := range h.layerVCols.order {
		desired.set(id, h.layerVCols.byID[id])
	}
	for _, id := range h.publishedVCols.order {
		if _, keep := desired.byID[id]; !keep && h.pres != nil {
			h.pres.VirtualColumnClear(id)
		}
	}
	for _, id := range desired.order {
		h.publishVirtualColumn(id, desired.byID[id])
	}
	h.publishedVCols = desired
}

func (h *Host) reconcileEvalRegistrations() {
	if h.evalEnv == nil {
		return
	}
	for _, name := range h.evalNames {
		h.evalEnv.RawSetString(name, lua.LNil)
	}
	h.evalNames = nil
	h.evalUsage = make(map[string]string)
	for _, name := range h.baseEval.order {
		if _, shadowed := h.layerEval.byName[name]; shadowed {
			continue
		}
		h.publishEvalRegistration(name, h.baseEval.byName[name])
	}
	for _, name := range h.layerEval.order {
		h.publishEvalRegistration(name, h.layerEval.byName[name])
	}
}

func (h *Host) publishEvalRegistration(name string, registration evalRegistration) {
	h.evalEnv.RawSetString(name, registration.fn)
	h.evalNames = append(h.evalNames, name)
	if registration.usage != "" {
		h.evalUsage[name] = registration.usage
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
