package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestInstall(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "extension")
	written, err := Install(dir, "v0.11.0-3-gabc1234")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(written) < 5 {
		t.Fatalf("Install wrote %d files, want at least 5", len(written))
	}

	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		ManifestVersion int      `json:"manifest_version"`
		Version         string   `json:"version"`
		VersionName     string   `json:"version_name"`
		Permissions     []string `json:"permissions"`
		HostPermissions []string `json:"host_permissions"`
		Background      struct {
			ServiceWorker string `json:"service_worker"`
		} `json:"background"`
		Action struct {
			DefaultPopup string `json:"default_popup"`
		} `json:"action"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if err := ValidateExtensionKey(data); err != nil {
		t.Fatalf("validate extension key: %v", err)
	}
	if manifest.ManifestVersion != 3 || manifest.Action.DefaultPopup != "popup.html" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if manifest.Version != "0.11.0.3" || manifest.VersionName != "v0.11.0-3-gabc1234" {
		t.Fatalf("extension version = %q (%q), want 0.11.0.3 (v0.11.0-3-gabc1234)", manifest.Version, manifest.VersionName)
	}
	if !slices.Contains(manifest.Permissions, "nativeMessaging") {
		t.Fatalf("manifest permissions = %v, want nativeMessaging", manifest.Permissions)
	}
	if !slices.Contains(manifest.Permissions, "contextMenus") {
		t.Fatalf("manifest permissions = %v, want contextMenus", manifest.Permissions)
	}
	if manifest.Background.ServiceWorker != "background.js" {
		t.Fatalf("background service worker = %q, want background.js", manifest.Background.ServiceWorker)
	}
	if len(manifest.HostPermissions) != 0 {
		t.Fatalf("manifest unexpectedly requests network hosts: %v", manifest.HostPermissions)
	}

	if err := os.WriteFile(manifestPath, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir, "v0.11.0"); err != nil {
		t.Fatalf("second Install: %v", err)
	}
	data, err = os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "changed" {
		t.Fatal("second Install did not refresh managed asset")
	}
}

func TestInstallRefusesUnmanagedDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mine.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Install(dir, "dev")
	if err == nil || !strings.Contains(err.Error(), "unmanaged") {
		t.Fatalf("Install error = %v, want unmanaged-directory refusal", err)
	}
}

func TestInstallAllowsEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir, "dev"); err != nil {
		t.Fatalf("Install into empty directory: %v", err)
	}
}

func TestInstallRefusesSymlink(t *testing.T) {
	target := t.TempDir()
	dir := filepath.Join(t.TempDir(), "extension")
	if err := os.Symlink(target, dir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := Install(dir, "dev")
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Install error = %v, want symlink refusal", err)
	}
}

func TestBrowserVersion(t *testing.T) {
	tests := []struct {
		name        string
		appVersion  string
		wantVersion string
		wantName    string
	}{
		{name: "release", appVersion: "v1.2.3", wantVersion: "1.2.3", wantName: "v1.2.3"},
		{name: "git describe", appVersion: "v1.2.3-42-gabcdef-dirty", wantVersion: "1.2.3.42", wantName: "v1.2.3-42-gabcdef-dirty"},
		{name: "snapshot", appVersion: "v1.2.4-next", wantVersion: "1.2.4", wantName: "v1.2.4-next"},
		{name: "development", appVersion: "dev", wantVersion: "0.0.0.1", wantName: "dev"},
		{name: "empty", wantVersion: "0.0.0.1", wantName: "dev"},
		{name: "component too large", appVersion: "v1.70000.3", wantVersion: "0.0.0.1", wantName: "v1.70000.3"},
		{name: "leading zero", appVersion: "v1.02.3", wantVersion: "0.0.0.1", wantName: "v1.02.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, versionName := browserVersion(tt.appVersion)
			if version != tt.wantVersion || versionName != tt.wantName {
				t.Fatalf("browserVersion(%q) = %q, %q; want %q, %q", tt.appVersion, version, versionName, tt.wantVersion, tt.wantName)
			}
		})
	}
}

func TestDefaultDirIsVisibleInHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if filepath.Base(dir) != "kbrd-chrome-extension" {
		t.Fatalf("DefaultDir = %q, want visible kbrd-chrome-extension directory", dir)
	}
}
