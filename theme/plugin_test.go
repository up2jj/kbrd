package theme

import (
	"os"
	"path/filepath"
	"testing"

	"kbrd/plugin"
)

func TestLoadPluginThemesAppliesNamespacedOverlay(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "night.toml")
	if err := os.WriteFile(path, []byte(`
name = "night"
base = "dark"
[palette]
primary = "#123456"
danger = "#abcdef"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	dark := Palette{Primary: "dark-primary", Danger: "dark-danger", FgBase: "dark-fg"}
	light := Palette{Primary: "light-primary"}
	themes, err := LoadPluginThemes([]plugin.AssetSource{{ID: "acme/planning-kit", Path: root}}, dark, light)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := themes["acme/planning-kit/night"]
	if !ok || got.Primary != "#123456" || got.Danger != "#abcdef" || got.FgBase != "dark-fg" {
		t.Fatalf("theme = %+v, present=%v", got, ok)
	}
}

func TestLoadPluginThemesRejectsUnknownPaletteField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.toml")
	if err := os.WriteFile(path, []byte("[palette]\nprimray = \"#123456\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPluginThemes([]plugin.AssetSource{{ID: "acme/planning-kit", Path: path}}, Palette{}, Palette{})
	if err == nil {
		t.Fatal("LoadPluginThemes accepted an unknown palette field")
	}
}
