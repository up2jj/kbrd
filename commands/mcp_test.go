package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPSetupCommand_CodexDryRun(t *testing.T) {
	isolateConfig(t)
	t.Setenv("CODEX_HOME", "")
	bin := t.TempDir()
	fakeExecutable(t, bin, "codex")
	t.Setenv("PATH", bin)

	out, err := executeRoot("mcp", "setup", "--client", "codex", "--dry-run")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "kbrd") || !strings.Contains(out, "codex") || !strings.Contains(out, "would configure Codex MCP") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("codex config exists after dry run: %v", err)
	}
}

func TestMCPSetupCommand_ClaudeWithAddr(t *testing.T) {
	isolateConfig(t)
	t.Setenv("CODEX_HOME", "")
	bin := t.TempDir()
	fakeExecutable(t, bin, "claude")
	t.Setenv("PATH", bin)

	out, err := executeRoot("mcp", "setup", "--client", "claude", "--addr", "127.0.0.1:9999")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "Claude Code MCP configured") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".claude.json"))
	if err != nil {
		t.Fatalf("read claude config: %v", err)
	}
	if !strings.Contains(string(data), `"url": "http://127.0.0.1:9999"`) {
		t.Fatalf("claude config missing addr:\n%s", data)
	}
}

func TestMCPSetupCommand_ForceReplacesCollision(t *testing.T) {
	isolateConfig(t)
	t.Setenv("CODEX_HOME", "")
	home := os.Getenv("HOME")
	bin := t.TempDir()
	fakeCodexExecutable(t, bin)
	t.Setenv("PATH", bin)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[mcp_servers.kbrd]\nurl = \"http://127.0.0.1:1111\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := executeRoot("mcp", "setup", "--client", "codex", "--addr", "127.0.0.1:2222", "--force", "--no-enable-kbrd")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "Codex MCP configured") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "2222") || strings.Contains(string(data), "1111") {
		t.Fatalf("force did not replace collision:\n%s", data)
	}
}

func executeRoot(args ...string) (string, error) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func fakeExecutable(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func fakeCodexExecutable(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "codex")
	body := `#!/bin/sh
set -eu
if [ "$1" = "mcp" ] && [ "$2" = "remove" ] && [ "$3" = "kbrd" ]; then
  /bin/rm -f "$HOME/.codex/config.toml"
  exit 0
fi
if [ "$1" = "mcp" ] && [ "$2" = "add" ] && [ "$3" = "kbrd" ] && [ "$4" = "--url" ]; then
  /bin/mkdir -p "$HOME/.codex"
  printf '[mcp_servers.kbrd]\nurl = "%s"\n' "$5" > "$HOME/.codex/config.toml"
  exit 0
fi
exit 0
`
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
