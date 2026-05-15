package model

import (
	"os/exec"
	"strings"

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
	files := kbrdfs.GitChangedFiles(b.gitRepoRoot)
	b.gitPanel.Open(b.gitRepoRoot, branch, files, b.termWidth, b.termHeight)
	return initToast
}

func (b *Board) refreshGitPanel() {
	if !b.gitPanel.Active() {
		return
	}
	branch := kbrdfs.GitCurrentBranch(b.gitRepoRoot)
	files := kbrdfs.GitChangedFiles(b.gitRepoRoot)
	b.gitPanel.Refresh(branch, files, b.termWidth, b.termHeight)
}

func (b *Board) handleGitDiff() (tea.Model, tea.Cmd) {
	c := exec.Command("git", "-C", b.gitRepoRoot, "diff")
	return b, tea.ExecProcess(c, func(err error) tea.Msg {
		return gitRefreshMsg{}
	})
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
	if msg.ThenSync {
		return b, tea.Batch(
			b.notifier.Send("committed; syncing…", notifySuccess),
			func() tea.Msg { return gitSyncRequestMsg{} },
		)
	}
	return b, b.notifier.Send("commit ok", notifySuccess)
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
