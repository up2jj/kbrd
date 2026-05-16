package model

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	kbrdfs "kbrd/fs"
)

type gitPanelMode int

const (
	gitPanelList gitPanelMode = iota
	gitPanelCommitInput
	gitPanelRemoteInput
	gitPanelOutput
)

type gitPanelCloseMsg struct{}
type gitDiffRequestMsg struct{}
type gitSyncRequestMsg struct{}
type gitContinueSyncMsg struct{}
type gitLogRequestMsg struct{}
type gitCommitRequestMsg struct {
	Message  string
	ThenSync bool
}
type gitAddRemoteRequestMsg struct {
	URL string
}
type gitRefreshMsg struct{}

type GitPanel struct {
	active        bool
	mode          gitPanelMode
	repoRoot      string
	branch        string
	hasRemote     bool
	files         []kbrdfs.FileChange
	table         table.Model
	commitIn      textinput.Model
	remoteIn      textinput.Model
	thenSync      bool
	output        viewport.Model
	outputTitle   string
	outputPending tea.Msg
}

func (p *GitPanel) Active() bool { return p.active }

func (p *GitPanel) Open(repoRoot, branch string, hasRemote bool, files []kbrdfs.FileChange, termW, termH int) {
	p.active = true
	p.mode = gitPanelList
	p.repoRoot = repoRoot
	p.branch = branch
	p.hasRemote = hasRemote
	p.files = files
	p.thenSync = false
	p.rebuildTable(termW, termH)

	p.commitIn = newPanelInput("  msg: ", 200)
	p.remoteIn = newPanelInput("  url: ", 300)
	p.remoteIn.Placeholder = "git@github.com:user/repo.git"
}

func newPanelInput(prompt string, charLimit int) textinput.Model {
	ti := textinput.New()
	ti.Prompt = prompt
	ti.CharLimit = charLimit
	ti.Width = 60
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#60a5fa")).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0"))
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#fde047"))
	return ti
}

func (p *GitPanel) Refresh(branch string, hasRemote bool, files []kbrdfs.FileChange, termW, termH int) {
	p.branch = branch
	p.hasRemote = hasRemote
	p.files = files
	p.rebuildTable(termW, termH)
}

func (p *GitPanel) rebuildTable(termW, termH int) {
	pathW := termW - 24
	if pathW < 20 {
		pathW = 20
	}
	if pathW > 80 {
		pathW = 80
	}
	cols := []table.Column{
		{Title: "St", Width: 3},
		{Title: "File", Width: pathW},
		{Title: "+/-", Width: 14},
	}
	rows := make([]table.Row, 0, len(p.files))
	for _, f := range p.files {
		stats := ""
		if f.Added > 0 || f.Deleted > 0 {
			stats = fmt.Sprintf("+%d -%d", f.Added, f.Deleted)
		}
		display := f.Path
		if f.OrigPath != "" {
			display = f.OrigPath + " → " + f.Path
		}
		rows = append(rows, table.Row{f.Status, display, stats})
	}
	// table.WithHeight subtracts the 2-line header from the viewport, so the
	// total height we pass must include those 2 lines to show every row.
	const headerLines = 2
	bodyH := len(rows)
	if bodyH < 1 {
		bodyH = 1
	}
	if max := termH - 14; max > 0 && bodyH > max {
		bodyH = max
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(bodyH+headerLines),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#334155")).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color("#94a3b8"))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#3b82f6")).
		Bold(true)
	t.SetStyles(s)
	p.table = t
}

func (p *GitPanel) Close() {
	p.active = false
	p.files = nil
	p.mode = gitPanelList
	p.thenSync = false
	p.hasRemote = false
	p.outputPending = nil
}

func (p *GitPanel) ShowOutput(title, content string, pending tea.Msg, termW, termH int) {
	vw := termW - 14
	if vw < 40 {
		vw = 40
	}
	if vw > 120 {
		vw = 120
	}
	vh := termH - 12
	if vh < 5 {
		vh = 5
	}
	if vh > 20 {
		vh = 20
	}
	vp := viewport.New(vw, vh)
	vp.SetContent(content)
	p.output = vp
	p.outputTitle = title
	p.outputPending = pending
	p.mode = gitPanelOutput
}

func (p *GitPanel) Update(msg tea.Msg) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	if p.mode == gitPanelOutput {
		if key.Matches(km, Keys.GitPanelClose) {
			pending := p.outputPending
			p.outputPending = nil
			p.mode = gitPanelList
			if pending != nil {
				return func() tea.Msg { return pending }
			}
			return nil
		}
		var cmd tea.Cmd
		p.output, cmd = p.output.Update(km)
		return cmd
	}

	if p.mode == gitPanelCommitInput {
		switch {
		case key.Matches(km, Keys.GitCommitCancel):
			p.mode = gitPanelList
			p.commitIn.Blur()
			p.thenSync = false
			return nil
		case km.String() == "enter":
			msg := p.commitIn.Value()
			thenSync := p.thenSync
			p.commitIn.Blur()
			p.mode = gitPanelList
			p.thenSync = false
			return func() tea.Msg {
				return gitCommitRequestMsg{Message: msg, ThenSync: thenSync}
			}
		}
		var cmd tea.Cmd
		p.commitIn, cmd = p.commitIn.Update(km)
		return cmd
	}

	if p.mode == gitPanelRemoteInput {
		switch {
		case key.Matches(km, Keys.GitCommitCancel):
			p.mode = gitPanelList
			p.remoteIn.Blur()
			return nil
		case km.String() == "enter":
			url := p.remoteIn.Value()
			p.remoteIn.Blur()
			p.mode = gitPanelList
			return func() tea.Msg {
				return gitAddRemoteRequestMsg{URL: url}
			}
		}
		var cmd tea.Cmd
		p.remoteIn, cmd = p.remoteIn.Update(km)
		return cmd
	}

	if key.Matches(km, Keys.GitPanelClose) {
		return func() tea.Msg { return gitPanelCloseMsg{} }
	}
	if key.Matches(km, Keys.GitLog) {
		return func() tea.Msg { return gitLogRequestMsg{} }
	}
	if !p.hasRemote && key.Matches(km, Keys.GitAddRemote) {
		p.startRemoteInput()
		return nil
	}
	// When the working tree is clean, only close, log, and add-remote are active.
	if len(p.files) == 0 {
		return nil
	}
	switch {
	case key.Matches(km, Keys.GitDiff):
		return func() tea.Msg { return gitDiffRequestMsg{} }
	case key.Matches(km, Keys.GitCommit):
		p.startCommitInput(false)
		return nil
	case key.Matches(km, Keys.GitCommitSync):
		if !p.hasRemote {
			return nil
		}
		p.startCommitInput(true)
		return nil
	case key.Matches(km, Keys.GitSync):
		if !p.hasRemote {
			return nil
		}
		return func() tea.Msg { return gitSyncRequestMsg{} }
	}
	var cmd tea.Cmd
	p.table, cmd = p.table.Update(km)
	return cmd
}

