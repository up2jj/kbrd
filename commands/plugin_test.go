package commands

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginListUsesBoardLocalLock(t *testing.T) {
	t.Setenv("KBRD_PLUGIN_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("KBRD_PLUGIN_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
	board := t.TempDir()
	var output bytes.Buffer
	root := NewRootCmd()
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"plugin", "--board", board, "list"})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := output.String(); !strings.Contains(got, "no plugins locked for this board") {
		t.Fatalf("output = %q", got)
	}
}
