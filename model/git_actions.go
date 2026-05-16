package model

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	kbrdfs "kbrd/fs"
)

type gitSyncStepMsg struct {
	Stage    string // "pull" or "push"
	Err      error
	Output   string
	ThenPush bool
}

type gitPostCommitMsg struct {
	Output   string
	Err      error
	ThenSync bool
}

func (b *Board) openGitPanel() tea.Cmd {
	if !kbrdfs.GitAvailable() {
		return b.notifier.Send("git not installed", notifyError)
	}
	var initToast tea.Cmd
	if b.gitRepoRoot == "" {
		if err := kbrdfs.GitInit(b.cfg.Path); err != nil {
			return b.notifier.Send("git init failed: "+err.Error(), notifyError)
		}
		b.gitRepoRoot = kbrdfs.GitRepoRoot(b.cfg.Path)
		if b.gitRepoRoot == "" {
			return b.notifier.Send("git init succeeded but repo not detected", notifyError)
		}
		initToast = b.notifier.Send("initialized git repo", notifySuccess)
	}
	branch := kbrdfs.GitCurrentBranch(b.gitRepoRoot)
	hasRemote := kbrdfs.GitHasRemote(b.gitRepoRoot)
	files := kbrdfs.GitChangedFiles(b.gitRepoRoot)
	b.gitPanel.Open(b.gitRepoRoot, branch, hasRemote, files, b.termWidth, b.termHeight)
	diffCmd := b.gitPanel.DiffRequestForCurrent()
	if initToast == nil {
		return diffCmd
	}
	if diffCmd == nil {
		return initToast
	}
	return tea.Batch(initToast, diffCmd)
}

func (b *Board) refreshGitPanel() {
	if !b.gitPanel.Active() {
		return
	}
	branch := kbrdfs.GitCurrentBranch(b.gitRepoRoot)
	hasRemote := kbrdfs.GitHasRemote(b.gitRepoRoot)
	files := kbrdfs.GitChangedFiles(b.gitRepoRoot)
	b.gitPanel.Refresh(branch, hasRemote, files, b.termWidth, b.termHeight)
}

func (b *Board) handleGitDiffForFile(msg gitDiffForFileMsg) (tea.Model, tea.Cmd) {
	text := b.runFileDiff(msg.Path, msg.Status, msg.OrigPath)
	b.gitPanel.SetDiffForFile(msg.Path, text)
	return b, nil
}

func (b *Board) runFileDiff(path, status, origPath string) string {
	tool := resolveDiffTool(b.cfg.GitDiffTool)

	// Untracked: diff against /dev/null so the new file shows as additions.
	if status == "??" {
		return b.runGitDiff(tool, []string{"diff", "--no-index", "--", "/dev/null", path}, "(empty file)")
	}

	args := []string{"diff", "HEAD", "--"}
	if origPath != "" {
		args = append(args, origPath, path)
	} else {
		args = append(args, path)
	}
	return b.runGitDiff(tool, args, "(no changes)")
}

func (b *Board) runGitDiff(tool string, diffArgs []string, emptyText string) string {
	args := []string{"--no-optional-locks", "-C", b.gitRepoRoot}
	if tool != "difft" {
		args = append(args, "-c", "color.ui=always")
	}
	args = append(args, diffArgs...)
	cmd := exec.Command("git", args...)
	if tool == "difft" {
		width := b.termWidth - 8
		if width < 40 {
			width = 40
		}
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

func (b *Board) handleGitCommit(msg gitCommitRequestMsg) (tea.Model, tea.Cmd) {
	commitMsg := strings.TrimSpace(msg.Message)
	if commitMsg == "" {
		return b, b.notifier.Send("commit message is empty", notifyError)
	}
	if err := exec.Command("git", "-C", b.gitRepoRoot, "add", ".").Run(); err != nil {
		return b, b.notifier.Send("git add failed: "+err.Error(), notifyError)
	}
	thenSync := msg.ThenSync
	return b, func() tea.Msg {
		out, err := exec.Command("git", "-C", b.gitRepoRoot, "commit", "-m", commitMsg).CombinedOutput()
		return gitPostCommitMsg{Output: string(out), Err: err, ThenSync: thenSync}
	}
}

func (b *Board) handleGitPostCommit(msg gitPostCommitMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		b.refreshGitStats()
		b.refreshGitPanel()
		detail := strings.TrimSpace(msg.Output)
		if detail == "" {
			detail = msg.Err.Error()
		}
		return b, b.notifier.Send("commit failed: "+detail, notifyError)
	}
	b.refreshGitStats()
	b.refreshGitPanel()
	cmds := []tea.Cmd{b.notifier.Send("commit ok", notifySuccess)}
	if c := b.gitPanel.DiffRequestForCurrent(); c != nil {
		cmds = append(cmds, c)
	}
	if msg.ThenSync {
		cmds = append(cmds, func() tea.Msg { return gitContinueSyncMsg{} })
	}
	return b, tea.Batch(cmds...)
}

func (b *Board) handleGitLog() (tea.Model, tea.Cmd) {
	out, err := exec.Command("git", "--no-optional-locks", "-C", b.gitRepoRoot,
		"log", "--pretty=format:%h %as %an %s", "--date=short", "-n", "50").CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return b, b.notifier.Send("git log failed: "+detail, notifyError)
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		text = "(no commits yet)"
	}
	b.gitPanel.SetLog(text)
	return b, nil
}