func (p *GitPanel) startCommitInput(thenSync bool) {
	p.mode = gitPanelCommitInput
	p.thenSync = thenSync
	p.commitIn.SetValue(time.Now().Format("2006-01-02 15:04:05"))
	p.commitIn.CursorEnd()
	p.commitIn.Focus()
}

func joinSep(parts []string, sep string) string {
	out := ""
	for i, s := range parts {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}

func (p *GitPanel) startRemoteInput() {
	p.mode = gitPanelRemoteInput
	p.remoteIn.SetValue("")
	p.remoteIn.Focus()
}

func (p *GitPanel) View() string {
	branchLabel := p.branch
	if branchLabel == "" {
		branchLabel = "(no branch)"
	}
	titleText := "git" + " · " + branchLabel
	if p.mode == gitPanelOutput {
		titleText = "git · " + p.outputTitle
	}
	title := helpTitleStyle.Render(titleText)

	sep := helpSepStyle.Render(" · ")

	var body, footer string
	switch p.mode {
	case gitPanelOutput:
		body = p.output.View()
		pendingHint := ""
		if p.outputPending != nil {
			pendingHint = sep + helpDimStyle.Render("continues on close")
		}
		footer = helpKeyStyle.Render("j/k") + " " + helpLabelStyle.Render("scroll") + sep +
			helpKeyStyle.Render("q/esc") + " " + helpLabelStyle.Render("back") + pendingHint
	case gitPanelCommitInput:
		if len(p.files) == 0 {
			body = helpDimStyle.Render("working tree clean")
		} else {
			body = p.table.View()
		}
		hint := helpDimStyle.Render("  enter to commit · esc cancel")
		if p.thenSync {
			hint = helpDimStyle.Render("  enter to commit + sync · esc cancel")
		}
		footer = p.commitIn.View() + hint
	case gitPanelRemoteInput:
		if len(p.files) == 0 {
			body = helpDimStyle.Render("working tree clean")
		} else {
			body = p.table.View()
		}
		hint := helpDimStyle.Render("  enter to add as origin · esc cancel")
		footer = p.remoteIn.View() + hint
	default: // gitPanelList
		parts := []string{}
		add := func(k, label string) {
			parts = append(parts, helpKeyStyle.Render(k)+" "+helpLabelStyle.Render(label))
		}
		if len(p.files) == 0 {
			body = helpDimStyle.Render("working tree clean")
		} else {
			body = p.table.View()
			add("d", "diff")
			add("c", "commit")
			if p.hasRemote {
				add("s", "sync")
				add("S", "commit+sync")
			}
		}
		if !p.hasRemote {
			add("a", "add remote")
		}
		add("l", "log")
		add("q/esc", "close")
		footer = joinSep(parts, sep)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", footer)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Padding(1, 3).
		Render(content)
}
