package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/events"
	kbrdfs "kbrd/fs"
)

type gitSyncStepMsg struct {
	gitMsg
	Stage    string // "pull", "push", or "sync" (one-shot merge reconcile)
	Err      error
	Output   string
	ThenPush bool
	Sidecars []string // conflict copies created by an "auto" reconcile
}

type gitPostCommitMsg struct {
	gitMsg
	Committed bool
	Err       error
	ThenSync  bool
}

// Open initializes the repo if needed, opens the panel, and requests the first
// diff. Triggered by the host's git-panel key.
func (c *Controller) Open() tea.Cmd {
	if !kbrdfs.GitAvailable() {
		return c.notifier.Error("git not installed")
	}
	var initToast tea.Cmd
	if c.repoRoot == "" {
		if err := kbrdfs.GitInit(c.cfg.Path); err != nil {
			return c.notifier.Error("git init failed: " + err.Error())
		}
		c.repoRoot = kbrdfs.GitRepoRoot(c.cfg.Path)
		if c.repoRoot == "" {
			return c.notifier.Error("git init succeeded but repo not detected")
		}
		initToast = c.notifier.Success("initialized git repo")
	}
	branch := kbrdfs.GitCurrentBranch(c.repoRoot)
	hasRemote := kbrdfs.GitHasRemote(c.repoRoot)
	c.hasRemote = hasRemote
	files := kbrdfs.GitChangedFiles(c.repoRoot)
	c.panel.Open(c.repoRoot, branch, hasRemote, files, c.termW, c.termH)
	diffCmd := c.panel.DiffRequestForCurrent()
	if initToast == nil {
		return diffCmd
	}
	if diffCmd == nil {
		return initToast
	}
	return tea.Batch(initToast, diffCmd)
}

func (c *Controller) refreshPanel() {
	if !c.panel.Active() {
		return
	}
	branch := kbrdfs.GitCurrentBranch(c.repoRoot)
	hasRemote := kbrdfs.GitHasRemote(c.repoRoot)
	files := kbrdfs.GitChangedFiles(c.repoRoot)
	c.panel.Refresh(branch, hasRemote, files, c.termW, c.termH)
}

func (c *Controller) handleGitDiffForFile(msg gitDiffForFileMsg) tea.Cmd {
	text := c.runFileDiff(msg.Path, msg.Status, msg.OrigPath)
	c.panel.SetDiffForFile(msg.Path, text)
	return nil
}

func (c *Controller) runFileDiff(path, status, origPath string) string {
	tool := resolveDiffTool(c.cfg.GitDiffTool)

	// Untracked: diff against /dev/null so the new file shows as additions.
	if status == "??" {
		return c.runGitDiff(tool, []string{"diff", "--no-index", "--", "/dev/null", path}, "(empty file)")
	}

	args := []string{"diff", "HEAD", "--"}
	if origPath != "" {
		args = append(args, origPath, path)
	} else {
		args = append(args, path)
	}
	return c.runGitDiff(tool, args, "(no changes)")
}

func (c *Controller) runGitDiff(tool string, diffArgs []string, emptyText string) string {
	args := []string{"--no-optional-locks"}
	if tool != "difft" {
		args = append(args, "-c", "color.ui=always")
	}
	args = append(args, diffArgs...)
	cmd := kbrdfs.GitCommand(c.repoRoot, args...)
	if tool == "difft" {
		width := max(c.termW-8, 40)
		cmd.Env = append(os.Environ(),
			"GIT_EXTERNAL_DIFF=difft",
			"DFT_COLOR=always",
			"DFT_DISPLAY=inline",
			"DFT_WIDTH="+strconv.Itoa(width),
		)
	}
	out, _ := cmd.CombinedOutput() // diff --no-index exits 1 on differences; ignore status
	if tool == "diff-so-fancy" {
		if path, err := exec.LookPath("diff-so-fancy"); err == nil {
			pc := exec.Command(path)
			pc.Stdin = bytes.NewReader(out)
			if pretty, perr := pc.Output(); perr == nil {
				out = pretty
			}
		}
	}
	text := strings.TrimRight(string(out), "\n")
	if strings.TrimSpace(text) == "" {
		text = emptyText
	}
	return text
}

