package model

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"kbrd/config"
)

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

func newTestBoard(repoRoot string) *Board {
	return &Board{
		cfg:         config.Config{},
		notifier:    NewNotifier("none"),
		gitRepoRoot: repoRoot,
	}
}

func TestShouldAutoSync_NoRepoRoot(t *testing.T) {
	b := newTestBoard("")
	if b.shouldAutoSync() {
		t.Fatal("expected false when gitRepoRoot is empty")
	}
}

func TestShouldAutoSync_AlreadySyncing(t *testing.T) {
	dir := initSyncRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	b := newTestBoard(dir)
	b.gitSyncing = true
	if b.shouldAutoSync() {
		t.Fatal("expected false when gitSyncing is true")
	}
}

func TestShouldAutoSync_NoRemote(t *testing.T) {
	dir := initSyncRepo(t)
	b := newTestBoard(dir)
	if b.shouldAutoSync() {
		t.Fatal("expected false when no remote configured")
	}
}

func TestShouldAutoSync_DirtyTree(t *testing.T) {
	dir := initSyncRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	if err := os.WriteFile(filepath.Join(dir, "dirty.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := newTestBoard(dir)
	if b.shouldAutoSync() {
		t.Fatal("expected false when working tree is dirty")
	}
}

func TestShouldAutoSync_Ready(t *testing.T) {
	dir := initSyncRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	b := newTestBoard(dir)
	if !b.shouldAutoSync() {
		t.Fatal("expected true when repo has remote and clean tree")
	}
}

func TestHandleAutoSyncDone_ClearsFlag_Success(t *testing.T) {
	b := newTestBoard("")
	b.gitSyncing = true
	if _, _ = b.handleAutoSyncDone(autoSyncDoneMsg{Stage: "push"}); b.gitSyncing {
		t.Fatal("expected gitSyncing cleared after success")
	}
}

func TestHandleAutoSyncDone_ClearsFlag_Error(t *testing.T) {
	b := newTestBoard("")
	b.gitSyncing = true
	_, cmd := b.handleAutoSyncDone(autoSyncDoneMsg{Stage: "pull", Err: errors.New("boom"), Output: "fail"})
	if b.gitSyncing {
		t.Fatal("expected gitSyncing cleared after error")
	}
	if cmd == nil {
		t.Fatal("expected an error notification cmd on failure")
	}
}

func TestHandleGitAddRemote_AddsOrigin(t *testing.T) {
	dir := initSyncRepo(t)
	b := newTestBoard(dir)
	_, _ = b.handleGitAddRemote(gitAddRemoteRequestMsg{URL: "https://example.com/x.git"})

	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		t.Fatalf("origin not set: %v", err)
	}
	if got := string(out); got == "" || got[:19] != "https://example.com" {
		t.Fatalf("origin url: got %q", got)
	}
}

func TestHandleGitAddRemote_EmptyURL(t *testing.T) {
	b := newTestBoard("")
	_, cmd := b.handleGitAddRemote(gitAddRemoteRequestMsg{URL: "   "})
	if cmd == nil {
		t.Fatal("expected error notification for empty URL")
	}
}

func TestScheduleAutoSync_DisabledReturnsNil(t *testing.T) {
	b := newTestBoard("")
	if c := b.scheduleAutoSync(); c != nil {
		t.Fatal("expected nil cmd when interval is zero")
	}
}
