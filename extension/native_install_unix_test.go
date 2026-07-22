//go:build darwin || linux

package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallNativeHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	executable := filepath.Join(t.TempDir(), "kbrd")
	if err := os.WriteFile(executable, []byte("test binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	path, err := InstallNativeHost(executable)
	if err != nil {
		t.Fatalf("InstallNativeHost: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest nativeHostManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != NativeHostName || manifest.Path != executable {
		t.Fatalf("manifest = %+v", manifest)
	}
	if len(manifest.AllowedOrigins) != 1 || manifest.AllowedOrigins[0] != ExtensionOrigin {
		t.Fatalf("allowed origins = %v", manifest.AllowedOrigins)
	}
}
