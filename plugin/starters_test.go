package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoardStarterApplyPreservesDirectoriesAndModes(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "Todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(source, "bin", "setup.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cache := filepath.Join(t.TempDir(), "cached")
	if err := copyPluginTree(source, cache); err != nil {
		t.Fatalf("copyPluginTree: %v", err)
	}
	target := t.TempDir()
	if err := (BoardStarter{root: cache}).apply(target, false); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if info, err := os.Stat(filepath.Join(target, "Todo")); err != nil || !info.IsDir() {
		t.Fatalf("empty starter directory: info=%v err=%v", info, err)
	}
	info, err := os.Stat(filepath.Join(target, "bin", "setup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("script mode = %#o, want 0755", got)
	}
}

func TestBoardStarterApplyRejectsSymlinkedTargetParent(t *testing.T) {
	source := t.TempDir()
	path := filepath.Join(source, "config", "tool.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("enabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	target := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(target, "config")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	err := (BoardStarter{root: source}).apply(target, true)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("apply error = %v, want symlink rejection", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "tool.toml")); !os.IsNotExist(err) {
		t.Fatalf("starter wrote outside target: %v", err)
	}
}
