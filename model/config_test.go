package model

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"kbrd/config"
)

func realPath(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", p, err)
	}
	return resolved
}

func TestLocalConfigPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	got, err := localConfigPath()
	if err != nil {
		t.Fatalf("localConfigPath: %v", err)
	}
	want := filepath.Join(realPath(t, dir), config.FolderConfigFile)
	if realPath(t, filepath.Dir(got)) != filepath.Dir(want) || filepath.Base(got) != filepath.Base(want) {
		t.Fatalf("path: got %q want %q", got, want)
	}
}

func TestGlobalConfigPath_IsPure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := globalConfigPath()
	if err != nil {
		t.Fatalf("globalConfigPath: %v", err)
	}

	if filepath.Base(got) != config.GlobalConfigFile {
		t.Fatalf("filename: got %q want %q", filepath.Base(got), config.GlobalConfigFile)
	}
	if filepath.Base(filepath.Dir(got)) != config.AppDirName {
		t.Fatalf("parent dir name: got %q want %q", filepath.Base(filepath.Dir(got)), config.AppDirName)
	}

	if _, err := os.Stat(filepath.Dir(got)); !os.IsNotExist(err) {
		t.Fatalf("parent dir should not be created by path resolver; stat err: %v", err)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Fatalf("config file should not exist; stat err: %v", err)
	}
}

func TestEnsureConfigFile_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deeper", "x.toml")

	if err := ensureConfigFile(path); err != nil {
		t.Fatalf("ensureConfigFile: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestConfigCommandEntries(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	t.Chdir(cwd)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	// Pre-create the local config so we can assert Exists toggles.
	localPath := filepath.Join(cwd, config.FolderConfigFile)
	if err := os.WriteFile(localPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed local: %v", err)
	}

	entries := configCommandEntries()
	if len(entries) != 2 {
		t.Fatalf("entries: got %d want 2", len(entries))
	}

	local, global := entries[0], entries[1]

	if local.Key != "c" || global.Key != "C" {
		t.Fatalf("keys: got %q/%q want c/C", local.Key, global.Key)
	}
	if local.Err != nil || global.Err != nil {
		t.Fatalf("unexpected errors: local=%v global=%v", local.Err, global.Err)
	}
	if filepath.Base(local.Path) != config.FolderConfigFile {
		t.Fatalf("local path basename: got %q", filepath.Base(local.Path))
	}
	if filepath.Base(global.Path) != config.GlobalConfigFile {
		t.Fatalf("global path basename: got %q", filepath.Base(global.Path))
	}
	if filepath.Base(filepath.Dir(global.Path)) != config.AppDirName {
		t.Fatalf("global parent dir name: got %q want %q",
			filepath.Base(filepath.Dir(global.Path)), config.AppDirName)
	}
	if !local.Exists {
		t.Fatal("local.Exists: got false, want true (file was seeded)")
	}
	if global.Exists {
		t.Fatal("global.Exists: got true, want false (no file written)")
	}
}

func TestConfigFileExists(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present.toml")
	if err := os.WriteFile(present, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !configFileExists(present) {
		t.Fatalf("expected true for existing file")
	}
	if configFileExists(filepath.Join(dir, "missing.toml")) {
		t.Fatalf("expected false for missing file")
	}
}

func TestEnsureConfigFile_CreatesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.toml")

	if err := ensureConfigFile(path); err != nil {
		t.Fatalf("ensureConfigFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, config.Template) {
		t.Fatalf("contents: got %d bytes, want template (%d bytes)", len(got), len(config.Template))
	}
}

func TestEnsureConfigFile_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.toml")
	sentinel := []byte("# user edits\n")
	if err := os.WriteFile(path, sentinel, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := ensureConfigFile(path); err != nil {
		t.Fatalf("ensureConfigFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, sentinel) {
		t.Fatalf("file was overwritten; got %q want %q", got, sentinel)
	}
}

func TestEnsureConfigFile_StatErrorBubblesUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based unreadable parent test does not behave portably on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}

	parent := filepath.Join(t.TempDir(), "locked")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	if err := ensureConfigFile(filepath.Join(parent, "x.toml")); err == nil {
		t.Fatal("expected error from unreadable parent, got nil")
	}
}
