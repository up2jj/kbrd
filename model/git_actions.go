package model

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
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
	tool := resolveDiffTool(b.cfg.GitDiffTool)
	args := []string{"--no-optional-locks", "-C", b.gitRepoRoot}
	if tool != "difft" {
		args = append(args, "-c", "color.ui=always")
	}
	args = append(args, "diff")
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return b, b.notifier.Send("git diff failed: "+detail, notifyError)
	}
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
		text = "(no unstaged changes)"
	}
	b.gitPanel.ShowOutput("diff ("+tool+")", text, nil, b.termWidth, b.termHeight)
	return b, nil
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
	output := strings.TrimSpace(msg.Output)
	if output == "" {
		output = "commit succeeded (no output)"
	}
	var pending tea.Msg
	if msg.ThenSync {
		pending = gitContinueSyncMsg{}
	}
	b.gitPanel.ShowOutput("commit", output, pending, b.termWidth, b.termHeight)
	return b, nil
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
	b.gitPanel.ShowOutput("log", text, nil, b.termWidth, b.termHeight)
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
