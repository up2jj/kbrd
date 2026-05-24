package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	kbrdfs "kbrd/fs"
)

type gitPanelFocus int

const (
	focusFiles gitPanelFocus = iota
	focusDiff
)

type gitPanelInput int

const (
	inputNone gitPanelInput = iota
	inputCommit
	inputRemote
)

type gitPanelRightView int

const (
	rightDiff gitPanelRightView = iota
	rightLog
)

type gitPanelCloseMsg struct{}
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
type gitDiffForFileMsg struct {
	Path     string
	Status   string
	OrigPath string
}

type GitPanel struct {
	active     bool
	focus      gitPanelFocus
	input      gitPanelInput
	rightView  gitPanelRightView
	repoRoot   string
	branch     string
	hasRemote  bool
	files      []kbrdfs.FileChange
	table      table.Model
	commitIn   textinput.Model
	remoteIn   textinput.Model
	thenSync   bool
	right        viewport.Model
	rightTitle   string
	rightContent string
	diffCache    map[string]string
	lastCursor int
	termW      int
	termH      int
	palette    Palette
}

// SetPalette updates the panel's palette and restyles any pre-built inputs
// so the new colors apply on the next render.
func (p *GitPanel) SetPalette(pal Palette) {
	p.palette = pal
	applyInputPalette(&p.commitIn, pal)
	applyInputPalette(&p.remoteIn, pal)
}

func (p *GitPanel) Active() bool { return p.active }

func (p *GitPanel) Open(repoRoot, branch string, hasRemote bool, files []kbrdfs.FileChange, termW, termH int) {
	p.active = true
	p.focus = focusFiles
	p.input = inputNone
	p.rightView = rightDiff
	p.repoRoot = repoRoot
	p.branch = branch
	p.hasRemote = hasRemote
	p.files = files
	p.thenSync = false
	p.diffCache = map[string]string{}
	p.termW = termW
	p.termH = termH
	p.rebuild()

	p.commitIn = newPanelInput("  msg: ", 200, p.palette)
	p.remoteIn = newPanelInput("  url: ", 300, p.palette)
	p.remoteIn.Placeholder = "git@github.com:user/repo.git"
}

func newPanelInput(prompt string, charLimit int, pal Palette) textinput.Model {
	ti := textinput.New()
	ti.Prompt = prompt
	ti.CharLimit = charLimit
	ti.Width = 60
	applyInputPalette(&ti, pal)
	return ti
}

func (p *GitPanel) Refresh(branch string, hasRemote bool, files []kbrdfs.FileChange, termW, termH int) {
	p.branch = branch
	p.hasRemote = hasRemote
	p.files = files
	p.termW = termW
	p.termH = termH
	p.diffCache = map[string]string{}
	p.rebuild()
}

// panelDimensions returns inner content sizes: total inner W/H, left pane W, right pane W.
func (p *GitPanel) dims() (innerW, innerH, leftW, rightW int) {
	innerW = p.termW - 20
	if max := 140; innerW > max {
		innerW = max
	}
	if innerW < 60 {
		innerW = 60
	}
	innerH = p.termH - 10
	if max := 28; innerH > max {
		innerH = max
	}
	if innerH < 10 {
		innerH = 10
	}
	leftW = innerW * 3 / 5
	if leftW > 60 {
		leftW = 60
	}
	if leftW < 36 {
		leftW = 36
	}
	rightW = innerW - leftW - 3 // 3 chars for separator + padding
	if rightW < 20 {
		rightW = 20
	}
	return
}

