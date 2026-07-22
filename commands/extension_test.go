package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtensionInstallCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dir := filepath.Join(t.TempDir(), "chrome")
	buf := new(bytes.Buffer)
	root := NewRootCmd()
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"extension", "install", "--dir", dir})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("ExecuteContext: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Fatalf("manifest not installed: %v", err)
	}
	if output := buf.String(); !strings.Contains(output, "Load unpacked") || !strings.Contains(output, "registered native messaging host") || !strings.Contains(output, dir) {
		t.Fatalf("output = %q", output)
	}
}
