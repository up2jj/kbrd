package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/config"
)

// stubNotifier satisfies the Notifier interface; it returns non-nil cmds so
// tests can assert that error paths produce a notification.
type stubNotifier struct{}

func (stubNotifier) Success(string) tea.Cmd { return func() tea.Msg { return nil } }
func (stubNotifier) Error(string) tea.Cmd   { return func() tea.Msg { return nil } }

func newTestController(repoRoot string) Controller {
	return Controller{
		cfg:      config.Config{},
		notifier: stubNotifier{},
		repoRoot: repoRoot,
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func initSyncRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	seed := filepath.Join(dir, "seed.md")
	if err := os.WriteFile(seed, []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "initial")
	return dir
}

func TestShouldAutoSync_NoRepoRoot(t *testing.T) {
	c := newTestController("")
	if c.shouldAutoSync() {
		t.Fatal("expected false when repoRoot is empty")
	}
}

func TestShouldAutoSync_AlreadySyncing(t *testing.T) {
	dir := initSyncRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	c := newTestController(dir)
	c.syncing = true
	if c.shouldAutoSync() {
		t.Fatal("expected false when syncing is true")
	}
}

func TestShouldAutoSync_NoRemote(t *testing.T) {
	dir := initSyncRepo(t)
	c := newTestController(dir)
	if c.shouldAutoSync() {
		t.Fatal("expected false when no remote configured")
	}
}

func TestShouldAutoSync_DirtyTree(t *testing.T) {
	dir := initSyncRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	if err := os.WriteFile(filepath.Join(dir, "dirty.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := newTestController(dir)
	if c.shouldAutoSync() {
		t.Fatal("expected false when working tree is dirty")
	}
}

func TestShouldAutoSync_Ready(t *testing.T) {
	dir := initSyncRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	c := newTestController(dir)
	if !c.shouldAutoSync() {
		t.Fatal("expected true when repo has remote and clean tree")
	}
}

func TestHandleAutoSyncDone_ClearsFlag_Success(t *testing.T) {
	c := newTestController("")
	c.syncing = true
	if c.handleAutoSyncDone(autoSyncDoneMsg{Stage: "push"}); c.syncing {
		t.Fatal("expected syncing cleared after success")
	}
}

func TestHandleAutoSyncDone_ClearsFlag_Error(t *testing.T) {
	c := newTestController("")
	c.syncing = true
	cmd := c.handleAutoSyncDone(autoSyncDoneMsg{Stage: "pull", Err: errors.New("boom")})
	if c.syncing {
		t.Fatal("expected syncing cleared after error")
	}
	if cmd == nil {
		t.Fatal("expected an error notification cmd on failure")
	}
}

func TestHandleAutoSyncDone_ShutdownPending_Signals(t *testing.T) {
	c := newTestController("")
	c.syncing = true
	c.shutdownPending = true
	called := false
	c.onSyncSettled = func() tea.Cmd { called = true; return tea.Quit }
	cmd := c.handleAutoSyncDone(autoSyncDoneMsg{Stage: "push"})
	if c.syncing {
		t.Fatal("expected syncing cleared")
	}
	if !called || cmd == nil {
		t.Fatal("expected OnSyncSettled to be invoked when shutdown pending")
	}
}

func TestSyncOnce_TimeoutClearsHungGit(t *testing.T) {
	dir := initSyncRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://example.com/x.git")

	fakeDir := t.TempDir()
	fakeGit := filepath.Join(fakeDir, "git")
	script := `#!/bin/sh
if [ "$1" = "--no-optional-locks" ]; then
	shift
fi
if [ "$1" = "-C" ]; then
	shift 2
fi
case "$1" in
	remote)
		echo origin
		exit 0
		;;
	status)
		exit 0
		;;
	fetch)
		exec sleep 5
		;;
	*)
		echo "unexpected fake git command: $*" >&2
		exit 1
		;;
esac
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	oldTimeout := autoSyncGitTimeout
	autoSyncGitTimeout = 20 * time.Millisecond
	t.Cleanup(func() { autoSyncGitTimeout = oldTimeout })

	c := newTestController(dir)
	cmd := c.SyncOnce()
	if cmd == nil {
		t.Fatal("expected sync command")
	}
	if !c.syncing {
		t.Fatal("expected syncing flag set while command runs")
	}

	msg, ok := cmd().(autoSyncDoneMsg)
	if !ok {
		t.Fatalf("expected autoSyncDoneMsg, got %T", msg)
	}
	if msg.Err == nil {
		t.Fatal("expected hung git to time out")
	}
	if !strings.Contains(msg.Err.Error(), "timed out after") {
		t.Fatalf("expected timeout error, got %v", msg.Err)
	}

	c.handleAutoSyncDone(msg)
	if c.syncing {
		t.Fatal("expected syncing cleared after timeout")
	}
}

func TestHandleGitAddRemote_AddsOrigin(t *testing.T) {
	dir := initSyncRepo(t)
	c := newTestController(dir)
	_ = c.handleGitAddRemote(gitAddRemoteRequestMsg{URL: "https://example.com/x.git"})

	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		t.Fatalf("origin not set: %v", err)
	}
	if got := string(out); got == "" || got[:19] != "https://example.com" {
		t.Fatalf("origin url: got %q", got)
	}
}

func TestHandleGitAddRemote_EmptyURL(t *testing.T) {
	c := newTestController("")
	cmd := c.handleGitAddRemote(gitAddRemoteRequestMsg{URL: "   "})
	if cmd == nil {
		t.Fatal("expected error notification for empty URL")
	}
}

func TestScheduleAutoSync_DisabledReturnsNil(t *testing.T) {
	c := newTestController("")
	if cmd := c.scheduleAutoSync(); cmd != nil {
		t.Fatal("expected nil cmd when interval is zero")
	}
}
