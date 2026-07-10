package setup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_EmptyMachineConfiguresAllTargets(t *testing.T) {
	home := isolateHome(t)
	var calls [][]string

	results, err := Run(Options{
		Addr:       "127.0.0.1:9999",
		EnableKBRD: true,
		LookPath:   foundLookPath,
		Run:        fakeMCPRun(t, home, &calls),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %v, want kbrd plus two clients", results)
	}
	if len(calls) != 2 || calls[0][0] != "/bin/codex" || calls[1][0] != "/bin/claude" {
		t.Fatalf("client CLI calls = %#v", calls)
	}
	assertFileContains(t, userConfigPath(t), "[mcp]\naddr = \"127.0.0.1:9999\"\nenabled = true\n")
	assertFileContains(t, codexConfigPathForHome(home), "[mcp_servers.kbrd]\nurl = \"http://127.0.0.1:9999\"\n")
	assertFileContains(t, claudeConfigPathForHome(home), `"url": "http://127.0.0.1:9999"`)
}

func TestRun_DryRunWritesNothing(t *testing.T) {
	home := isolateHome(t)
	results, err := Run(Options{
		Addr:       "127.0.0.1:7777",
		EnableKBRD: true,
		DryRun:     true,
		LookPath:   foundLookPath,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, res := range results {
		if res.Status != StatusFound {
			t.Fatalf("result = %+v, want found in dry run", res)
		}
	}
	for _, path := range []string{
		userConfigPath(t),
		codexConfigPathForHome(home),
		claudeConfigPathForHome(home),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s exists after dry-run: %v", path, err)
		}
	}
}

func TestRun_MissingClientsAreSkipped(t *testing.T) {
	isolateHome(t)
	results, err := Run(Options{
		Addr:       "127.0.0.1:7777",
		EnableKBRD: false,
		LookPath: func(string) (string, error) {
			return "", execNotFound
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %v", results)
	}
	for _, res := range results {
		if res.Status != StatusSkipped {
			t.Fatalf("result = %+v, want skipped", res)
		}
	}
}

var execNotFound = errors.New("not found")

func foundLookPath(name string) (string, error) {
	return "/bin/" + name, nil
}

func fakeMCPRun(t *testing.T, home string, calls *[][]string) func(string, ...string) error {
	t.Helper()
	return func(name string, args ...string) error {
		if calls != nil {
			*calls = append(*calls, append([]string{name}, args...))
		}
		switch filepath.Base(name) {
		case "codex":
			return fakeCodexRun(t, home, args...)
		case "claude":
			return fakeClaudeRun(t, home, args...)
		default:
			return nil
		}
	}
}

func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CODEX_HOME", "")
	return home
}

func userConfigPath(t *testing.T) string {
	t.Helper()
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "kbrd", "config.toml")
}

func codexConfigPathForHome(home string) string {
	return filepath.Join(home, ".codex", "config.toml")
}

func claudeConfigPathForHome(home string) string {
	return filepath.Join(home, ".claude.json")
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("%s does not contain %q:\n%s", path, want, data)
	}
}

func assertFileNotContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(data), want) {
		t.Fatalf("%s contains %q:\n%s", path, want, data)
	}
}
