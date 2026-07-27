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
