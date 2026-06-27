package git

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	kbrdfs "kbrd/fs"
	"kbrd/theme"
)

// panelKeys are the git panel's in-panel bindings. The panel-open binding ("g")
// stays in the host's global keymap; these are internal to the panel.
var panelKeys = struct {
	Commit, CommitSync, CommitCancel, Sync, Log, Diff, AddRemote, Close, FocusToggle, FocusLeft, FocusRight key.Binding
}{
	Diff:         key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff")),
	FocusLeft:    key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "files")),
	FocusRight:   key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "diff")),
	Commit:       key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "commit")),
	Sync:         key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sync (pull+push)")),
	CommitSync:   key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "commit + sync")),
	Log:          key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "log")),
	AddRemote:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add remote")),
	Close:        key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("q/esc", "close")),
	CommitCancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	FocusToggle:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus pane")),
}

// Git-local copies of the host's help-row styles, rebuilt from the palette on
// SetPalette. Kept here so the git package does not depend on the host's styles.
var (
	gitTitleStyle lipgloss.Style
	gitSepStyle   lipgloss.Style
	gitKeyStyle   lipgloss.Style
	gitLabelStyle lipgloss.Style
	gitDimStyle   lipgloss.Style
)

func setGitStyles(p theme.Palette) {
	gitKeyStyle = lipgloss.NewStyle().Bold(true).Foreground(p.FgBase)
	gitLabelStyle = lipgloss.NewStyle().Foreground(p.FgMuted)
	gitSepStyle = lipgloss.NewStyle().Foreground(p.FgDim)
	gitTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(p.Primary)
	gitDimStyle = lipgloss.NewStyle().Foreground(p.FgDim).Italic(true)
}

// applyInputPalette restyles a bubbles textinput using the palette colors.
func applyInputPalette(ti *textinput.Model, p theme.Palette) {
	styles := ti.Styles()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(p.Primary).Bold(true)
	styles.Blurred.Prompt = styles.Focused.Prompt
	styles.Focused.Text = lipgloss.NewStyle().Foreground(p.FgBase)
	styles.Blurred.Text = styles.Focused.Text
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(p.FgDim).Italic(true)
	styles.Blurred.Placeholder = styles.Focused.Placeholder
	styles.Cursor.Color = p.Highlight
	ti.SetStyles(styles)
}

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

type gitPanelCloseMsg struct{ gitMsg }
type gitSyncRequestMsg struct{ gitMsg }
type gitContinueSyncMsg struct{ gitMsg }
type gitLogRequestMsg struct{ gitMsg }
type gitCommitRequestMsg struct {
	gitMsg
	Message  string
	ThenSync bool
}
type gitAddRemoteRequestMsg struct {
	gitMsg
	URL string
}
type gitRefreshMsg struct{ gitMsg }
type gitDiffForFileMsg struct {
	gitMsg
	Path     string
	Status   string
	OrigPath string
}

type GitPanel struct {
	active       bool
	focus        gitPanelFocus
	input        gitPanelInput
	rightView    gitPanelRightView
	repoRoot     string
	branch       string
	hasRemote    bool
	files        []kbrdfs.FileChange
	table        table.Model
	commitIn     textinput.Model
	remoteIn     textinput.Model
	thenSync     bool
	right        viewport.Model
	rightTitle   string
	rightContent string
	diffCache    map[string]string
	lastCursor   int
	termW        int
	termH        int
	palette      theme.Palette
}