func resolveDiffTool(pref string) string {
	switch pref {
	case "difft", "diff-so-fancy", "git":
		if pref == "git" {
			return "git"
		}
		if _, err := exec.LookPath(pref); err == nil {
			return pref
		}
		return "git"
	}
	if _, err := exec.LookPath("difft"); err == nil {
		return "difft"
	}
	if _, err := exec.LookPath("diff-so-fancy"); err == nil {
		return "diff-so-fancy"
	}
	return "git"
}

func (c *Controller) handleGitCommit(msg gitCommitRequestMsg) tea.Cmd {
	commitMsg := strings.TrimSpace(msg.Message)
	if commitMsg == "" {
		return c.notifier.Error("commit message is empty")
	}
	if c.beforeCommit != nil {
		if err := c.beforeCommit(); err != nil {
			return c.notifier.Error("pre-commit failed: " + err.Error())
		}
	}
	thenSync := msg.ThenSync
	root := c.repoRoot
	return func() tea.Msg {
		// Empty identity: interactive commits carry the user's own git config.
		committed, err := kbrdfs.GitCommitAll(root, commitMsg, "", "")
		return gitPostCommitMsg{Committed: committed, Err: err, ThenSync: thenSync}
	}
}

func (c *Controller) handleGitPostCommit(msg gitPostCommitMsg) tea.Cmd {
	if msg.Err != nil {
		c.refreshStats()
		c.refreshPanel()
		return c.notifier.Error(msg.Err.Error())
	}
	c.refreshStats()
	c.refreshPanel()
	if !msg.Committed {
		return c.notifier.Error("nothing to commit")
	}
	cmds := []tea.Cmd{c.notifier.Success("commit ok")}
	if cc := c.panel.DiffRequestForCurrent(); cc != nil {
		cmds = append(cmds, cc)
	}
	if msg.ThenSync {
		cmds = append(cmds, func() tea.Msg { return gitContinueSyncMsg{} })
	}
	return tea.Batch(cmds...)
}

func (c *Controller) handleGitLog() tea.Cmd {
	commits, err := kbrdfs.GitLog(c.repoRoot, 50)
	if err != nil {
		return c.notifier.Error(err.Error())
	}
	lines := make([]string, 0, len(commits))
	for _, commit := range commits {
		lines = append(lines, fmt.Sprintf("%s %s %s %s",
			commit.Short, commit.Time.Format(time.DateOnly), commit.Author, commit.Subject))
	}
	text := strings.Join(lines, "\n")
	if text == "" {
		text = "(no commits yet)"
	}
	c.panel.SetLog(text)
	return nil
}

// handleGitSync runs the manual sync, whose policy is set by
// git.manual_sync_mode. "attended" (default) does pull→push via ExecProcess so
// git owns the terminal and can prompt for credentials, and `--ff-only` fails
// loudly on divergence for the user to resolve. "auto" runs the same
// self-healing merge-with-sidecar reconcile the automatic flows use, off-thread.
func (c *Controller) handleGitSync() tea.Cmd {
	if c.cfg.GitManualSyncMode == "auto" {
		root := c.repoRoot
		label := c.conflictLabel()
		return func() tea.Msg {
			sidecars, err := kbrdfs.GitMergeResolveSidecar(root, label, "", "")
			if err != nil {
				return gitSyncStepMsg{Stage: "sync", Err: err}
			}
			return gitSyncStepMsg{Stage: "sync", Err: kbrdfs.GitPush(root), Sidecars: sidecars}
		}
	}
	cmd := kbrdfs.GitCommand(c.repoRoot, "pull", "--ff-only")
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return gitSyncStepMsg{Stage: "pull", Err: err, ThenPush: err == nil}
	})
}

