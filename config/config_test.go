package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoad_DefaultsOnly(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	cfg, err := loadFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.ColumnWidth != 32 || cfg.PreviewLines != 3 || cfg.Theme != "dark" || cfg.NotifyBackend != "auto" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Path != folder {
		t.Fatalf("path: got %q want %q", cfg.Path, folder)
	}
}

func TestLoad_GlobalOnly(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(globalDir, "config.toml"), `
[display]
column_width = 50
theme = "dark"
`)

	cfg, err := loadFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.ColumnWidth != 50 {
		t.Fatalf("column_width: got %d want 50", cfg.ColumnWidth)
	}
	if cfg.Theme != "dark" {
		t.Fatalf("theme: got %q want dark", cfg.Theme)
	}
	if cfg.PreviewLines != 3 {
		t.Fatalf("preview_lines: got %d want default 3", cfg.PreviewLines)
	}
}

func TestLoad_PerFolderOverridesGlobal(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(globalDir, "config.toml"), `
[display]
column_width = 50
`)
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[display]
column_width = 24
`)

	cfg, err := loadFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.ColumnWidth != 24 {
		t.Fatalf("column_width: got %d want 24", cfg.ColumnWidth)
	}
}

func TestLoad_PerFolderPartialMerge(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(globalDir, "config.toml"), `
[display]
column_width = 50
preview_lines = 10
`)
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[display]
column_width = 24
`)

	cfg, err := loadFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.ColumnWidth != 24 {
		t.Fatalf("column_width: got %d want 24", cfg.ColumnWidth)
	}
	if cfg.PreviewLines != 10 {
		t.Fatalf("preview_lines: got %d want 10 (inherited from global)", cfg.PreviewLines)
	}
}

func TestLoad_EnvBeatsConfig(t *testing.T) {
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(globalDir, "config.toml"), `
[notify]
backend = "osascript"
`)
	t.Setenv("KBRD_NOTIFY", "osc9")

	cfg, err := loadFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.NotifyBackend != "osc9" {
		t.Fatalf("notify backend: got %q want osc9", cfg.NotifyBackend)
	}
}

func TestLoad_MalformedTOML(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(folder, "kbrd.toml"), "not = valid = toml = nope")

	_, err := loadFrom(globalDir, folder)
	if err == nil {
		t.Fatal("expected error for malformed TOML, got nil")
	}
	if !strings.Contains(err.Error(), "folder config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTemplate_IsValidGlobalConfig(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(globalDir, GlobalConfigFile), string(Template))

	if _, err := loadFrom(globalDir, folder); err != nil {
		t.Fatalf("Template is not a valid global config: %v", err)
	}
}

func TestLoad_AutoSyncInterval_Default(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	cfg, err := loadFrom(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.GitAutoSyncInterval != 0 {
		t.Fatalf("default auto_sync_interval: got %v want 0", cfg.GitAutoSyncInterval)
	}
}

func TestLoad_AutoSyncInterval_Parsed(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[git]
auto_sync_interval = "5m"
`)
	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.GitAutoSyncInterval != 5*time.Minute {
		t.Fatalf("auto_sync_interval: got %v want 5m", cfg.GitAutoSyncInterval)
	}
}

func TestLoad_AutoSyncInterval_Invalid(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[git]
auto_sync_interval = "banana"
`)
	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.GitAutoSyncInterval != 0 {
		t.Fatalf("invalid auto_sync_interval should yield 0, got %v", cfg.GitAutoSyncInterval)
	}
}

func TestLoad_MissingFolderPresentGlobal(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(globalDir, "config.toml"), `
[display]
theme = "dark"
`)

	cfg, err := loadFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.Theme != "dark" {
		t.Fatalf("theme: got %q want dark", cfg.Theme)
	}
}

func TestLoad_ServeSection(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(globalDir, "config.toml"), `
[serve]
addr = ":9090"
pull_interval = "30s"
`)
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[serve]
pull_interval = "5s"
`)

	cfg, err := loadFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.Serve.Addr != ":9090" {
		t.Fatalf("serve.addr: got %q want :9090 (inherited from global)", cfg.Serve.Addr)
	}
	if cfg.Serve.PullInterval != "5s" {
		t.Fatalf("serve.pull_interval: got %q want 5s (folder overrides global)", cfg.Serve.PullInterval)
	}
	if cfg.Serve.Domain != "" {
		t.Fatalf("serve.domain: got %q want empty default", cfg.Serve.Domain)
	}
	if cfg.Serve.TokenInTOML {
		t.Fatal("TokenInTOML: got true without serve.token present")
	}
}

func TestLoad_ServeDefaultsEmpty(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	cfg, err := loadFrom(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.Serve.Addr != "" || cfg.Serve.Domain != "" || cfg.Serve.PullInterval != "" {
		t.Fatalf("serve defaults must be empty (unset), got %+v", cfg.Serve)
	}
}

func TestLoad_ServeTokenDetectedNotRead(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[serve]
token = "supersecretvalue"
`)
	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if !cfg.Serve.TokenInTOML {
		t.Fatal("TokenInTOML: got false, want true")
	}
}

func TestValidateServe(t *testing.T) {
	cases := []struct {
		name    string
		toml    string
		wantErr string // substring; "" means valid
	}{
		{"empty", "", ""},
		{"no serve section", "[board]\nname = \"x\"\n", ""},
		{"valid serve", "[serve]\naddr = \":9090\"\npull_interval = \"30s\"\n", ""},
		{"zero interval", "[serve]\npull_interval = \"0\"\n", ""},
		{"bad toml", "not = valid = toml", "invalid TOML"},
		{"token present", "[serve]\ntoken = \"abc\"\n", "serve.token cannot be set"},
		{"bad interval", "[serve]\npull_interval = \"banana\"\n", "not a duration"},
		{"negative interval", "[serve]\npull_interval = \"-5s\"\n", "must not be negative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateServe([]byte(tc.toml))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateServe: unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateServe: got %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}