// SetPalette updates the panel's palette and restyles any pre-built inputs
// so the new colors apply on the next render.
func (p *GitPanel) SetPalette(pal theme.Palette) {
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

func newPanelInput(prompt string, charLimit int, pal theme.Palette) textinput.Model {
	ti := textinput.New()
	ti.Prompt = prompt
	ti.CharLimit = charLimit
	ti.SetWidth(60)
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

// dims returns inner content sizes: total inner W/H, left pane W, right pane W.
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
	leftW = max(min(innerW*3/5, 60), 36)
	rightW = max(
		// 3 chars for separator + padding
		innerW-leftW-3, 20)
	return
}

func panelPaneContentWidth(outerW int) int {
	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Padding(0, 1)
	return theme.StyleContentWidth(style, outerW)
}

func (p *GitPanel) rebuild() {
	_, innerH, leftW, rightW := p.dims()
	bodyH := max(
		// leave room for title + footer
		innerH-4, 5)

	// Pane has 2-char border + 2-char inner padding, and the table itself adds
	// 1-char cell padding on both sides of every column (so 6 chars overhead
	// for three columns). St=3, +/-=7 → file column = leftW - 4 - 6 - 3 - 7.
	pathW := max(leftW-20, 10)
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
	// Both panes must render bodyH content lines so their boxes line up. The
	// right pane is a 1-line title over a (bodyH-1)-line viewport = bodyH lines.
	// For the table: WithHeight is applied before SetStyles adds the header's
	// bottom border, so the rendered header ends up 2 lines while WithHeight only
	// accounted for 1 — hence bodyH-1 here yields a bodyH-line table.View().
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(p.focus == focusFiles),
		table.WithWidth(panelPaneContentWidth(leftW)),
		table.WithHeight(bodyH-1),
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

	// Reserve one column for the scrollbar inside the pane's content area.
	vpW := max(panelPaneContentWidth(rightW)-1, 10)
	vp := viewport.New(viewport.WithWidth(vpW), viewport.WithHeight(bodyH-1)) // -1 to make room for the title row
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
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	// Active input takes most keys.
	if p.input == inputCommit {
		switch {
		case key.Matches(km, panelKeys.CommitCancel):
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
		case key.Matches(km, panelKeys.CommitCancel):
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
	if key.Matches(km, panelKeys.Close) {
		return func() tea.Msg { return gitPanelCloseMsg{} }
	}
	if key.Matches(km, panelKeys.FocusToggle) {
		p.toggleFocus()
		return nil
	}
	if key.Matches(km, panelKeys.FocusLeft) && p.focus == focusDiff {
		p.toggleFocus()
		return nil
	}
	if key.Matches(km, panelKeys.FocusRight) && p.focus == focusFiles {
		p.toggleFocus()
		return nil
	}
	if key.Matches(km, panelKeys.Log) {
		return func() tea.Msg { return gitLogRequestMsg{} }
	}
	if key.Matches(km, panelKeys.Diff) {
		// `d` shows current file diff (useful when right pane is on log)
		p.rightView = rightDiff
		return p.DiffRequestForCurrent()
	}
	if !p.hasRemote && key.Matches(km, panelKeys.AddRemote) {
		p.startRemoteInput()
		return nil
	}
	// Sync (pull+push) gates only on having a remote, so it can pull onto a
	// clean tree — not just when there are local changes to commit+push.
	if p.hasRemote && key.Matches(km, panelKeys.Sync) {
		return func() tea.Msg { return gitSyncRequestMsg{} }
	}
	if len(p.files) > 0 {
		switch {
		case key.Matches(km, panelKeys.Commit):
			p.startCommitInput(false)
			return nil
		case key.Matches(km, panelKeys.CommitSync):
			if !p.hasRemote {
				return nil
			}
			p.startCommitInput(true)
			return nil
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

func (p *GitPanel) HandleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		p.right.ScrollUp(3)
	case tea.MouseWheelDown:
		p.right.ScrollDown(3)
	}
	return nil
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
	var out strings.Builder
	for i, s := range parts {
		if i > 0 {
			out.WriteString(sep)
		}
		out.WriteString(s)
	}
	return out.String()
}

func (p *GitPanel) startRemoteInput() {
	p.input = inputRemote
	p.remoteIn.SetValue("")
	p.remoteIn.Focus()
}

// renderScrollbar returns a height-line vertical bar: a dim track with a bright
// thumb whose size and position reflect the diff viewport's scroll state. When
// the content fits entirely, it returns a plain dim track so the pane width
// stays stable. The thumb is drawn as a background fill rather than a block
// glyph so it forms one solid bar — block glyphs (█) leave font-dependent gaps
// between cells.
func (p *GitPanel) renderScrollbar(height int) string {
	if height < 1 {
		return ""
	}
	track := lipgloss.NewStyle().Foreground(p.palette.FgDim).Render("│")

	total := p.right.TotalLineCount()
	if total <= height || total == 0 {
		lines := make([]string, height)
		for i := range lines {
			lines[i] = track
		}
		return strings.Join(lines, "\n")
	}

	thumbN := min(max(height*height/total, 1), height)
	// Round to nearest cell: (x*2+1)/2 with integer math.
	pos := min(max(int((float64(height-thumbN)*p.right.ScrollPercent())*2+1)/2, 0), height-thumbN)

	thumb := lipgloss.NewStyle().Background(p.palette.Primary).Render(" ")
	lines := make([]string, height)
	for i := range lines {
		if i >= pos && i < pos+thumbN {
			lines[i] = thumb
		} else {
			lines[i] = track
		}
	}
	return strings.Join(lines, "\n")
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
	sep := gitSepStyle.Render(" · ")

	_, _, leftW, rightW := p.dims()

	// Left pane: file table (or "clean" message).
	var leftBody string
	if len(p.files) == 0 {
		leftBody = gitDimStyle.Render("working tree clean")
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
	if p.right.TotalLineCount() > p.right.Height() {
		scroll = fmt.Sprintf("%d%%", int(p.right.ScrollPercent()*100))
	} else if p.right.TotalLineCount() > 0 {
		scroll = "all"
	}
	titleW := max(panelPaneContentWidth(rightW), 10)
	leftTitle := p.rightTitle
	if w := titleW - len(scroll) - 1; w > 0 && len(leftTitle) > w {
		leftTitle = leftTitle[:w-1] + "…"
	}
	pad := max(titleW-len(leftTitle)-len(scroll), 1)
	rightTitle := gitDimStyle.Render(leftTitle + strings.Repeat(" ", pad) + scroll)
	bar := p.renderScrollbar(p.right.Height())
	diffWithBar := lipgloss.JoinHorizontal(lipgloss.Top, p.right.View(), bar)
	rightBody := lipgloss.JoinVertical(lipgloss.Left, rightTitle, diffWithBar)
	rightStyle := paneIdleStyle
	if p.focus == focusDiff {
		rightStyle = paneActiveStyle
	}
	rightPane := rightStyle.Width(rightW).Render(rightBody)

	row := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, " ", rightPane)

	parts := []string{}
	add := func(k, label string) {
		parts = append(parts, gitKeyStyle.Render(k)+" "+gitLabelStyle.Render(label))
	}
	if p.focus == focusDiff {
		add("j/k", "scroll")
		add("ctrl+u/d", "half page")
		add("g/G", "top/bottom")
		add("←/tab", "files")
		add("q/esc", "close")
	} else {
		if len(p.files) > 0 {
			add("c", "commit all")
		}
		if p.hasRemote {
			add("s", "sync")
			if len(p.files) > 0 {
				add("S", "commit all+sync")
			}
		}
		if !p.hasRemote {
			add("a", "add remote")
		}
		add("l", "log")
		add("d", "diff")
		add("→/tab", "scroll diff")
		add("q/esc", "close")
	}
	footer := joinSep(parts, sep)

	content := lipgloss.JoinVertical(lipgloss.Left, row, "", footer)
	panel := theme.RoundedFrame("git · "+branchLabel, gitTitleStyle, content, p.palette.BorderActive, 1, 3, 0)

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
	sep := gitSepStyle.Render(" · ")
	keyLabel := func(k, label string) string {
		return gitKeyStyle.Render(k) + " " + gitLabelStyle.Render(label)
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
	content := lipgloss.JoinVertical(lipgloss.Left, body, "", hint)
	return theme.RoundedFrame(title, gitTitleStyle, content, p.palette.BorderActive, 1, 3, 0)
}
