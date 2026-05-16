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
type gitRefreshMsg struct{}

type GitPanel struct {
	active        bool
	mode          gitPanelMode
	repoRoot      string
	branch        string
	files         []kbrdfs.FileChange
	table         table.Model
	commitIn      textinput.Model
	thenSync      bool
	output        viewport.Model
	outputTitle   string
	outputPending tea.Msg
}

func (p *GitPanel) Active() bool { return p.active }

func (p *GitPanel) Open(repoRoot, branch string, files []kbrdfs.FileChange, termW, termH int) {
	p.active = true
	p.mode = gitPanelList
	p.repoRoot = repoRoot
	p.branch = branch
	p.files = files
	p.thenSync = false
	p.rebuildTable(termW, termH)

	ti := textinput.New()
	ti.Prompt = "  msg: "
	ti.CharLimit = 200
	ti.Width = 60
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#60a5fa")).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0"))
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#fde047"))
	p.commitIn = ti
}

func (p *GitPanel) Refresh(branch string, files []kbrdfs.FileChange, termW, termH int) {
	p.branch = branch
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

	if key.Matches(km, Keys.GitPanelClose) {
		return func() tea.Msg { return gitPanelCloseMsg{} }
	}
	if key.Matches(km, Keys.GitLog) {
		return func() tea.Msg { return gitLogRequestMsg{} }
	}
	// When the working tree is clean, only close and log are active.
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
		p.startCommitInput(true)
		return nil
	case key.Matches(km, Keys.GitSync):
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
	default: // gitPanelList
		if len(p.files) == 0 {
			body = helpDimStyle.Render("working tree clean")
			footer = helpKeyStyle.Render("l") + " " + helpLabelStyle.Render("log") + sep +
				helpKeyStyle.Render("q/esc") + " " + helpLabelStyle.Render("close")
		} else {
			body = p.table.View()
			footer = helpKeyStyle.Render("d") + " " + helpLabelStyle.Render("diff") + sep +
				helpKeyStyle.Render("c") + " " + helpLabelStyle.Render("commit") + sep +
				helpKeyStyle.Render("s") + " " + helpLabelStyle.Render("sync") + sep +
				helpKeyStyle.Render("S") + " " + helpLabelStyle.Render("commit+sync") + sep +
				helpKeyStyle.Render("l") + " " + helpLabelStyle.Render("log") + sep +
				helpKeyStyle.Render("q/esc") + " " + helpLabelStyle.Render("close")
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", footer)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Padding(1, 3).
		Render(content)
}
