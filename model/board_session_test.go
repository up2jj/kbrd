package model

import (
	"os"
	"path/filepath"
	"testing"

	"kbrd/boardenv"
	"kbrd/config"
)

func TestBoardSessionLoadBoardAppliesEnvironmentAndPreservesInstance(t *testing.T) {
	clearTestEnv(t, "KBRD_SWITCH_TEST", "DIRENV_DIFF")
	environment := testDirenvManager(t, `{"KBRD_SWITCH_TEST":"new-board","DIRENV_DIFF":"new-board-state"}`)
	b := NewBoardWithOptions(config.Config{
		Path:          t.TempDir(),
		InstanceName:  "local-instance",
		NotifyBackend: "none",
	}, BoardOptions{Environment: environment})
	t.Cleanup(b.Close)

	if _, err := b.session().loadBoard(t.TempDir()); err != nil {
		t.Fatalf("loadBoard: %v", err)
	}
	if got := os.Getenv("KBRD_SWITCH_TEST"); got != "new-board" {
		t.Fatalf("KBRD_SWITCH_TEST = %q, want new-board", got)
	}
	if b.cfg.InstanceName != "local-instance" {
		t.Fatalf("instance name = %q, want local-instance", b.cfg.InstanceName)
	}
	if !environment.Active() {
		t.Fatal("direnv should be active after switching boards")
	}
}

func TestBoardSessionLoadBoardRestoresEnvironmentOnConfigFailure(t *testing.T) {
	t.Setenv("KBRD_SWITCH_TEST", "old-board")
	environment := testDirenvManager(t, `{"KBRD_SWITCH_TEST":"new-board"}`)
	b := NewBoardWithOptions(config.Config{
		Path:          t.TempDir(),
		NotifyBackend: "none",
	}, BoardOptions{Environment: environment})

	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, config.FolderConfigFile), []byte("not = [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.session().loadBoard(target); err == nil {
		t.Fatal("loadBoard should reject invalid config")
	}
	if got := os.Getenv("KBRD_SWITCH_TEST"); got != "old-board" {
		t.Fatalf("KBRD_SWITCH_TEST = %q, want restored old-board", got)
	}
}

func TestBoardSessionSafeModeSkipsDirenvAndSurvivesSwitch(t *testing.T) {
	clearTestEnv(t, "KBRD_SWITCH_TEST", "DIRENV_DIFF")
	environment := testDirenvManager(t, `{"KBRD_SWITCH_TEST":"should-not-load","DIRENV_DIFF":"should-not-load"}`)
	b := NewBoardWithOptions(config.Config{
		Path:          t.TempDir(),
		InstanceName:  "safe-instance",
		NotifyBackend: "none",
	}, BoardOptions{Safe: true, Environment: environment})
	t.Cleanup(b.Close)

	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, config.FolderConfigFile), []byte(`
[scripting]
enabled = true
[hooks]
enabled = true
[template]
exec = true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.session().loadBoard(target); err != nil {
		t.Fatalf("loadBoard: %v", err)
	}
	if _, set := os.LookupEnv("KBRD_SWITCH_TEST"); set {
		t.Fatal("safe mode should not apply direnv environment")
	}
	if b.cfg.InstanceName != "safe-instance" {
		t.Fatalf("instance name = %q, want safe-instance", b.cfg.InstanceName)
	}
	if b.cfg.Scripting.Enabled || b.cfg.Hooks.Enabled || b.cfg.Template.Exec {
		t.Fatalf("safe mode overrides not preserved: scripting=%v hooks=%v template=%v",
			b.cfg.Scripting.Enabled, b.cfg.Hooks.Enabled, b.cfg.Template.Exec)
	}
}

func testDirenvManager(t *testing.T, output string) *boardenv.Manager {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "direnv")
	script := "#!/bin/sh\nprintf '%s' \"$FAKE_DIRENV_OUTPUT\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake direnv: %v", err)
	}
	t.Setenv("FAKE_DIRENV_OUTPUT", output)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return boardenv.New()
}

func clearTestEnv(t *testing.T, keys ...string) {
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
			t.Fatal(err)
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
