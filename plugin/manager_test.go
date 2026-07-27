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

	runtimePlugins, err := manager.RuntimePlugins(board)
	if err != nil || len(runtimePlugins) != 1 {
		t.Fatalf("RuntimePlugins = %+v, %v", runtimePlugins, err)
	}
	if err := os.RemoveAll(paths.ContentCache); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RuntimePlugins(board); err == nil {
		t.Fatal("RuntimePlugins succeeded with missing cache")
	}
	if synced, err := manager.Sync(t.Context(), board); err != nil || len(synced) != 1 {
		t.Fatalf("Sync = %+v, %v", synced, err)
	}

	t.Setenv("KBRD_PLUGIN_CONFIG_DIR", configRoot)
	t.Setenv("KBRD_PLUGIN_CACHE_DIR", cacheRoot)
	host, err := script.New(config.ScriptingConfig{Enabled: true, InitTimeoutMs: 2000}, nil, nil, board, "")
	if err != nil {
		t.Fatalf("script.New: %v", err)
	}
	defer host.Close()
	commands := host.Commands()
	if len(commands) != 2 {
		t.Fatalf("commands = %+v", commands)
	}
	if commands[0].ID != "acme/date-tools:plugin-date" || commands[1].ID != "board-date" {
		t.Fatalf("command ids = %q, %q", commands[0].ID, commands[1].ID)
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
  "entrypoint": "init.lua"
}`)
	writeFile(t, filepath.Join(pluginRoot, "init.lua"), `
local util = require("acme.date-tools.util")
kbrd.command("plugin-date", "Plugin date", function() return util.value end)
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
