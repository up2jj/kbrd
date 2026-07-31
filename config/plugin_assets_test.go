package config

import (
	"os"
	"path/filepath"
	"testing"

	"kbrd/plugin"
)

func TestPluginAssetLoadersNamespaceAndAllowBoardOverrides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	presets := filepath.Join(root, "presets")
	writeAssetTestFile(t, filepath.Join(presets, "planning.toml"), `
[[frontmatter_presets]]
id = "today"
name = "Today"
[frontmatter_presets.set]
due = "{{today}}"
`)
	loadedPresets, err := LoadPluginFrontmatterPresets([]plugin.AssetSource{{ID: "acme/planning-kit", Path: presets}})
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedPresets) != 1 || loadedPresets[0].ID != "acme/planning-kit:today" {
		t.Fatalf("presets = %+v", loadedPresets)
	}
	boardPreset := FrontmatterPreset{ID: "acme/planning-kit:today", Name: "Board today", Set: map[string]any{"due": "later"}}
	merged := MergeFrontmatterPresets(loadedPresets, []FrontmatterPreset{boardPreset})
	if len(merged) != 1 || merged[0].Name != "Board today" {
		t.Fatalf("merged presets = %+v", merged)
	}

	commands := filepath.Join(root, "commands")
	writeAssetTestFile(t, filepath.Join(commands, "planning.yml"), `
commands:
  - id: plan
    name: Plan
    command: echo plan
`)
	board := t.TempDir()
	writeAssetTestFile(t, filepath.Join(board, FolderCommandsFile), `
commands:
  - id: acme/planning-kit:plan
    name: Board plan
    command: echo board
`)
	loadedCommands, warnings, err := LoadCommandsWithPluginAssets(board, []plugin.AssetSource{{ID: "acme/planning-kit", Path: commands}}, CommandLoadOptions{IncludeFolder: true})
	if err != nil || len(warnings) != 0 {
		t.Fatalf("commands warnings/error = %+v, %v", warnings, err)
	}
	if len(loadedCommands) != 1 || loadedCommands[0].ID != "acme/planning-kit:plan" || loadedCommands[0].Name != "Board plan" {
		t.Fatalf("commands = %+v", loadedCommands)
	}
}

func writeAssetTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
