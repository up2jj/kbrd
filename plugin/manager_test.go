package plugin_test

import (
	"bytes"
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

func TestPreviewUpdateShowsChangesWithoutMutatingActivation(t *testing.T) {
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
	locked, err := manager.AddPlugin(t.Context(), board, "acme/date-tools")
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(board, plugin.LockFile)
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	registryBefore, err := os.ReadFile(paths.RegistryFile)
	if err != nil {
		t.Fatal(err)
	}

	pluginRoot := filepath.Join(repo, "plugins", "date-tools")
	writeFile(t, filepath.Join(pluginRoot, "plugin.json"), `{
  "apiVersion": 1,
  "name": "date-tools",
  "version": "2.0.0",
  "description": "Safer date helpers",
  "entrypoint": "init.lua",
  "commands": ["plugin-date", "tomorrow"],
  "shellAccess": true
}`)
	writeFile(t, filepath.Join(pluginRoot, "init.lua"), `kbrd.command("tomorrow", "Tomorrow", function() return "tomorrow" end)`)
	writeFile(t, filepath.Join(pluginRoot, "new.lua"), `return "new"`)
	if err := os.Remove(filepath.Join(pluginRoot, "util.lua")); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "preview candidate")

	preview, err := manager.PreviewUpdate(t.Context(), board, "acme/date-tools")
	if err != nil {
		t.Fatalf("PreviewUpdate: %v", err)
	}
	if !preview.Outdated() || preview.Current.Version != "1.0.0" || preview.Candidate.Version != "2.0.0" {
		t.Fatalf("preview = %+v", preview)
	}
	manifestFields := make(map[string]bool)
	for _, change := range preview.ManifestChanges {
		manifestFields[change.Field] = true
	}
	for _, field := range []string{"version", "description", "commands", "shellAccess"} {
		if !manifestFields[field] {
			t.Errorf("manifest changes missing %q: %+v", field, preview.ManifestChanges)
		}
	}
	fileStatuses := make(map[string]string)
	for _, file := range preview.Files {
		fileStatuses[file.Path] = file.Status
	}
	for path, status := range map[string]string{
		"init.lua": "modified", "new.lua": "added", "util.lua": "removed", "plugin.json": "modified",
	} {
		if fileStatuses[path] != status {
			t.Errorf("file %s status = %q, want %q; files: %+v", path, fileStatuses[path], status, preview.Files)
		}
	}
	for _, want := range []string{"diff --git", "+return \"new\"", "-return {value=\"ok\"}"} {
		if !strings.Contains(preview.Patch, want) {
			t.Errorf("patch missing %q:\n%s", want, preview.Patch)
		}
	}
	if strings.Contains(preview.Patch, root) {
		t.Errorf("patch exposes cache staging path:\n%s", preview.Patch)
	}
	lockAfter, _ := os.ReadFile(lockPath)
	registryAfter, _ := os.ReadFile(paths.RegistryFile)
	if !bytes.Equal(lockBefore, lockAfter) {
		t.Fatal("PreviewUpdate changed the board lock")
	}
	if !bytes.Equal(registryBefore, registryAfter) {
		t.Fatal("PreviewUpdate changed the marketplace registry")
	}
	stillLocked, err := plugin.LoadBoardLock(board)
	if err != nil || len(stillLocked.Plugins) != 1 || stillLocked.Plugins[0].ContentSHA256 != locked.ContentSHA256 {
		t.Fatalf("lock after preview = %+v, %v", stillLocked, err)
	}
}

