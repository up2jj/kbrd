package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteLocalTemplate(t *testing.T) {
	dir := t.TempDir()
	if err := writeLocalTemplate(dir); err != nil {
		t.Fatalf("writeLocalTemplate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "kbrd.toml")); err != nil {
		t.Errorf("expected kbrd.toml to be written: %v", err)
	}
	// A second run must refuse to clobber the existing file.
	if err := writeLocalTemplate(dir); err == nil {
		t.Error("expected error on second writeLocalTemplate (overwrite)")
	} else if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("error = %q, want it to mention 'refusing to overwrite'", err)
	}
}

func TestWriteGlobalTemplate(t *testing.T) {
	isolateConfig(t)
	if err := writeGlobalTemplate(); err != nil {
		t.Fatalf("writeGlobalTemplate: %v", err)
	}
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "kbrd", "config.toml")); err != nil {
		t.Errorf("expected global config template to be written: %v", err)
	}
}
