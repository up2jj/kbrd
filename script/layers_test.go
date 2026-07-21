package script

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/config"
)

func TestLayersActivateDefaultAndSwitchResources(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("base", "Base", function() kbrd.notify("base") end)
kbrd.column.set("shared", { name = "Base column", items = {} })

kbrd.layer{
  id = "work", name = "Work", description = "work context", default = true,
  setup = function()
    kbrd.command("mode", "Work command", function() kbrd.notify("work") end)
    kbrd.timer.after("1h", function() kbrd.notify("old timer") end)
    kbrd.async.run("printf work", function() kbrd.column.set("late", { name = "Late", items = {} }) end)
    kbrd.http.request({url="https://example.test/work"}, function()
      kbrd.column.set("late-http", { name = "Late HTTP", items = {} })
    end)
    kbrd.column.set("shared", { name = "Work column", items = {} })
  end,
}
kbrd.layer{
  id = "home", name = "Home",
  setup = function()
    kbrd.command("mode", "Home command", function() kbrd.notify("home") end)
    kbrd.http.request({url="https://example.test/home"}, function() end)
    kbrd.column.set("home", { name = "Home column", items = {} })
  end,
}`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	layers := h.Layers()
	if len(layers) != 2 || layers[0].ID != "work" || !layers[0].Default || layers[1].ID != "home" {
		t.Fatalf("layers = %+v", layers)
	}
	active, ok := h.ActiveLayer()
	if !ok || active.ID != "work" {
		t.Fatalf("active = %+v, %v", active, ok)
	}
	if got := commandNames(h.Commands()); !strings.Contains(got, "Base") || !strings.Contains(got, "Work command") {
		t.Fatalf("default commands = %q", got)
	}
	timers := h.PendingTimers()
	async := h.PendingAsync()
	httpRequests := h.PendingHTTP()
	if len(timers) != 1 || len(async) != 1 || len(httpRequests) != 1 {
		t.Fatalf("default pending timers=%d async=%d http=%d", len(timers), len(async), len(httpRequests))
	}
	if got := api.vcolSets[len(api.vcolSets)-1].Spec.Name; got != "Work column" {
		t.Fatalf("active shared column = %q", got)
	}

	if err := h.ActivateLayer("home"); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if got := commandNames(h.Commands()); !strings.Contains(got, "Base") || !strings.Contains(got, "Home command") || strings.Contains(got, "Work command") {
		t.Fatalf("switched commands = %q", got)
	}
	if err := h.FireTimer(timers[0].Token); err != nil {
		t.Fatalf("stale timer: %v", err)
	}
	if err := h.FireAsync(async[0].Token, "work", 0, ""); err != nil {
		t.Fatalf("stale async: %v", err)
	}
	if err := h.FireHTTP(httpRequests[0].Token, HTTPClientResult{}); err != nil {
		t.Fatalf("stale HTTP: %v", err)
	}
	if pending := h.PendingHTTP(); len(pending) != 1 || pending[0].URL != "https://example.test/home" {
		t.Fatalf("home pending HTTP = %+v", pending)
	}
	for _, set := range api.vcolSets {
		if set.Spec.Name == "Late" {
			t.Fatal("stale async callback recreated a virtual column")
		}
		if set.Spec.Name == "Late HTTP" {
			t.Fatal("stale HTTP callback recreated a virtual column")
		}
	}
	if got := api.vcolSets[len(api.vcolSets)-2].Spec.Name; got != "Base column" {
		t.Fatalf("base shadow was not restored, penultimate set = %q", got)
	}
	if got := api.vcolSets[len(api.vcolSets)-1].Spec.Name; got != "Home column" {
		t.Fatalf("home column = %q", got)
	}
}

func TestLayerSwitchFailureKeepsCurrentResources(t *testing.T) {
	dir := writeInit(t, `
kbrd.layer{ id="ok", default=true, setup=function()
  kbrd.command("mode", "Working", function() end)
end }
kbrd.layer{ id="broken", setup=function()
  kbrd.command("mode", "Broken", function() end)
  kbrd.timer.every("1h", function() end)
  error("boom")
end }`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if err := h.ActivateLayer("broken"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("switch error = %v", err)
	}
	active, _ := h.ActiveLayer()
	if active.ID != "ok" {
		t.Fatalf("active after failure = %q", active.ID)
	}
	if got := commandNames(h.Commands()); !strings.Contains(got, "Working") || strings.Contains(got, "Broken") {
		t.Fatalf("commands after failure = %q", got)
	}
	if pending := h.PendingTimers(); len(pending) != 0 {
		t.Fatalf("failed setup leaked %d timers", len(pending))
	}
}

func TestLayerValidationAndDeclarationScope(t *testing.T) {
	t.Run("exactly one default", func(t *testing.T) {
		dir := writeInit(t, `
kbrd.layer{ id="one", setup=function() end }
kbrd.layer{ id="two", setup=function() end }`)
		h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
		if h == nil {
			t.Fatal("partial host is nil")
		}
		defer h.Close()
		if err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("duplicate id", func(t *testing.T) {
		dir := writeInit(t, `
kbrd.layer{ id="same", default=true, setup=function() end }
kbrd.layer{ id="same", setup=function() end }`)
		h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
		if h == nil {
			t.Fatal("partial host is nil")
		}
		defer h.Close()
		if err == nil || !strings.Contains(err.Error(), "duplicate id") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("global init rejected", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		configHome, err := os.UserConfigDir()
		if err != nil {
			t.Fatal(err)
		}
		globalDir := filepath.Join(configHome, "kbrd")
		if err := os.MkdirAll(globalDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(globalDir, GlobalInitFile), []byte(`
kbrd.layer{ id="global", default=true, setup=function() end }`), 0o644); err != nil {
			t.Fatal(err)
		}
		h, err := New(defaultCfg(), &fakeAPI{}, nil, t.TempDir(), "")
		if h == nil {
			t.Fatal("partial host is nil")
		}
		defer h.Close()
		if err == nil || !strings.Contains(err.Error(), "only allowed while loading .kbrd.lua") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestLayerCommandShadowsAndRestoresBase(t *testing.T) {
	dir := writeInit(t, `
kbrd.command("same", "Base", function() kbrd.notify("base") end)
kbrd.layer{ id="one", default=true, setup=function()
  kbrd.command("same", "Layer", function() kbrd.notify("layer") end)
end }
kbrd.layer{ id="two", setup=function() end }`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if got := commandNames(h.Commands()); got != "Layer" {
		t.Fatalf("active command = %q", got)
	}
	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.ActivateLayer("two"); err != nil {
		t.Fatal(err)
	}
	if got := commandNames(h.Commands()); got != "Base" {
		t.Fatalf("restored command = %q", got)
	}
}

func TestLayerOwnershipPropagatesThroughCallbacks(t *testing.T) {
	dir := writeInit(t, `
kbrd.layer{ id="one", default=true, setup=function()
  kbrd.command("spawn", "Spawn", function()
    kbrd.command("late-command", "Late command", function() end)
  end)
  kbrd.timer.after("1h", function()
    kbrd.column.set("from-timer", { name="Timer", items={} })
  end)
  kbrd.async.run("printf ok", function()
    kbrd.column.set("from-async", { name="Async", items={} })
  end)
end }
kbrd.layer{ id="two", setup=function() end }`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	timer := h.PendingTimers()[0]
	async := h.PendingAsync()[0]
	if _, err := h.RunCommand(h.Commands()[0].LuaRef, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.FireTimer(timer.Token); err != nil {
		t.Fatal(err)
	}
	if err := h.FireAsync(async.Token, "ok", 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.layerVCols.byID["from-timer"]; !ok {
		t.Fatal("timer callback did not inherit layer ownership")
	}
	if _, ok := h.layerVCols.byID["from-async"]; !ok {
		t.Fatal("async callback did not inherit layer ownership")
	}
	if got := commandNames(h.Commands()); !strings.Contains(got, "Late command") {
		t.Fatalf("command callback registration = %q", got)
	}
	if err := h.ActivateLayer("two"); err != nil {
		t.Fatal(err)
	}
	if len(h.layerVCols.byID) != 0 || strings.Contains(commandNames(h.Commands()), "Late command") {
		t.Fatal("callback-created resources survived their layer")
	}
}

func commandNames(commands []config.Command) string {
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		names = append(names, command.Name)
	}
	return strings.Join(names, ",")
}
