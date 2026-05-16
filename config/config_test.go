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
	if cfg.ColumnWidth != 32 || cfg.PreviewLines != 3 || cfg.Theme != "light" || cfg.NotifyBackend != "auto" {
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
