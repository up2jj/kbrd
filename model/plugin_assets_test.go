package model

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/config"
	kbrdfs "kbrd/fs"
	"kbrd/plugin"
)

func TestStaticPluginAssetsLoadIntoBoardRuntime(t *testing.T) {
	if !kbrdfs.GitAvailable() {
		t.Skip("git unavailable")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KBRD_PLUGIN_CONFIG_DIR", filepath.Join(home, "plugin-config"))
	t.Setenv("KBRD_PLUGIN_CACHE_DIR", filepath.Join(home, "plugin-cache"))

	repo := t.TempDir()
	writePluginAssetFixture(t, filepath.Join(repo, "marketplace.json"), `{
  "apiVersion": 1,
  "name": "acme",
  "plugins": [{"name":"planning-kit","source":"plugins/planning-kit"}]
}`)
	root := filepath.Join(repo, "plugins", "planning-kit")
	writePluginAssetFixture(t, filepath.Join(root, "plugin.json"), `{
  "apiVersion": 1,
  "name": "planning-kit",
  "description": "Planning assets",
  "assets": {
    "cardTemplates": "templates",
    "themes": "themes",
    "frontmatterPresets": "presets.toml",
    "customCommands": "commands.yml",
    "boardStarters": "starters"
  }
}`)
	writePluginAssetFixture(t, filepath.Join(root, "templates", "task.md"), "---\nname: Task\n---\nTask body\n")
	writePluginAssetFixture(t, filepath.Join(root, "themes", "night.toml"), `
name = "night"
base = "dark"
[palette]
primary = "#123456"
`)
	writePluginAssetFixture(t, filepath.Join(root, "presets.toml"), `
[[frontmatter_presets]]
id = "today"
name = "Plugin today"
[frontmatter_presets.set]
due = "{{today}}"
`)
	writePluginAssetFixture(t, filepath.Join(root, "commands.yml"), `
commands:
  - id: plan
    name: Plan
    command: echo plan
`)
	writePluginAssetFixture(t, filepath.Join(root, "starters", "simple", "README.md"), "# Simple\n")
	runPluginAssetGit(t, repo, "init")
	runPluginAssetGit(t, repo, "config", "user.email", "test@example.com")
	runPluginAssetGit(t, repo, "config", "user.name", "Test")
	runPluginAssetGit(t, repo, "add", ".")
	runPluginAssetGit(t, repo, "commit", "-m", "static assets")

	manager, err := plugin.DefaultManager()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.AddMarketplace(t.Context(), repo, ""); err != nil {
		t.Fatal(err)
	}
	boardDir := t.TempDir()
	writePluginAssetFixture(t, filepath.Join(boardDir, config.FolderConfigFile), `
[display]
theme = "acme/planning-kit/night"

[[frontmatter_presets]]
id = "acme/planning-kit:today"
name = "Board today"
[frontmatter_presets.set]
due = "later"
`)
	columnDir := filepath.Join(boardDir, "Todo")
	if err := os.MkdirAll(columnDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.AddPlugin(t.Context(), boardDir, "acme/planning-kit"); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(boardDir)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBoard(cfg)
	defer b.Close()
	if err := b.initRuntime(); err != nil {
		t.Fatalf("initRuntime: %v", err)
	}
	if b.palette.Primary != "#123456" {
		t.Fatalf("plugin theme primary = %q", b.palette.Primary)
	}
	if len(b.cfg.FrontmatterPresets) != 1 || b.cfg.FrontmatterPresets[0].Name != "Board today" {
		t.Fatalf("frontmatter presets = %+v", b.cfg.FrontmatterPresets)
	}
	if command, ok := commandByID(b.commands, "acme/planning-kit:plan"); !ok || command.Name != "Plan" {
		t.Fatalf("commands = %+v", b.commands)
	}
	templates, warnings, err := b.listTemplates(columnDir)
	if err != nil || len(warnings) != 0 || len(templates) != 1 || templates[0].Name != "acme/planning-kit: Task" {
		t.Fatalf("templates/warnings/error = %+v, %+v, %v", templates, warnings, err)
	}
}

func writePluginAssetFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runPluginAssetGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
