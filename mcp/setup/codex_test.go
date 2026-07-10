package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexConfigure_AddsViaCLI(t *testing.T) {
	home := isolateHome(t)
	var calls [][]string

	res, err := (codexAdapter{}).Configure(runContext{
		addr: "127.0.0.1:9999",
		opts: Options{
			LookPath: foundLookPath,
			Run:      fakeMCPRun(t, home, &calls),
		},
	}, "/bin/codex")
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if res.Status != StatusConfigured {
		t.Fatalf("result = %+v, want configured", res)
	}
	if len(calls) != 1 || calls[0][0] != "/bin/codex" {
		t.Fatalf("calls = %#v", calls)
	}
	assertFileContains(t, codexConfigPathForHome(home), "[mcp_servers.kbrd]\nurl = \"http://127.0.0.1:9999\"\n")
}

func TestCodexConfigure_PreservesUnrelatedConfig(t *testing.T) {
	home := isolateHome(t)
	path := codexConfigPathForHome(home)
	writeTestFile(t, path, "model = \"gpt-5\"\n\n[mcp_servers.other]\nurl = \"http://other\"\n")

	if _, err := (codexAdapter{}).Configure(runContext{
		addr: "127.0.0.1:7777",
		opts: Options{Run: fakeMCPRun(t, home, nil)},
	}, "/bin/codex"); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	assertFileContains(t, path, "model = \"gpt-5\"")
	assertFileContains(t, path, "[mcp_servers.other]")
	assertFileContains(t, path, "[mcp_servers.kbrd]")
}

func TestCodexConfigure_ExistingEntrySkippedUnlessForced(t *testing.T) {
	home := isolateHome(t)
	path := codexConfigPathForHome(home)
	writeTestFile(t, path, "[mcp_servers.kbrd]\nurl = \"http://127.0.0.1:1111\"\n")

	res, err := (codexAdapter{}).Configure(runContext{
		addr: "127.0.0.1:2222",
		opts: Options{},
	}, "/bin/codex")
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if res.Status != StatusSkipped {
		t.Fatalf("result = %+v, want skipped", res)
	}
	assertFileContains(t, path, "1111")

	if _, err := (codexAdapter{}).Configure(runContext{
		addr: "127.0.0.1:2222",
		opts: Options{
			Force: true,
			Run:   fakeMCPRun(t, home, nil),
		},
	}, "/bin/codex"); err != nil {
		t.Fatalf("Configure force: %v", err)
	}
	assertFileContains(t, path, "2222")
	assertFileNotContains(t, path, "1111")
}

func TestCodexConfigure_InvalidConfigDoesNotRewrite(t *testing.T) {
	home := isolateHome(t)
	path := codexConfigPathForHome(home)
	bad := "not = valid = toml"
	writeTestFile(t, path, bad)

	_, err := codexAdapter{}.Configure(runContext{
		addr: "127.0.0.1:7777",
		opts: Options{Run: fakeMCPRun(t, home, nil)},
	}, "/bin/codex")
	if err == nil {
		t.Fatal("expected invalid TOML error")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != bad {
		t.Fatalf("invalid config was rewritten: %q", got)
	}
}

func fakeCodexRun(t *testing.T, home string, args ...string) error {
	t.Helper()
	path := codexConfigPathForHome(home)
	if len(args) >= 3 && args[0] == "mcp" && args[1] == "remove" && args[2] == "kbrd" {
		_ = os.Remove(path)
		return nil
	}
	if len(args) == 5 && args[0] == "mcp" && args[1] == "add" && args[2] == "kbrd" && args[3] == "--url" {
		data, _, err := readOptional(path)
		if err != nil {
			t.Fatal(err)
		}
		block := "[mcp_servers.kbrd]\nurl = " + quoteTOMLString(args[4]) + "\n"
		writeTestFile(t, path, string(appendTOMLBlock(data, block)))
		return nil
	}
	return nil
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