func (b *Board) handleGitSync() (tea.Model, tea.Cmd) {
	c := exec.Command("git", "-C", b.gitRepoRoot, "pull", "--ff-only")
	return b, tea.ExecProcess(c, func(err error) tea.Msg {
		return gitSyncStepMsg{Stage: "pull", Err: err, ThenPush: err == nil}
	})
}

func (b *Board) handleGitSyncStep(msg gitSyncStepMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		b.refreshGitStats()
		b.refreshGitPanel()
		return b, b.notifier.Send("git "+msg.Stage+" failed: "+msg.Err.Error(), notifyError)
	}
	if msg.Stage == "pull" && msg.ThenPush {
		c := exec.Command("git", "-C", b.gitRepoRoot, "push")
		return b, tea.ExecProcess(c, func(err error) tea.Msg {
			return gitSyncStepMsg{Stage: "push", Err: err}
		})
	}
	b.refreshGitStats()
	b.refreshGitPanel()
	return b, b.notifier.Send("sync ok", notifySuccess)
}

type autoSyncTickMsg struct{}

type autoSyncDoneMsg struct {
	Stage  string // "pull" or "push"
	Err    error
	Output string
}

func (b *Board) scheduleAutoSync() tea.Cmd {
	if b.cfg.GitAutoSyncInterval <= 0 {
		return nil
	}
	return tea.Tick(b.cfg.GitAutoSyncInterval, func(time.Time) tea.Msg {
		return autoSyncTickMsg{}
	})
}

func (b *Board) shouldAutoSync() bool {
	if b.gitSyncing {
		return false
	}
	if !kbrdfs.GitAvailable() || b.gitRepoRoot == "" {
		return false
	}
	if !kbrdfs.GitHasRemote(b.gitRepoRoot) {
		return false
	}
	if !kbrdfs.GitWorkingTreeClean(b.gitRepoRoot) {
		return false
	}
	return true
}

func (b *Board) handleAutoSyncTick() (tea.Model, tea.Cmd) {
	reschedule := b.scheduleAutoSync()
	if !b.shouldAutoSync() {
		return b, reschedule
	}
	b.gitSyncing = true
	repoRoot := b.gitRepoRoot
	syncCmd := func() tea.Msg {
		pullOut, err := exec.Command("git", "-C", repoRoot, "pull", "--ff-only").CombinedOutput()
		if err != nil {
			return autoSyncDoneMsg{Stage: "pull", Err: err, Output: string(pullOut)}
		}
		pushOut, err := exec.Command("git", "-C", repoRoot, "push").CombinedOutput()
		if err != nil {
			return autoSyncDoneMsg{Stage: "push", Err: err, Output: string(pushOut)}
		}
		return autoSyncDoneMsg{Stage: "push", Output: string(pushOut)}
	}
	if reschedule == nil {
		return b, syncCmd
	}
	return b, tea.Batch(syncCmd, reschedule)
}

func (b *Board) handleAutoSyncDone(msg autoSyncDoneMsg) (tea.Model, tea.Cmd) {
	b.gitSyncing = false
	if msg.Err != nil {
		detail := strings.TrimSpace(msg.Output)
		if detail == "" {
			detail = msg.Err.Error()
		}
		return b, b.notifier.Send("auto-sync "+msg.Stage+" failed: "+detail, notifyError)
	}
	b.refreshGitStats()
	b.refreshGitPanel()
	return b, nil
}

func (b *Board) handleGitAddRemote(msg gitAddRemoteRequestMsg) (tea.Model, tea.Cmd) {
	url := strings.TrimSpace(msg.URL)
	if url == "" {
		return b, b.notifier.Send("remote URL is empty", notifyError)
	}
	out, err := exec.Command("git", "-C", b.gitRepoRoot, "remote", "add", "origin", url).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return b, b.notifier.Send("git remote add failed: "+detail, notifyError)
	}
	b.refreshGitPanel()
	return b, b.notifier.Send("remote 'origin' added", notifySuccess)
}

func (b *Board) handleGitRefresh() (tea.Model, tea.Cmd) {
	b.refreshGitStats()
	b.refreshGitPanel()
	return b, nil
}

func (b *Board) handleGitPanelClose() (tea.Model, tea.Cmd) {
	b.gitPanel.Close()
	b.refreshGitStats()
	return b, nil
}