func (c *Controller) handleGitSyncStep(msg gitSyncStepMsg) tea.Cmd {
	if msg.Err != nil {
		c.recordSyncOutcome(msg.Err, 0)
		c.refreshStats()
		c.refreshPanel()
		c.bus.Publish(events.GitSyncDone{OK: false, Stage: msg.Stage, Err: msg.Err.Error()})
		return tea.Batch(restoreMouse, c.notifier.Error("git "+msg.Stage+" failed: "+msg.Err.Error()))
	}
	if msg.Stage == "pull" && msg.ThenPush {
		cmd := kbrdfs.GitCommand(c.repoRoot, "push")
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			return gitSyncStepMsg{Stage: "push", Err: err}
		})
	}
	c.recordSyncOutcome(nil, len(msg.Sidecars))
	c.refreshStats()
	c.refreshPanel()
	c.bus.Publish(events.GitSyncDone{OK: true, Stage: msg.Stage})
	if n := len(msg.Sidecars); n > 0 {
		return tea.Batch(restoreMouse, c.notifier.Success("sync ok — "+conflictCopyNote(n)))
	}
	return tea.Batch(restoreMouse, c.notifier.Success("sync ok"))
}

// restoreMouse re-enables mouse tracking after a tea.ExecProcess. Bubble Tea's
// RestoreTerminal brings back the alt-screen and bracketed paste but not mouse
// reporting, so attended git steps (which hand the terminal to git) leave the
// board mouse-dead until we re-arm it here.
var restoreMouse tea.Cmd = tea.EnableMouseCellMotion

var autoSyncGitTimeout = 2 * time.Minute

type autoSyncTickMsg struct{ gitMsg }

type autoSyncDoneMsg struct {
	gitMsg
	Stage    string // "merge" or "push"
	Err      error
	Sidecars []string // conflict copies created during the reconcile
}

func (c *Controller) scheduleAutoSync() tea.Cmd {
	if c.cfg.GitAutoSyncInterval <= 0 {
		return nil
	}
	return tea.Tick(c.cfg.GitAutoSyncInterval, func(time.Time) tea.Msg {
		return autoSyncTickMsg{}
	})
}

func (c *Controller) shouldAutoSync() bool {
	if c.syncing {
		return false
	}
	if c.editorActive != nil && c.editorActive() {
		return false
	}
	if !kbrdfs.GitAvailable() || c.repoRoot == "" {
		return false
	}
	if !kbrdfs.GitHasRemote(c.repoRoot) {
		return false
	}
	// A merge can't run over uncommitted edits, so a dirty tree normally blocks
	// auto-sync. With auto_commit on, SyncOnce commits first, so dirty is allowed.
	if !c.cfg.GitAutoCommit && !kbrdfs.GitWorkingTreeClean(c.repoRoot) {
		return false
	}
	return true
}

// SyncOnce runs one guarded reconcile→push cycle off-thread, emitting
// autoSyncDoneMsg. It self-skips (returns nil) when a sync is not due, so any
// caller — the auto-sync ticker today, a background-task scheduler tomorrow —
// can drive it without knowing git's preconditions.
//
// Auto-sync self-heals like every automatic flow: GitMergeResolveSidecar merges
// the remote, auto-resolving true conflicts into sidecar copies (local wins)
// rather than failing loudly, then pushes so the merge — and any copies —
// propagate. It runs only when no in-app editor is active, and only on a clean
// tree unless git.auto_commit is set; with auto_commit, it commits pending edits
// first on ticks that happen after the editor closes.
func (c *Controller) SyncOnce() tea.Cmd {
	if !c.shouldAutoSync() {
		return nil
	}
	c.syncing = true
	root := c.repoRoot
	label := c.conflictLabel()
	autoCommit := c.cfg.GitAutoCommit
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), autoSyncGitTimeout)
		defer cancel()
		done := func(stage string, err error, sidecars []string) autoSyncDoneMsg {
			if err != nil && ctx.Err() != nil {
				err = fmt.Errorf("timed out after %s during %s", autoSyncGitTimeout, stage)
			}
			return autoSyncDoneMsg{Stage: stage, Err: err, Sidecars: sidecars}
		}
		if autoCommit {
			// Empty identity uses the user's git config, matching handleGitCommit.
			// A no-op on a clean tree. README regen (beforeCommit) is intentionally
			// skipped here — it would churn on every tick.
			if _, err := kbrdfs.GitCommitAllContext(ctx, root, "kbrd: auto-sync", "", ""); err != nil {
				return done("commit", err, nil)
			}
		}
		sidecars, err := kbrdfs.GitMergeResolveSidecarContext(ctx, root, label, "", "")
		if err != nil {
			return done("merge", err, nil)
		}
		return done("push", kbrdfs.GitPushContext(ctx, root), sidecars)
	}
}

