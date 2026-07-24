package commands

import (
	"bytes"
	"strings"
	"testing"
)

func TestCacheScriptCommandsUseCobraOutput(t *testing.T) {
	t.Setenv("KBRD_CACHE_DIR", t.TempDir())
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "list", args: []string{"cache", "script", "list"}, want: "no cached remote scripts"},
		{name: "purge", args: []string{"cache", "script", "purge"}, want: "removed 0 cached script(s)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			root := NewRootCmd()
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs(tt.args)
			if err := root.ExecuteContext(t.Context()); err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got := output.String(); !strings.Contains(got, tt.want) {
				t.Fatalf("output = %q, want %q", got, tt.want)
			}
		})
	}
}
