package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"kbrd/config"
	kbrdfs "kbrd/fs"
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

func TestGitPanelHandleMouseScrollsRightViewport(t *testing.T) {
	var p GitPanel
	p.Open("", "main", false, []kbrdfs.FileChange{{Path: "task.md", Status: " M"}}, 120, 30)

	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i)
	}
	p.SetLog(strings.Join(lines, "\n"))

	if p.right.YOffset() != 0 {
		t.Fatalf("initial right viewport offset = %d, want 0", p.right.YOffset())
	}

	p.HandleMouse(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if p.right.YOffset() != 3 {
		t.Fatalf("right viewport offset after wheel down = %d, want 3", p.right.YOffset())
	}

	p.HandleMouse(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if p.right.YOffset() != 0 {
		t.Fatalf("right viewport offset after wheel up = %d, want 0", p.right.YOffset())
	}

	p.HandleMouse(tea.MouseClickMsg{Button: tea.MouseLeft})
	if p.right.YOffset() != 0 {
		t.Fatalf("right viewport offset after non-wheel mouse = %d, want 0", p.right.YOffset())
	}
}

func TestGitPanelRightPaneScrollbarDoesNotWrap(t *testing.T) {
	var p GitPanel
	p.Open("", "main", false, []kbrdfs.FileChange{{Path: "task.md", Status: " M"}}, 120, 30)

	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i) + " " + strings.Repeat("wide ", 20)
	}
	p.SetLog(strings.Join(lines, "\n"))

	view := ansi.Strip(p.View())
	if !strings.Contains(view, "task.md") {
		t.Fatalf("panel left pane did not render changed file row\n%s", view)
	}
	gotH := lipgloss.Height(view)
	const wantH = 24
	if gotH != wantH {
		t.Fatalf("panel height = %d, want %d; right pane likely wrapped\n%s", gotH, wantH, view)
	}

	width := -1
	for line := range strings.SplitSeq(view, "\n") {
		w := lipgloss.Width(line)
		if width < 0 {
			width = w
			continue
		}
		if w != width {
			t.Fatalf("panel line width = %d, want %d for line %q\n%s", w, width, line, view)
		}
	}
}

func TestShouldAutoSync_NoRepoRoot(t *testing.T) {
	c := newTestController("")
	c.cfg.GitAutoSyncInterval = time.Minute
	if c.shouldAutoSync() {
		t.Fatal("expected false when repoRoot is empty")
	}
}

func TestLineChanges_NoRepoRoot(t *testing.T) {
	c := newTestController("")
	if got := c.LineChanges(filepath.Join(t.TempDir(), "task.md")); got != nil {
		t.Fatalf("LineChanges without repo root = %+v, want nil", got)
	}
}