// handleAutoSyncTick reschedules the next tick and kicks off one sync cycle.
// This thin wrapper is the only scheduling logic; a future registry would call
// SyncOnce directly and this (plus autoSyncTickMsg/scheduleAutoSync) is deleted.
func (c *Controller) handleAutoSyncTick() tea.Cmd {
	reschedule := c.scheduleAutoSync()
	sync := c.SyncOnce()
	switch {
	case sync == nil:
		return reschedule
	case reschedule == nil:
		return sync
	default:
		return tea.Batch(sync, reschedule)
	}
}

func (c *Controller) handleAutoSyncDone(msg autoSyncDoneMsg) tea.Cmd {
	c.syncing = false
	if c.shutdownPending {
		// A quit was deferred until this sync settled — signal the host.
		if c.onSyncSettled != nil {
			return c.onSyncSettled()
		}
		return nil
	}
	if msg.Err != nil {
		c.recordSyncOutcome(msg.Err, 0)
		c.bus.Publish(events.GitSyncDone{OK: false, Stage: msg.Stage, Err: msg.Err.Error()})
		return c.notifier.Error("auto-sync " + msg.Stage + " failed: " + msg.Err.Error())
	}
	c.recordSyncOutcome(nil, len(msg.Sidecars))
	c.refreshStats()
	c.refreshPanel()
	c.bus.Publish(events.GitSyncDone{OK: true, Stage: msg.Stage})
	if n := len(msg.Sidecars); n > 0 {
		return c.notifier.Success("auto-sync — " + conflictCopyNote(n))
	}
	return nil
}

// conflictLabel tags conflict-copy filenames: the instance name when set, else
// a timestamp so distinct conflicts don't collide.
func (c *Controller) conflictLabel() string {
	if c.cfg.InstanceName != "" {
		return c.cfg.InstanceName
	}
	return time.Now().Format("2006-01-02 1504")
}

// conflictCopyNote renders the "<n> conflict cop(y|ies) created" toast suffix.
func conflictCopyNote(n int) string {
	noun := "copy"
	if n > 1 {
		noun = "copies"
	}
	return fmt.Sprintf("%d conflict %s created", n, noun)
}

func (c *Controller) handleGitAddRemote(msg gitAddRemoteRequestMsg) tea.Cmd {
	url := strings.TrimSpace(msg.URL)
	if url == "" {
		return c.notifier.Error("remote URL is empty")
	}
	if err := kbrdfs.GitAddRemoteOrigin(c.repoRoot, url); err != nil {
		return c.notifier.Error(err.Error())
	}
	c.refreshRemote()
	c.refreshPanel()
	return c.notifier.Success("remote 'origin' added")
}

func (c *Controller) handleGitRefresh() tea.Cmd {
	c.refreshStats()
	c.refreshPanel()
	return nil
}

func (c *Controller) handleGitPanelClose() tea.Cmd {
	c.panel.Close()
	c.refreshStats()
	return nil
}
