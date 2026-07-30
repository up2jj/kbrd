package commands

import (
	"bytes"
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