func TestPreviewUpdatesHandlesMultiplePluginsFromOneMarketplace(t *testing.T) {
	if !kbrdfs.GitAvailable() {
		t.Skip("git unavailable")
	}
	repo := createMarketplaceRepo(t)
	writeFile(t, filepath.Join(repo, "marketplace.json"), `{
  "apiVersion": 1,
  "name": "acme",
  "description": "Test plugins",
  "plugins": [
    {"name":"date-tools","source":"plugins/date-tools"},
    {"name":"text-tools","source":"plugins/text-tools"}
  ]
}`)
	textRoot := filepath.Join(repo, "plugins", "text-tools")
	writeFile(t, filepath.Join(textRoot, "plugin.json"), `{
  "apiVersion": 1,
  "name": "text-tools",
  "version": "1.0.0",
  "description": "Text helpers",
  "entrypoint": "init.lua"
}`)
	writeFile(t, filepath.Join(textRoot, "init.lua"), `return "text"`)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "add text plugin")

	root := t.TempDir()
	manager := plugin.NewManager(plugin.Paths{
		ConfigRoot:       filepath.Join(root, "config"),
		CacheRoot:        filepath.Join(root, "cache"),
		RegistryFile:     filepath.Join(root, "config", "marketplaces.json"),
		MarketplaceCache: filepath.Join(root, "cache", "marketplaces"),
		ContentCache:     filepath.Join(root, "cache", "content"),
	})
	if _, err := manager.AddMarketplace(t.Context(), repo, ""); err != nil {
		t.Fatal(err)
	}
	board := t.TempDir()
	for _, id := range []string{"acme/date-tools", "acme/text-tools"} {
		if _, err := manager.AddPlugin(t.Context(), board, id); err != nil {
			t.Fatal(err)
		}
	}

	for _, name := range []string{"date-tools", "text-tools"} {
		manifestPath := filepath.Join(repo, "plugins", name, "plugin.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, manifestPath, strings.Replace(string(data), `"version": "1.0.0"`, `"version": "2.0.0"`, 1))
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "update both plugins")

	previews, err := manager.PreviewUpdates(t.Context(), board, []string{"acme/date-tools", "acme/text-tools"})
	if err != nil {
		t.Fatalf("PreviewUpdates: %v", err)
	}
	if len(previews) != 2 {
		t.Fatalf("got %d previews, want 2", len(previews))
	}
	for _, preview := range previews {
		if !preview.Outdated() || preview.Candidate.Version != "2.0.0" {
			t.Errorf("preview = %+v", preview)
		}
	}
}

