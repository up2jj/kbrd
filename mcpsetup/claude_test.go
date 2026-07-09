package mcpsetup

import (
	"errors"
	"testing"
)

func TestClaudeConfigure_AddsViaCLIWithFallbackVerification(t *testing.T) {
	home := isolateHome(t)
	var calls [][]string

	res, err := (claudeAdapter{}).Configure(runContext{
		addr: "127.0.0.1:9999",
		opts: Options{Run: fakeMCPRun(t, home, &calls)},
	}, "/bin/claude")
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if res.Status != StatusConfigured {
		t.Fatalf("result = %+v, want configured", res)
	}
	if len(calls) != 1 || calls[0][0] != "/bin/claude" {
		t.Fatalf("calls = %#v", calls)
	}
	assertFileContains(t, claudeConfigPathForHome(home), `"kbrd": {`)
	assertFileContains(t, claudeConfigPathForHome(home), `"url": "http://127.0.0.1:9999"`)
}

func TestClaudeConfigure_PreservesUnrelatedConfig(t *testing.T) {
	home := isolateHome(t)
	path := claudeConfigPathForHome(home)
	writeTestFile(t, path, "{\n  \"theme\": \"dark\",\n  \"mcpServers\": {\n    \"other\": {\"type\":\"http\",\"url\":\"http://other\"}\n  }\n}\n")

	if _, err := (claudeAdapter{}).Configure(runContext{
		addr: "127.0.0.1:7777",
		opts: Options{Run: func(string, ...string) error { return errors.New("fall back to JSON writer") }},
	}, "/bin/claude"); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	assertFileContains(t, path, `"theme": "dark"`)
	assertFileContains(t, path, `"other": {`)
	assertFileContains(t, path, `"kbrd": {`)
}

func TestClaudeConfigure_ExistingEntrySkippedUnlessForced(t *testing.T) {
	home := isolateHome(t)
	path := claudeConfigPathForHome(home)
	writeTestFile(t, path, "{\n  \"mcpServers\": {\n    \"kbrd\": {\"type\":\"http\",\"url\":\"http://127.0.0.1:1111\"}\n  }\n}\n")

	res, err := (claudeAdapter{}).Configure(runContext{
		addr: "127.0.0.1:2222",
		opts: Options{},
	}, "/bin/claude")
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if res.Status != StatusSkipped {
		t.Fatalf("result = %+v, want skipped", res)
	}
	assertFileContains(t, path, "1111")

	if _, err := (claudeAdapter{}).Configure(runContext{
		addr: "127.0.0.1:2222",
		opts: Options{Force: true},
	}, "/bin/claude"); err != nil {
		t.Fatalf("Configure force: %v", err)
	}
	assertFileContains(t, path, "2222")
	assertFileNotContains(t, path, "1111")
}

func fakeClaudeRun(t *testing.T, home string, args ...string) error {
	t.Helper()
	if len(args) == 7 &&
		args[0] == "mcp" &&
		args[1] == "add" &&
		args[2] == "--transport" &&
		args[3] == "http" &&
		args[4] == "--scope" &&
		args[5] == "user" &&
		args[6] == "kbrd" {
		data, _, err := readOptional(claudeConfigPathForHome(home))
		if err != nil {
			t.Fatal(err)
		}
		next, err := writeClaudeEntry(data, "http://127.0.0.1:9999")
		if err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, claudeConfigPathForHome(home), string(next))
		return nil
	}
	return nil
}