func (p *GitPanel) rebuild() {
	_, innerH, leftW, rightW := p.dims()
	bodyH := innerH - 4 // leave room for title + footer
	if bodyH < 5 {
		bodyH = 5
	}

	// Pane has 2-char border + 2-char inner padding, and the table itself adds
	// 1-char cell padding on both sides of every column (so 6 chars overhead
	// for three columns). St=3, +/-=7 → file column = leftW - 4 - 6 - 3 - 7.
	pathW := leftW - 20
	if pathW < 10 {
		pathW = 10
	}
	cols := []table.Column{
		{Title: "St", Width: 3},
		{Title: "File", Width: pathW},
		{Title: "+/-", Width: 7},
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
	const headerLines = 2
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(p.focus == focusFiles),
		table.WithHeight(bodyH-headerLines),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(p.palette.BorderMuted).
		BorderBottom(true).
		Bold(true).
		Foreground(p.palette.FgMuted)
	s.Selected = s.Selected.
		Foreground(p.palette.FgOnAccent).
		Background(p.palette.PrimaryStrong).
		Bold(true)
	t.SetStyles(s)
	p.table = t
	p.lastCursor = t.Cursor()

	vp := viewport.New(rightW, bodyH-1) // -1 to make room for the title row
	vp.SetContent(p.rightContent)
	if p.rightTitle == "" {
		p.rightTitle = "diff"
	}
	p.right = vp
}

func (p *GitPanel) Close() {
	p.active = false
	p.files = nil
	p.input = inputNone
	p.focus = focusFiles
	p.rightView = rightDiff
	p.thenSync = false
	p.hasRemote = false
	p.diffCache = nil
	p.rightTitle = ""
}

func (p *GitPanel) SetDiffForFile(path, content string) {
	if p.diffCache == nil {
		p.diffCache = map[string]string{}
	}
	p.diffCache[path] = content
	if p.rightView != rightDiff {
		return
	}
	if cur, ok := p.CurrentFile(); !ok || cur.Path != path {
		return
	}
	p.rightTitle = "diff: " + path
	p.rightContent = content
	p.right.SetContent(content)
	p.right.GotoTop()
}

func (p *GitPanel) SetLog(content string) {
	p.rightView = rightLog
	p.rightTitle = "log"
	p.rightContent = content
	p.right.SetContent(content)
	p.right.GotoTop()
}

func (p *GitPanel) CurrentFile() (kbrdfs.FileChange, bool) {
	if len(p.files) == 0 {
		return kbrdfs.FileChange{}, false
	}
	idx := p.table.Cursor()
	if idx < 0 || idx >= len(p.files) {
		return kbrdfs.FileChange{}, false
	}
	return p.files[idx], true
}

func (p *GitPanel) currentPath() string {
	if f, ok := p.CurrentFile(); ok {
		return f.Path
	}
	return ""
}

func (p *GitPanel) RightView() gitPanelRightView { return p.rightView }

func (p *GitPanel) DiffRequestForCurrent() tea.Cmd {
	f, ok := p.CurrentFile()
	if !ok {
		return nil
	}
	if cached, hit := p.diffCache[f.Path]; hit {
		p.rightTitle = "diff: " + f.Path
		p.rightContent = cached
		p.right.SetContent(cached)
		p.right.GotoTop()
		return nil
	}
	return func() tea.Msg {
		return gitDiffForFileMsg{Path: f.Path, Status: f.Status, OrigPath: f.OrigPath}
	}
}

func (p *GitPanel) Update(msg tea.Msg) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	// Active input takes most keys.
	if p.input == inputCommit {
		switch {
		case key.Matches(km, Keys.GitCommitCancel):
			p.input = inputNone
			p.commitIn.Blur()
			p.thenSync = false
			return nil
		case km.String() == "enter":
			msg := p.commitIn.Value()
			thenSync := p.thenSync
			p.commitIn.Blur()
			p.input = inputNone
			p.thenSync = false
			return func() tea.Msg {
				return gitCommitRequestMsg{Message: msg, ThenSync: thenSync}
			}
		}
		var cmd tea.Cmd
		p.commitIn, cmd = p.commitIn.Update(km)
		return cmd
	}

	if p.input == inputRemote {
		switch {
		case key.Matches(km, Keys.GitCommitCancel):
			p.input = inputNone
			p.remoteIn.Blur()
			return nil
		case km.String() == "enter":
			url := p.remoteIn.Value()
			p.remoteIn.Blur()
			p.input = inputNone
			return func() tea.Msg {
				return gitAddRemoteRequestMsg{URL: url}
			}
		}
		var cmd tea.Cmd
		p.remoteIn, cmd = p.remoteIn.Update(km)
		return cmd
	}

	// No input active.
	if key.Matches(km, Keys.GitPanelClose) {
		return func() tea.Msg { return gitPanelCloseMsg{} }
	}
	if key.Matches(km, Keys.GitPanelFocusToggle) {
		p.toggleFocus()
		return nil
	}
	if key.Matches(km, Keys.GitLog) {
		return func() tea.Msg { return gitLogRequestMsg{} }
	}
	if key.Matches(km, Keys.GitDiff) {
		// `d` shows current file diff (useful when right pane is on log)
		p.rightView = rightDiff
		return p.DiffRequestForCurrent()
	}
	if !p.hasRemote && key.Matches(km, Keys.GitAddRemote) {
		p.startRemoteInput()
		return nil
	}
	if len(p.files) > 0 {
		switch {
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
	}

	if p.focus == focusDiff {
		var cmd tea.Cmd
		p.right, cmd = p.right.Update(km)
		return cmd
	}

	// focusFiles: delegate to table, then dispatch diff if selection changed.
	var cmd tea.Cmd
	p.table, cmd = p.table.Update(km)
	if c := p.table.Cursor(); c != p.lastCursor {
		p.lastCursor = c
		if p.rightView == rightDiff {
			if dc := p.DiffRequestForCurrent(); dc != nil {
				if cmd == nil {
					return dc
				}
				return tea.Batch(cmd, dc)
			}
		}
	}
	return cmd
}

func (p *GitPanel) toggleFocus() {
	if p.focus == focusFiles {
		p.focus = focusDiff
		p.table.Blur()
	} else {
		p.focus = focusFiles
		p.table.Focus()
	}
}

func (p *GitPanel) startCommitInput(thenSync bool) {
	p.input = inputCommit
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
	p.input = inputRemote
	p.remoteIn.SetValue("")
	p.remoteIn.Focus()
}

func (p *GitPanel) View() string {
	paneActiveStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(p.palette.BorderActive).
		Padding(0, 1)
	paneIdleStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(p.palette.BorderMuted).
		Padding(0, 1)
	branchLabel := p.branch
	if branchLabel == "" {
		branchLabel = "(no branch)"
	}
	title := helpTitleStyle.Render("git · " + branchLabel)
	sep := helpSepStyle.Render(" · ")

	_, _, leftW, rightW := p.dims()

	// Left pane: file table (or "clean" message).
	var leftBody string
	if len(p.files) == 0 {
		leftBody = helpDimStyle.Render("working tree clean")
	} else {
		leftBody = p.table.View()
	}
	leftStyle := paneIdleStyle
	if p.focus == focusFiles {
		leftStyle = paneActiveStyle
	}
	leftPane := leftStyle.Width(leftW).Render(leftBody)

	// Right pane: viewport with a title row that includes a scroll indicator.
	scroll := ""
	if p.right.TotalLineCount() > p.right.Height {
		scroll = fmt.Sprintf("%d%%", int(p.right.ScrollPercent()*100))
	} else if p.right.TotalLineCount() > 0 {
		scroll = "all"
	}
	titleW := rightW - 4 // border 2 + padding 2
	if titleW < 10 {
		titleW = 10
	}
	leftTitle := p.rightTitle
	if w := titleW - len(scroll) - 1; w > 0 && len(leftTitle) > w {
		leftTitle = leftTitle[:w-1] + "…"
	}
	pad := titleW - len(leftTitle) - len(scroll)
	if pad < 1 {
		pad = 1
	}
	rightTitle := helpDimStyle.Render(leftTitle + strings.Repeat(" ", pad) + scroll)
	rightBody := lipgloss.JoinVertical(lipgloss.Left, rightTitle, p.right.View())
	rightStyle := paneIdleStyle
	if p.focus == focusDiff {
		rightStyle = paneActiveStyle
	}
	rightPane := rightStyle.Width(rightW).Render(rightBody)

	row := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, " ", rightPane)

	parts := []string{}
	add := func(k, label string) {
		parts = append(parts, helpKeyStyle.Render(k)+" "+helpLabelStyle.Render(label))
	}
	if p.focus == focusDiff {
		add("j/k", "scroll")
		add("ctrl+u/d", "half page")
		add("g/G", "top/bottom")
		add("tab", "files")
		add("q/esc", "close")
	} else {
		if len(p.files) > 0 {
			add("c", "commit all")
			if p.hasRemote {
				add("s", "sync")
				add("S", "commit all+sync")
			}
		}
		if !p.hasRemote {
			add("a", "add remote")
		}
		add("l", "log")
		add("d", "diff")
		add("tab", "scroll diff")
		add("q/esc", "close")
	}
	footer := joinSep(parts, sep)

	content := lipgloss.JoinVertical(lipgloss.Left, title, "", row, "", footer)
	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.palette.BorderActive).
		Padding(1, 2).
		Render(content)

	if p.input != inputNone {
		return lipgloss.Place(
			lipgloss.Width(panel), lipgloss.Height(panel),
			lipgloss.Center, lipgloss.Center,
			p.inputDialog(),
			lipgloss.WithWhitespaceChars(" "),
		)
	}
	return panel
}

func (p *GitPanel) inputDialog() string {
	sep := helpSepStyle.Render(" · ")
	keyLabel := func(k, label string) string {
		return helpKeyStyle.Render(k) + " " + helpLabelStyle.Render(label)
	}
	var title, body, hint string
	switch p.input {
	case inputCommit:
		title = "Commit all"
		confirmLabel := "commit all"
		if p.thenSync {
			title = "Commit all + sync"
			confirmLabel = "commit all + sync"
		}
		body = p.commitIn.View()
		hint = keyLabel("enter", confirmLabel) + sep + keyLabel("esc", "cancel")
	case inputRemote:
		title = "Add remote"
		body = p.remoteIn.View()
		hint = keyLabel("enter", "add as origin") + sep + keyLabel("esc", "cancel")
	}
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(p.palette.FgEmphasis)
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(title),
		"",
		body,
		"",
		hint,
	)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.palette.BorderActive).
		Padding(1, 3).
		Render(content)
}