func TestUpdatePluginsLeavesLockUnchangedWhenAnyCandidateFails(t *testing.T) {
	if !kbrdfs.GitAvailable() {
		t.Skip("git unavailable")
	}
	repo, manager, board := setupTwoPluginMarketplace(t)
	lockPath := filepath.Join(board, plugin.LockFile)
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	dateManifest := filepath.Join(repo, "plugins", "date-tools", "plugin.json")
	data, err := os.ReadFile(dateManifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dateManifest, strings.Replace(string(data), `"version": "1.0.0"`, `"version": "2.0.0"`, 1))
	writeFile(t, filepath.Join(repo, "plugins", "text-tools", "plugin.json"), `{not valid json`)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "one valid and one invalid update")

	if _, err := manager.UpdatePlugins(t.Context(), board, []string{"acme/date-tools", "acme/text-tools"}); err == nil {
		t.Fatal("UpdatePlugins succeeded with an invalid second candidate")
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("board lock changed after failed update:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestUpdatePluginsWritesAllCandidatesTogether(t *testing.T) {
	if !kbrdfs.GitAvailable() {
		t.Skip("git unavailable")
	}
	repo, manager, board := setupTwoPluginMarketplace(t)
	if err := manager.SetPluginEnabled(board, "acme/date-tools", false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"date-tools", "text-tools"} {
		manifestPath := filepath.Join(repo, "plugins", name, "plugin.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, manifestPath, strings.Replace(string(data), `"version": "1.0.0"`, `"version": "2.0.0"`, 1))
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "update both plugins transactionally")

	updated, err := manager.UpdatePlugins(t.Context(), board, []string{"acme/date-tools", "acme/text-tools"})
	if err != nil {
		t.Fatalf("UpdatePlugins: %v", err)
	}
	if len(updated) != 2 {
		t.Fatalf("updated %d plugins, want 2", len(updated))
	}
	lock, err := plugin.LoadBoardLock(board)
	if err != nil {
		t.Fatal(err)
	}
	for _, locked := range lock.Plugins {
		if locked.Version != "2.0.0" {
			t.Errorf("%s version = %q, want 2.0.0", locked.ID, locked.Version)
		}
		if locked.ID == "acme/date-tools" && !locked.Disabled {
			t.Errorf("update re-enabled %s", locked.ID)
		}
	}
	if runtime, err := manager.RuntimePlugins(board); err != nil || len(runtime) != 1 || runtime[0].ID != "acme/text-tools" {
		t.Fatalf("updated content is not synchronized: %v", err)
	}
}

func setupTwoPluginMarketplace(t *testing.T) (string, *plugin.Manager, string) {
	t.Helper()
	repo := createMarketplaceRepo(t)
	writeFile(t, filepath.Join(repo, "marketplace.json"), `{
  "apiVersion": 1,
  "name": "acme",
  "description": "Test plugins",
  "plugins": [
    {"name":"date-tools","source":"plugins/date-tools"},
    {"name":"text-tools","source":"plugins/text-tools"}
  ]
}`)
	textRoot := filepath.Join(repo, "plugins", "text-tools")
	writeFile(t, filepath.Join(textRoot, "plugin.json"), `{
  "apiVersion": 1,
  "name": "text-tools",
  "version": "1.0.0",
  "description": "Text helpers",
  "entrypoint": "init.lua"
}`)
	writeFile(t, filepath.Join(textRoot, "init.lua"), `return "text"`)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "add text plugin")

	root := t.TempDir()
	manager := plugin.NewManager(plugin.Paths{
		ConfigRoot:       filepath.Join(root, "config"),
		CacheRoot:        filepath.Join(root, "cache"),
		RegistryFile:     filepath.Join(root, "config", "marketplaces.json"),
		MarketplaceCache: filepath.Join(root, "cache", "marketplaces"),
		ContentCache:     filepath.Join(root, "cache", "content"),
	})
	if _, err := manager.AddMarketplace(t.Context(), repo, ""); err != nil {
		t.Fatal(err)
	}
	board := t.TempDir()
	for _, id := range []string{"acme/date-tools", "acme/text-tools"} {
		if _, err := manager.AddPlugin(t.Context(), board, id); err != nil {
			t.Fatal(err)
		}
	}
	return repo, manager, board
}

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
	if err := manager.SetPluginEnabled(board, "acme/date-tools", false); err != nil {
		t.Fatalf("Disable plugin: %v", err)
	}
	updated, err := manager.UpdatePlugin(t.Context(), board, "acme/date-tools")
	if err != nil {
		t.Fatalf("UpdatePlugin without registered marketplace: %v", err)
	}
	if updated.ID != "acme/date-tools" || updated.Version != "2.0.0" || !updated.Disabled {
		t.Fatalf("UpdatePlugin = %+v", updated)
	}
	marketplaces, err := manager.Marketplaces()
	if err != nil {
		t.Fatalf("Marketplaces: %v", err)
	}
	if len(marketplaces) != 1 || marketplaces[0].Name != "acme" {
		t.Fatalf("marketplaces after UpdatePlugin = %+v", marketplaces)
	}
	if err := manager.SetPluginEnabled(board, "acme/date-tools", true); err != nil {
		t.Fatalf("Enable plugin: %v", err)
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

func TestDisabledPluginDoesNotBlockFolderLuaStartup(t *testing.T) {
	configRoot := t.TempDir()
	cacheRoot := t.TempDir()
	t.Setenv("KBRD_PLUGIN_CONFIG_DIR", configRoot)
	t.Setenv("KBRD_PLUGIN_CACHE_DIR", cacheRoot)
	board := t.TempDir()
	writeFile(t, filepath.Join(board, ".kbrd.lua"), `
kbrd.command("board-command", "Board command", function() return "ok" end)
`)
	lock := plugin.BoardLock{Plugins: []plugin.LockedPlugin{{
		ID: "acme/date-tools", Disabled: true, Marketplace: "acme",
		MarketplaceURL: "https://example.com/acme.git", MarketplaceCommit: strings.Repeat("a", 40),
		Source: "plugins/date-tools", Entrypoint: "init.lua",
		ContentSHA256: "sha256:" + strings.Repeat("0", 64),
	}}}
	if err := plugin.SaveBoardLock(board, lock); err != nil {
		t.Fatal(err)
	}

	host, err := script.New(config.ScriptingConfig{Enabled: true, InitTimeoutMs: 2000}, nil, nil, board, "")
	if err != nil {
		t.Fatalf("script.New with disabled missing plugin cache: %v", err)
	}
	defer host.Close()
	commands := host.Commands()
	if len(commands) != 1 || commands[0].ID != "board-command" {
		t.Fatalf("commands = %+v", commands)
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
