package plugin_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/config"
	kbrdfs "kbrd/fs"
	"kbrd/plugin"
	"kbrd/script"
)

func TestManagerLocksSyncsAndLoadsBoardPlugin(t *testing.T) {
	if !kbrdfs.GitAvailable() {
		t.Skip("git unavailable")
	}
	repo := createMarketplaceRepo(t)
	configRoot := t.TempDir()
	cacheRoot := t.TempDir()
	paths := plugin.Paths{
		ConfigRoot:       configRoot,
		CacheRoot:        cacheRoot,
		RegistryFile:     filepath.Join(configRoot, "marketplaces.json"),
		MarketplaceCache: filepath.Join(cacheRoot, "marketplaces"),
		ContentCache:     filepath.Join(cacheRoot, "content"),
	}
	manager := plugin.NewManager(paths)
	marketplace, err := manager.AddMarketplace(t.Context(), repo, "")
	if err != nil {
		t.Fatalf("AddMarketplace: %v", err)
	}
	if marketplace.Name != "acme" || len(marketplace.Commit) != 40 {
		t.Fatalf("marketplace = %+v", marketplace)
	}

	board := t.TempDir()
	info, err := manager.Info(board, "acme/date-tools")
	if err != nil {
		t.Fatalf("Info before installation: %v", err)
	}
	if info.Installed != nil || info.Manifest.Version != "1.0.0" || len(info.Manifest.Commands) != 1 {
		t.Fatalf("Info before installation = %+v", info)
	}
	writeFile(t, filepath.Join(board, ".kbrd.lua"), `
local util = require("acme.date-tools.util")
kbrd.command("board-date", "Board date", function() return util.value end)
`)
	locked, err := manager.AddPlugin(t.Context(), board, "acme/date-tools")
	if err != nil {
		t.Fatalf("AddPlugin: %v", err)
	}
	if !strings.HasPrefix(locked.ContentSHA256, "sha256:") {
		t.Fatalf("digest = %q", locked.ContentSHA256)
	}
	info, err = manager.Info(board, "acme/date-tools")
	if err != nil || info.Installed == nil || info.Installed.Version != "1.0.0" {
		t.Fatalf("Info after installation = %+v, %v", info, err)
	}

	runtimePlugins, err := manager.RuntimePlugins(board)
	if err != nil || len(runtimePlugins) != 1 {
		t.Fatalf("RuntimePlugins = %+v, %v", runtimePlugins, err)
	}
	if err := os.RemoveAll(paths.ContentCache); err != nil {
		t.Fatal(err)
	}
	if err := manager.RemoveMarketplace("acme"); err != nil {
		t.Fatalf("RemoveMarketplace: %v", err)
	}
	if _, err := manager.RuntimePlugins(board); err == nil {
		t.Fatal("RuntimePlugins succeeded with missing cache")
	}
	if synced, err := manager.Sync(t.Context(), board); err != nil || len(synced) != 1 {
		t.Fatalf("Sync without registered marketplace = %+v, %v", synced, err)
	}
	writeFile(t, filepath.Join(repo, "plugins", "date-tools", "plugin.json"), `{
  "apiVersion": 1,
  "name": "date-tools",
  "version": "2.0.0",
  "description": "Date helpers",
  "entrypoint": "init.lua"
}`)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "update plugin")
	updated, err := manager.UpdatePlugin(t.Context(), board, "acme/date-tools")
	if err != nil {
		t.Fatalf("UpdatePlugin without registered marketplace: %v", err)
	}
	if updated.ID != "acme/date-tools" || updated.Version != "2.0.0" {
		t.Fatalf("UpdatePlugin = %+v", updated)
	}
	marketplaces, err := manager.Marketplaces()
	if err != nil {
		t.Fatalf("Marketplaces: %v", err)
	}
	if len(marketplaces) != 1 || marketplaces[0].Name != "acme" {
		t.Fatalf("marketplaces after UpdatePlugin = %+v", marketplaces)
	}

	t.Setenv("KBRD_PLUGIN_CONFIG_DIR", configRoot)
	t.Setenv("KBRD_PLUGIN_CACHE_DIR", cacheRoot)
	host, err := script.New(config.ScriptingConfig{Enabled: true, InitTimeoutMs: 2000}, nil, nil, board, "")
	if err != nil {
		t.Fatalf("script.New: %v", err)
	}
	defer host.Close()
	commands := host.Commands()
	if len(commands) != 3 {
		t.Fatalf("commands = %+v", commands)
	}
	if commands[0].ID != "acme/date-tools:plugin-date" || commands[1].ID != "board-date" || commands[2].ID != "acme/date-tools:layer-date" {
		t.Fatalf("command ids = %q, %q, %q", commands[0].ID, commands[1].ID, commands[2].ID)
	}
	layers := host.Layers()
	if len(layers) != 1 || layers[0].ID != "acme/date-tools:focus" || layers[0].Name != "focus" {
		t.Fatalf("layers = %+v", layers)
	}
	if active, ok := host.ActiveLayer(); !ok || active.ID != "acme/date-tools:focus" {
		t.Fatalf("active layer = %+v, %v", active, ok)
	}
	completions := host.EvalCompletions()
	if len(completions) != 1 || completions[0].Name != "plugin__acme__date_tools__layer_value" {
		t.Fatalf("eval completions = %+v", completions)
	}
	if got, ok, err := host.Eval("plugin__acme__date_tools__layer_value()"); err != nil || !ok || got != "ok" {
		t.Fatalf("plugin layer eval = %q, %v, %v", got, ok, err)
	}
}

