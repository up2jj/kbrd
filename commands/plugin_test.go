package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/plugin"
)

func TestPluginListUsesBoardLocalLock(t *testing.T) {
	t.Setenv("KBRD_PLUGIN_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("KBRD_PLUGIN_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
	board := t.TempDir()
	var output bytes.Buffer
	root := NewRootCmd()
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"plugin", "--board", board, "list"})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := output.String(); !strings.Contains(got, "no plugins locked for this board") {
		t.Fatalf("output = %q", got)
	}
}

func TestPluginDisableAndEnablePersistBoardLock(t *testing.T) {
	t.Setenv("KBRD_PLUGIN_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("KBRD_PLUGIN_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
	board := t.TempDir()
	locked := plugin.LockedPlugin{
		ID: "acme/date-tools", Version: "1.2.3", Description: "Date helpers",
		Marketplace: "acme", MarketplaceURL: "https://example.com/acme.git",
		MarketplaceCommit: strings.Repeat("a", 40), Source: "plugins/date-tools",
		Entrypoint: "init.lua", ContentSHA256: "sha256:" + strings.Repeat("0", 64),
	}
	if err := plugin.SaveBoardLock(board, plugin.BoardLock{Plugins: []plugin.LockedPlugin{locked}}); err != nil {
		t.Fatal(err)
	}

	output := executePluginCommand(t, "plugin", "--board", board, "disable", locked.ID)
	if !strings.Contains(output, "disabled acme/date-tools in "+plugin.LockFile) {
		t.Fatalf("disable output = %q", output)
	}
	lock, err := plugin.LoadBoardLock(board)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Plugins) != 1 || !lock.Plugins[0].Disabled {
		t.Fatalf("disabled lock = %+v", lock)
	}
	pinned := lock.Plugins[0]
	pinned.Disabled = false
	if pinned != locked {
		t.Fatalf("disable changed pin:\ngot  %+v\nwant %+v", pinned, locked)
	}
	raw, err := os.ReadFile(filepath.Join(board, plugin.LockFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"disabled": true`) {
		t.Fatalf("disabled state missing from board lock:\n%s", raw)
	}
	if output = executePluginCommand(t, "plugin", "--board", board, "list"); !strings.Contains(output, "disabled") {
		t.Fatalf("list output = %q", output)
	}
	manager, err := plugin.DefaultManager()
	if err != nil {
		t.Fatal(err)
	}
	if runtime, err := manager.RuntimePlugins(board); err != nil || len(runtime) != 0 {
		t.Fatalf("disabled RuntimePlugins = %+v, %v", runtime, err)
	}

	output = executePluginCommand(t, "plugin", "--board", board, "enable", locked.ID)
	if !strings.Contains(output, "enabled acme/date-tools in "+plugin.LockFile) {
		t.Fatalf("enable output = %q", output)
	}
	lock, err = plugin.LoadBoardLock(board)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Plugins) != 1 || lock.Plugins[0] != locked {
		t.Fatalf("re-enabled lock = %+v, want original pin %+v", lock, locked)
	}
	if _, err := manager.RuntimePlugins(board); err == nil {
		t.Fatal("enabled plugin with a missing cache did not block runtime loading")
	}
}

func TestPluginDisableRejectsPluginOutsideBoardLock(t *testing.T) {
	t.Setenv("KBRD_PLUGIN_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("KBRD_PLUGIN_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
	root := NewRootCmd()
	root.SetArgs([]string{"plugin", "--board", t.TempDir(), "disable", "acme/date-tools"})
	if err := root.ExecuteContext(t.Context()); err == nil || !strings.Contains(err.Error(), "is not in this board's lock") {
		t.Fatalf("disable error = %v", err)
	}
}

func executePluginCommand(t *testing.T, args ...string) string {
	t.Helper()
	var output bytes.Buffer
	root := NewRootCmd()
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs(args)
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	return output.String()
}

func TestPluginUpdateDryRunWithEmptyLock(t *testing.T) {
	t.Setenv("KBRD_PLUGIN_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("KBRD_PLUGIN_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
	board := t.TempDir()
	var output bytes.Buffer
	root := NewRootCmd()
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"plugin", "--board", board, "update", "--dry-run"})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, err := plugin.LoadBoardLock(board); err != nil {
		t.Fatalf("load unchanged lock: %v", err)
	}
}

func TestPrintPluginInfoShowsDeclarativeMetadata(t *testing.T) {
	info := plugin.PluginInfo{
		ID: "acme/date-tools",
		Manifest: plugin.PluginManifest{
			Version: "2.0.0", Description: "Date helpers",
			Author:  plugin.Owner{Name: "Acme", URL: "https://example.com"},
			License: "MIT", Homepage: "https://example.com/date-tools",
			Commands: []string{"set-due-date"}, Hooks: []string{"item_saved"},
			Layers: []string{"planning"}, Timers: []string{"daily rollover"},
			NetworkAccess: true, README: "README.md", Changelog: "CHANGELOG.md",
		},
		Marketplace: plugin.Marketplace{URL: "https://example.com/acme.git", Commit: strings.Repeat("a", 40)},
	}
	var output bytes.Buffer
	cmd := newPluginInfoCmd(new(string))
	cmd.SetOut(&output)
	printPluginInfo(cmd, info)
	for _, want := range []string{
		"Plugin: acme/date-tools", "Description: Date helpers", "Author: Acme (https://example.com)",
		"License: MIT", "Homepage: https://example.com/date-tools", "Installed version: not installed",
		"Available version: 2.0.0", "Marketplace URL: https://example.com/acme.git",
		"Commands: set-due-date", "Hooks: item_saved", "Layers: planning", "Timers: daily rollover",
		"Network access: true", "Shell access: false", "README: README.md", "Changelog: CHANGELOG.md",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
}

func TestPrintPluginInfoShowsDisabledStatus(t *testing.T) {
	info := plugin.PluginInfo{
		ID:          "acme/date-tools",
		Manifest:    plugin.PluginManifest{Description: "Date helpers"},
		Marketplace: plugin.Marketplace{},
		Installed:   &plugin.LockedPlugin{Version: "1.0.0", Disabled: true},
	}
	var output bytes.Buffer
	cmd := newPluginInfoCmd(new(string))
	cmd.SetOut(&output)
	printPluginInfo(cmd, info)
	if !strings.Contains(output.String(), "Status: disabled") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestPrintUpdatePreviewShowsManifestAndFiles(t *testing.T) {
	preview := plugin.UpdatePreview{
		ID: "acme/date-tools",
		Current: plugin.LockedPlugin{
			Version: "1.0.0", MarketplaceCommit: strings.Repeat("a", 40), ContentSHA256: "sha256:old",
		},
		Candidate: plugin.LockedPlugin{
			Version: "2.0.0", MarketplaceCommit: strings.Repeat("b", 40), ContentSHA256: "sha256:new",
		},
		ManifestChanges: []plugin.ManifestChange{{Field: "version", Before: `"1.0.0"`, After: `"2.0.0"`}},
		Files:           []plugin.PluginFileChange{{Path: "init.lua", Status: "modified"}, {Path: "new.lua", Status: "added"}},
		Patch:           "diff --git a/init.lua b/init.lua",
	}
	var output bytes.Buffer
	cmd := newPluginDiffCmd(new(string))
	cmd.SetOut(&output)
	printUpdatePreview(cmd, preview, true)
	for _, want := range []string{
		"acme/date-tools: 1.0.0 -> 2.0.0 (update available)",
		"marketplace: aaaaaaaaaa -> bbbbbbbbbb", "version: \"1.0.0\" -> \"2.0.0\"",
		"M init.lua", "A new.lua", "diff --git a/init.lua b/init.lua",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
}
