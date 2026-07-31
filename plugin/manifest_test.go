package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMarketplaceValidatesCatalogAndPlugin(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, MarketplaceFile), `{
  "apiVersion": 1,
  "name": "community",
  "plugins": [{"name":"date-tools","source":"plugins/date-tools"}]
}`)
	pluginRoot := filepath.Join(root, "plugins", "date-tools")
	writeTestFile(t, filepath.Join(pluginRoot, PluginFile), `{
  "apiVersion": 1,
  "name": "date-tools",
  "description": "Date helpers",
  "entrypoint": "init.lua"
}`)
	writeTestFile(t, filepath.Join(pluginRoot, "init.lua"), `return {}`)

	manifest, err := LoadMarketplace(root)
	if err != nil {
		t.Fatalf("LoadMarketplace: %v", err)
	}
	if manifest.Name != "community" || len(manifest.Plugins) != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func TestLoadMarketplaceRejectsEscapingSource(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, MarketplaceFile), `{
  "apiVersion": 1,
  "name": "community",
  "plugins": [{"name":"date-tools","source":"../date-tools"}]
}`)
	_, err := LoadMarketplace(root)
	if err == nil || !strings.Contains(err.Error(), "escapes its root") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadPluginRejectsInvalidDeclarativeMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata string
		want     string
	}{
		{name: "empty command", metadata: `,"commands":[""]`, want: "commands: declarations must not be empty"},
		{name: "duplicate hook", metadata: `,"hooks":["item_saved","item_saved"]`, want: `hooks: duplicate declaration "item_saved"`},
		{name: "escaping readme", metadata: `,"readme":"../README.md"`, want: "readme: path escapes its root"},
		{name: "missing changelog", metadata: `,"changelog":"CHANGELOG.md"`, want: "changelog:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, filepath.Join(root, PluginFile), `{
  "apiVersion":1,"name":"date-tools","description":"Date helpers","entrypoint":"init.lua"`+tt.metadata+`}`)
			writeTestFile(t, filepath.Join(root, "init.lua"), `return {}`)
			_, err := LoadPlugin(root)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestLoadPluginAcceptsStaticAssetsWithoutEntrypoint(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, PluginFile), `{
  "apiVersion": 1,
  "name": "planning-kit",
  "description": "Planning board assets",
  "commands": ["plan"],
  "shellAccess": true,
  "assets": {
    "cardTemplates": "templates",
    "themes": "themes",
    "frontmatterPresets": "presets.toml",
    "customCommands": "commands.yml",
    "boardStarters": "starters"
  }
}`)
	writeTestFile(t, filepath.Join(root, "templates", "task.md"), "---\n---\n")
	writeTestFile(t, filepath.Join(root, "themes", "night.toml"), "name = \"night\"\n")
	writeTestFile(t, filepath.Join(root, "presets.toml"), "[[frontmatter_presets]]\n")
	writeTestFile(t, filepath.Join(root, "commands.yml"), "commands: []\n")
	writeTestFile(t, filepath.Join(root, "starters", "simple", "README.md"), "# Simple\n")

	manifest, err := LoadPlugin(root)
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}
	if manifest.Entrypoint != "" || manifest.Assets.CardTemplates != "templates" || manifest.Assets.BoardStarters != "starters" {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func TestLoadPluginRejectsUnsafeOrInertStaticMetadata(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		want  string
		setup func(string)
	}{
		{
			name: "no entrypoint or assets",
			body: `{"apiVersion":1,"name":"empty","description":"Empty"}`,
			want: "entrypoint or assets are required",
		},
		{
			name: "asset escapes root",
			body: `{"apiVersion":1,"name":"escape","description":"Escape","assets":{"themes":"../themes"}}`,
			want: "path escapes its root",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, filepath.Join(root, PluginFile), tt.body)
			if tt.setup != nil {
				tt.setup(root)
			}
			_, err := LoadPlugin(root)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestContentDigestRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "target.lua"), `return {}`)
	if err := os.Symlink("target.lua", filepath.Join(root, "link.lua")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := contentDigest(root)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v", err)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
