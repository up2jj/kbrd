package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/config"
	"kbrd/template"
)

func TestTemplateExecDispatchDisabled(t *testing.T) {
	te := &templateExec{notifier: NewNotifier("none")}
	body := "A\n" + template.RenderShellMarker(1, "echo hi", "") + "\nB"
	out, cmd := te.dispatch("/tmp/card.md", body, "/tmp", config.TemplateConfig{Exec: false})
	if cmd != nil {
		t.Error("disabled exec must not return a command")
	}
	if te.Inflight() != 0 {
		t.Errorf("inflight = %d, want 0", te.Inflight())
	}
	if len(template.ParseShellMarkers(out)) != 0 {
		t.Errorf("markers should be gone: %q", out)
	}
	if !strings.Contains(out, "exec disabled") {
		t.Errorf("missing disabled note: %q", out)
	}
}

func TestTemplateExecDispatchEnabled(t *testing.T) {
	// Seed nextID so the reassigned id is observably different from the
	// original per-render id=1.
	te := &templateExec{notifier: NewNotifier("none"), nextID: 10}
	body := template.RenderShellMarker(1, "echo hi", "stdin")
	out, cmd := te.dispatch("/tmp/card.md", body, "/tmp", config.TemplateConfig{Exec: true, CommandTimeoutMs: 5000})
	if cmd == nil {
		t.Fatal("enabled exec must return a command")
	}
	if te.Inflight() != 1 {
		t.Errorf("inflight = %d, want 1", te.Inflight())
	}
	// The marker survives (for the worker to replace) but is reassigned a
	// session-monotonic id (11), not the original per-render id=1.
	markers := template.ParseShellMarkers(out)
	if len(markers) != 1 || markers[0].ID != 11 {
		t.Errorf("expected one marker with reassigned id 11, got %+v", markers)
	}
	if markers[0].Cmd != "echo hi" || markers[0].Stdin != "stdin" {
		t.Errorf("marker payload lost: %+v", markers[0])
	}
}

func TestTemplateExecDoneReplacesMarker(t *testing.T) {
	dir := t.TempDir()
	card := filepath.Join(dir, "card.md")
	body := "# Title\n\n## Out\n" + template.RenderShellMarker(5, "echo", "") + "\n"
	if err := os.WriteFile(card, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	te := &templateExec{notifier: NewNotifier("none"), inflight: 1}
	te.done(templateShellDoneMsg{CardPath: card, ID: 5, Output: "hello world\n"})

	if te.Inflight() != 0 {
		t.Errorf("inflight = %d, want 0", te.Inflight())
	}
	data, _ := os.ReadFile(card)
	got := string(data)
	if len(template.ParseShellMarkers(got)) != 0 {
		t.Errorf("marker not replaced: %q", got)
	}
	if !strings.Contains(got, "hello world") {
		t.Errorf("output not inserted: %q", got)
	}
	if !strings.HasPrefix(got, "# Title") {
		t.Errorf("surrounding content disturbed: %q", got)
	}
}

func TestTemplateExecDoneErrorNote(t *testing.T) {
	dir := t.TempDir()
	card := filepath.Join(dir, "card.md")
	os.WriteFile(card, []byte(template.RenderShellMarker(1, "x", "")+"\n"), 0o644)
	te := &templateExec{notifier: NewNotifier("none"), inflight: 1}
	te.done(templateShellDoneMsg{CardPath: card, ID: 1, ExitCode: 3, Output: "boom"})
	data, _ := os.ReadFile(card)
	if !strings.Contains(string(data), "exited 3") {
		t.Errorf("missing exit note: %q", data)
	}
}

func TestTemplateExecRecover(t *testing.T) {
	board := t.TempDir()
	col := filepath.Join(board, "todo")
	os.MkdirAll(col, 0o755)
	card := filepath.Join(col, "task.md")
	os.WriteFile(card, []byte("# Task\n"+template.RenderShellMarker(2, "slow", "")+"\n"), 0o644)
	// A card with no marker must be left untouched.
	plain := filepath.Join(col, "plain.md")
	os.WriteFile(plain, []byte("# Plain\n"), 0o644)

	te := &templateExec{notifier: NewNotifier("none")}
	te.recover(board)

	data, _ := os.ReadFile(card)
	if len(template.ParseShellMarkers(string(data))) != 0 {
		t.Errorf("stale marker not recovered: %q", data)
	}
	if !strings.Contains(string(data), "interrupted") {
		t.Errorf("missing interrupted note: %q", data)
	}
	if p, _ := os.ReadFile(plain); string(p) != "# Plain\n" {
		t.Errorf("plain card altered: %q", p)
	}
}