func TestLineChanges_ChangedFile(t *testing.T) {
	dir := initSyncRepo(t)
	path := filepath.Join(dir, "seed.md")
	if err := os.WriteFile(path, []byte("seed\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := newTestController(dir)
	got := c.LineChanges(path)
	want := []LineChange{{Line: 2, Kind: LineAdded}}
	if !sameLineChanges(got, want) {
		t.Fatalf("LineChanges = %+v, want %+v", got, want)
	}
}

func TestLineChanges_ModifiedAndAdded(t *testing.T) {
	dir := initSyncRepo(t)
	path := filepath.Join(dir, "seed.md")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "expand seed")

	if err := os.WriteFile(path, []byte("one\nTWO\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := newTestController(dir)
	got := c.LineChanges(path)
	want := []LineChange{{Line: 2, Kind: LineModified}, {Line: 4, Kind: LineAdded}}
	if !sameLineChanges(got, want) {
		t.Fatalf("LineChanges = %+v, want %+v", got, want)
	}
}

func TestLineChanges_DeletionAnchorsNextSurvivingLine(t *testing.T) {
	dir := initSyncRepo(t)
	path := filepath.Join(dir, "seed.md")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "expand seed")

	if err := os.WriteFile(path, []byte("one\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := newTestController(dir)
	got := c.LineChanges(path)
	want := []LineChange{{Line: 2, Kind: LineDeleted}}
	if !sameLineChanges(got, want) {
		t.Fatalf("LineChanges = %+v, want %+v", got, want)
	}
}

func TestLineChanges_DeletionAtEOFAnchorsLastLine(t *testing.T) {
	dir := initSyncRepo(t)
	path := filepath.Join(dir, "seed.md")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "expand seed")

	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := newTestController(dir)
	got := c.LineChanges(path)
	want := []LineChange{{Line: 2, Kind: LineDeleted}}
	if !sameLineChanges(got, want) {
		t.Fatalf("LineChanges = %+v, want %+v", got, want)
	}
}

func TestLineChanges_StagedChange(t *testing.T) {
	dir := initSyncRepo(t)
	path := filepath.Join(dir, "seed.md")
	if err := os.WriteFile(path, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "seed.md")

	c := newTestController(dir)
	got := c.LineChanges(path)
	want := []LineChange{{Line: 1, Kind: LineModified}}
	if !sameLineChanges(got, want) {
		t.Fatalf("LineChanges = %+v, want %+v", got, want)
	}
}

func TestLineChanges_UntrackedIsAllAdded(t *testing.T) {
	dir := initSyncRepo(t)
	path := filepath.Join(dir, "new.md")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := newTestController(dir)
	got := c.LineChanges(path)
	want := []LineChange{{Line: 1, Kind: LineAdded}, {Line: 2, Kind: LineAdded}}
	if !sameLineChanges(got, want) {
		t.Fatalf("LineChanges = %+v, want %+v", got, want)
	}
}

func TestLineChanges_OptionalGitInputs(t *testing.T) {
	dir := initSyncRepo(t)
	path := filepath.Join(dir, "seed.md")
	empty := newTestController("")
	if got := empty.LineChanges(path); got != nil {
		t.Fatalf("empty repo root changes = %+v, want nil", got)
	}
	nonRepo := newTestController(t.TempDir())
	if got := nonRepo.LineChanges(path); got != nil {
		t.Fatalf("non-repo changes = %+v, want nil", got)
	}
}

func sameLineChanges(got, want []LineChange) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestShouldAutoSync_AlreadySyncing(t *testing.T) {
	dir := initSyncRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	c := newTestController(dir)
	c.cfg.GitAutoSyncInterval = time.Minute
	c.syncing = true
	if c.shouldAutoSync() {
		t.Fatal("expected false when syncing is true")
	}
}

func TestShouldAutoSync_NoRemote(t *testing.T) {
	dir := initSyncRepo(t)
	c := newTestController(dir)
	c.cfg.GitAutoSyncInterval = time.Minute
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
	c.cfg.GitAutoSyncInterval = time.Minute
	if c.shouldAutoSync() {
		t.Fatal("expected false when working tree is dirty")
	}
}

func TestShouldAutoSync_EditorActive(t *testing.T) {
	dir := initSyncRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	c := newTestController(dir)
	c.cfg.GitAutoSyncInterval = time.Minute
	c.editorActive = func() bool { return true }
	if c.shouldAutoSync() {
		t.Fatal("expected false when editor is active")
	}
}

func TestShouldAutoSync_Ready(t *testing.T) {
	dir := initSyncRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	c := newTestController(dir)
	c.cfg.GitAutoSyncInterval = time.Minute
	if !c.shouldAutoSync() {
		t.Fatal("expected true when repo has remote and clean tree")
	}
}

func TestStartupSyncOnce_RunsWhenRecurringSyncDisabled(t *testing.T) {
	dir := initSyncRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	c := newTestController(dir)
	if cmd := c.SyncOnce(); cmd != nil {
		t.Fatal("recurring sync should stay disabled without an interval")
	}
	if cmd := c.StartupSyncOnce(); cmd == nil {
		t.Fatal("startup sync should not be gated by the recurring interval")
	}
	if !c.syncing {
		t.Fatal("startup sync should mark the controller busy")
	}
}

func TestManualSyncBlocksOverlapAndSettles(t *testing.T) {
	c := newTestController(t.TempDir())
	c.cfg.GitManualSyncMode = "auto"
	if cmd := c.handleGitSync(); cmd == nil {
		t.Fatal("expected manual sync command")
	}
	if !c.syncing {
		t.Fatal("manual sync should mark the controller busy")
	}
	if cmd := c.handleGitSync(); cmd == nil {
		t.Fatal("overlapping manual sync should produce a notification")
	}
	if cmd := c.handleGitSyncStep(gitSyncStepMsg{Stage: "sync"}); cmd == nil {
		t.Fatal("successful manual sync should notify")
	}
	if c.syncing {
		t.Fatal("manual sync should clear the busy guard when settled")
	}
}

func TestShouldAutoSync_NilEditorActivePreservesReady(t *testing.T) {
	dir := initSyncRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	c := newTestController(dir)
	c.cfg.GitAutoSyncInterval = time.Minute
	if c.editorActive != nil {
		t.Fatal("test controller should not set editorActive")
	}
	if !c.shouldAutoSync() {
		t.Fatal("expected true when editorActive is nil and repo is otherwise ready")
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

func TestSyncState_ExpiresStaleAutoSync(t *testing.T) {
	c := newTestController("")
	c.hasRemote = true
	c.syncing = true
	c.syncDeadline = time.Now().Add(-time.Second)
	c.activeSyncSeq = 7

	state := c.SyncState()
	if state.Syncing {
		t.Fatal("expected stale sync to be cleared")
	}
	if !state.Failed {
		t.Fatal("expected stale sync to be marked failed")
	}
	if c.activeSyncSeq != 0 {
		t.Fatal("expected active sync sequence to be cleared")
	}
}

func TestShouldAutoSync_ExpiredSyncCanRecover(t *testing.T) {
	dir := initSyncRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	c := newTestController(dir)
	c.cfg.GitAutoSyncInterval = time.Minute
	c.syncing = true
	c.syncDeadline = time.Now().Add(-time.Second)
	c.activeSyncSeq = 3

	if !c.shouldAutoSync() {
		t.Fatal("expected expired sync state to allow another auto-sync")
	}
}

func TestHandleAutoSyncDone_IgnoresLateStaleResult(t *testing.T) {
	c := newTestController("")
	c.syncing = true
	c.syncDeadline = time.Now().Add(time.Minute)
	c.activeSyncSeq = 2

	cmd := c.handleAutoSyncDone(autoSyncDoneMsg{Seq: 1, Stage: "push"})
	if cmd != nil {
		t.Fatal("expected no command for stale result")
	}
	if !c.syncing {
		t.Fatal("expected current sync to remain active")
	}
	if c.lastSyncFailed {
		t.Fatal("stale success must not alter current sync outcome")
	}
}

func TestHandleAutoSyncDone_IgnoresCompletionAfterExpiry(t *testing.T) {
	c := newTestController("")
	c.hasRemote = true
	c.syncing = true
	c.syncDeadline = time.Now().Add(-time.Second)
	c.activeSyncSeq = 4

	_ = c.SyncState()
	cmd := c.handleAutoSyncDone(autoSyncDoneMsg{Seq: 4, Stage: "push"})
	if cmd != nil {
		t.Fatal("expected no command for expired sync result")
	}
	if !c.lastSyncFailed {
		t.Fatal("expired sync result must not overwrite failed state")
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
	c.cfg.GitAutoSyncInterval = time.Minute
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

func TestSyncOnce_SkipsPushWhenAlreadySynced(t *testing.T) {
	dir := initSyncRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	fakeDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "push-called")
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
	status|fetch|merge)
		exit 0
		;;
	rev-list)
		echo 0
		exit 0
		;;
	push)
		touch "$PUSH_MARKER"
		exit 0
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
	t.Setenv("PUSH_MARKER", marker)

	c := newTestController(dir)
	c.cfg.GitAutoSyncInterval = time.Minute
	cmd := c.SyncOnce()
	if cmd == nil {
		t.Fatal("expected auto-sync command")
	}
	msg, ok := cmd().(autoSyncDoneMsg)
	if !ok {
		t.Fatalf("expected autoSyncDoneMsg, got %T", msg)
	}
	if msg.Err != nil {
		t.Fatalf("auto-sync failed: %v", msg.Err)
	}
	if msg.Stage != "merge" {
		t.Fatalf("stage = %q, want merge when push is skipped", msg.Stage)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("push ran despite no commits ahead: stat err=%v", err)
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
