package boardenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerApplyAndRestore(t *testing.T) {
	clearEnv(t, "BOARDENV_ADD", "DIRENV_DIFF")
	m := fakeManager(t, "printf '%s' \"$FAKE_DIRENV_OUTPUT\"")
	t.Setenv("BOARDENV_KEEP", "old")
	t.Setenv("BOARDENV_REMOVE", "remove-me")
	t.Setenv("FAKE_DIRENV_OUTPUT", `{"BOARDENV_KEEP":"new","BOARDENV_ADD":"added","BOARDENV_REMOVE":null,"DIRENV_DIFF":"state-a"}`)

	change, err := m.Apply(t.TempDir())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertEnv(t, "BOARDENV_KEEP", "new", true)
	assertEnv(t, "BOARDENV_ADD", "added", true)
	assertEnv(t, "BOARDENV_REMOVE", "", false)
	assertEnv(t, "DIRENV_DIFF", "state-a", true)

	if err := change.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	assertEnv(t, "BOARDENV_KEEP", "old", true)
	assertEnv(t, "BOARDENV_ADD", "", false)
	assertEnv(t, "BOARDENV_REMOVE", "remove-me", true)
	assertEnv(t, "DIRENV_DIFF", "", false)
	if err := change.Restore(); err != nil {
		t.Fatalf("second Restore: %v", err)
	}
}

func TestManagerApplySwitchesEnvironment(t *testing.T) {
	clearEnv(t, "BOARDENV_A", "BOARDENV_B", "DIRENV_DIFF")
	m := fakeManager(t, "printf '%s' \"$FAKE_DIRENV_OUTPUT\"")
	t.Setenv("FAKE_DIRENV_OUTPUT", `{"BOARDENV_A":"a","DIRENV_DIFF":"state-a"}`)
	if _, err := m.Apply(t.TempDir()); err != nil {
		t.Fatalf("apply board A: %v", err)
	}
	if !m.Active() {
		t.Fatal("manager should report board A environment active")
	}

	if err := os.Setenv("FAKE_DIRENV_OUTPUT", `{"BOARDENV_A":null,"BOARDENV_B":"b","DIRENV_DIFF":"state-b"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apply(t.TempDir()); err != nil {
		t.Fatalf("apply board B: %v", err)
	}
	assertEnv(t, "BOARDENV_A", "", false)
	assertEnv(t, "BOARDENV_B", "b", true)
	assertEnv(t, "DIRENV_DIFF", "state-b", true)
	if !m.Active() {
		t.Fatal("manager should report board B environment active")
	}

	if err := os.Setenv("FAKE_DIRENV_OUTPUT", `{"BOARDENV_B":null,"DIRENV_DIFF":null}`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apply(t.TempDir()); err != nil {
		t.Fatalf("unload board B: %v", err)
	}
	if m.Active() {
		t.Fatal("manager should report direnv inactive after unload")
	}
}

func TestManagerApplyFailureDoesNotChangeEnvironmentOrLeakStderr(t *testing.T) {
	m := fakeManager(t, "echo 'secret-token' >&2\nexit 1")
	t.Setenv("BOARDENV_KEEP", "old")

	_, err := m.Apply(t.TempDir())
	if err == nil {
		t.Fatal("Apply should fail")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaked direnv stderr: %v", err)
	}
	assertEnv(t, "BOARDENV_KEEP", "old", true)
}

func TestManagerApplyRejectsMalformedJSON(t *testing.T) {
	m := fakeManager(t, "printf 'not-json'")
	t.Setenv("BOARDENV_KEEP", "old")

	if _, err := m.Apply(t.TempDir()); err == nil {
		t.Fatal("Apply should reject malformed JSON")
	}
	assertEnv(t, "BOARDENV_KEEP", "old", true)
}

func TestManagerWithoutDirenvIsNoOp(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	m := New()
	t.Setenv("BOARDENV_KEEP", "old")

	change, err := m.Apply(t.TempDir())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if change != nil {
		t.Fatal("missing direnv should not produce a change")
	}
	assertEnv(t, "BOARDENV_KEEP", "old", true)
}

func fakeManager(t *testing.T, body string) *Manager {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "direnv")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake direnv: %v", err)
	}
	t.Setenv("PATH", dir)
	m := New()
	if m.executable == "" {
		t.Fatal("fake direnv was not resolved")
	}
	return m
}

func assertEnv(t *testing.T, key, want string, wantSet bool) {
	t.Helper()
	got, set := os.LookupEnv(key)
	if got != want || set != wantSet {
		t.Fatalf("%s = %q, set=%v; want %q, set=%v", key, got, set, want, wantSet)
	}
}

func clearEnv(t *testing.T, keys ...string) {
	t.Helper()
	type value struct {
		text string
		set  bool
	}
	before := make(map[string]value, len(keys))
	for _, key := range keys {
		text, set := os.LookupEnv(key)
		before[key] = value{text: text, set: set}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
	t.Cleanup(func() {
		for _, key := range keys {
			old := before[key]
			if old.set {
				_ = os.Setenv(key, old.text)
			} else {
				_ = os.Unsetenv(key)
			}
		}
	})
}