func TestRuntimePluginsRejectsTamperedCache(t *testing.T) {
	if !kbrdfs.GitAvailable() {
		t.Skip("git unavailable")
	}
	repo := createMarketplaceRepo(t)
	root := t.TempDir()
	paths := plugin.Paths{
		ConfigRoot:       filepath.Join(root, "config"),
		CacheRoot:        filepath.Join(root, "cache"),
		RegistryFile:     filepath.Join(root, "config", "marketplaces.json"),
		MarketplaceCache: filepath.Join(root, "cache", "marketplaces"),
		ContentCache:     filepath.Join(root, "cache", "content"),
	}
	manager := plugin.NewManager(paths)
	if _, err := manager.AddMarketplace(t.Context(), repo, ""); err != nil {
		t.Fatal(err)
	}
	board := t.TempDir()
	if _, err := manager.AddPlugin(t.Context(), board, "acme/date-tools"); err != nil {
		t.Fatal(err)
	}
	runtimePlugins, err := manager.RuntimePlugins(board)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(runtimePlugins[0].Root, "util.lua"), `return {value="tampered"}`)
	if _, err := manager.RuntimePlugins(board); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("error = %v", err)
	}
}

func TestManagerInfoDoesNotExecutePlugin(t *testing.T) {
	if !kbrdfs.GitAvailable() {
		t.Skip("git unavailable")
	}
	repo := createMarketplaceRepo(t)
	writeFile(t, filepath.Join(repo, "plugins", "date-tools", "init.lua"), `error("info executed plugin")`)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "make entrypoint fail if executed")
	root := t.TempDir()
	manager := plugin.NewManager(plugin.Paths{
		ConfigRoot:       filepath.Join(root, "config"),
		CacheRoot:        filepath.Join(root, "cache"),
		RegistryFile:     filepath.Join(root, "config", "marketplaces.json"),
		MarketplaceCache: filepath.Join(root, "cache", "marketplaces"),
		ContentCache:     filepath.Join(root, "cache", "content"),
	})
	if _, err := manager.AddMarketplace(t.Context(), repo, ""); err != nil {
		t.Fatalf("AddMarketplace: %v", err)
	}
	info, err := manager.Info(t.TempDir(), "acme/date-tools")
	if err != nil || info.Manifest.Name != "date-tools" || info.Installed != nil {
		t.Fatalf("Info = %+v, %v", info, err)
	}
}

func createMarketplaceRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "marketplace.json"), `{
  "apiVersion": 1,
  "name": "acme",
  "description": "Test plugins",
  "plugins": [{"name":"date-tools","source":"plugins/date-tools"}]
}`)
	pluginRoot := filepath.Join(repo, "plugins", "date-tools")
	writeFile(t, filepath.Join(pluginRoot, "plugin.json"), `{
  "apiVersion": 1,
  "name": "date-tools",
  "version": "1.0.0",
  "description": "Date helpers",
  "entrypoint": "init.lua",
  "author": {"name":"Plugin Test","url":"https://example.invalid/team"},
  "license": "MIT",
  "homepage": "https://example.invalid/date-tools",
  "commands": ["plugin-date"],
  "hooks": ["item_saved"],
  "layers": ["focus"],
  "timers": ["refresh dates"],
  "networkAccess": true,
  "shellAccess": false,
  "readme": "README.md",
  "changelog": "CHANGELOG.md"
}`)
	writeFile(t, filepath.Join(pluginRoot, "README.md"), `# Date tools`)
	writeFile(t, filepath.Join(pluginRoot, "CHANGELOG.md"), `# Changelog`)
	writeFile(t, filepath.Join(pluginRoot, "init.lua"), `
local util = require("acme.date-tools.util")
kbrd.command("plugin-date", "Plugin date", function() return util.value end)
kbrd.layer{
  id = "focus", default = true,
  setup = function()
    if not kbrd.has_command("plugin-date") then error("plugin base command missing") end
    kbrd.command("layer-date", "Layer date", function() return util.value end)
    kbrd.register("layer_value", function() return util.value end)
  end,
}
`)
	writeFile(t, filepath.Join(pluginRoot, "util.lua"), `return {value="ok"}`)
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.name", "Plugin Test")
	runGit(t, repo, "config", "user.email", "plugin@example.invalid")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial marketplace")
	return repo
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
