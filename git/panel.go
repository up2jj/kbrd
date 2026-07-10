package git

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	kbrdfs "kbrd/fs"
	"kbrd/theme"
)

// panelKeys are deliberately intent-led. Git's commit/sync distinction is
// still available to people who need it, but the default dirty-tree action is
// one "save & sync" operation rather than two easily-confused actions.
var panelKeys = struct {
	Save, Pull, History, Changes, Connect, Cancel, Close, FocusToggle, Up, Down key.Binding
}{
	Save:        key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "save & sync")),
	Pull:        key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "pull")),
	History:     key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "history")),
	Changes:     key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "changes")),
	Connect:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "connect remote")),
	Close:       key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("q/esc", "close")),
	Cancel:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	FocusToggle: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch section")),
	Up:          key.NewBinding(key.WithKeys("up", "k")),
	Down:        key.NewBinding(key.WithKeys("down", "j")),
}

type gitPanelFocus int

const (
	focusFiles gitPanelFocus = iota
	focusInspector
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
type gitPullRequestMsg struct{ gitMsg }
type gitContinueSyncMsg struct{ gitMsg }
type gitLogRequestMsg struct{ gitMsg }
type gitCommitRequestMsg struct {
	gitMsg
	Message  string
	ThenSync bool
}
type gitAddRemoteRequestMsg struct {
	gitMsg
	URL      string
	ThenSync bool
}
type gitConnectRemoteSyncRequestMsg struct{ gitMsg }
type gitConnectRemoteSyncDoneMsg struct {
	gitMsg
	Err error
}
type gitRefreshMsg struct{ gitMsg }
type gitDiffForFileMsg struct {
	gitMsg
	Path     string
	Status   string
	OrigPath string
}

// GitPanel is a single-flow changes sheet. The selected-file diff is part of
// the reading flow, rather than a second focusable pane with its own mode.
type GitPanel struct {
	active       bool
	input        gitPanelInput
	focus        gitPanelFocus
	rightView    gitPanelRightView
	repoRoot     string
	branch       string
	hasRemote    bool
	files        []kbrdfs.FileChange
	cursor       int
	commitIn     textinput.Model
	remoteIn     textinput.Model
	thenSync     bool
	right        viewport.Model
	rightTitle   string
	rightContent string
	logContent   string
	diffCache    map[string]string
	termW        int
	termH        int
	palette      theme.Palette
}

// maxInspectorLines keeps the changes sheet stable while selecting files with
// differently sized diffs. Long content scrolls; short content leaves a small,
// predictable amount of breathing room instead of resizing the whole dialog.
const (
	maxInspectorLines = 12
	sectionMarginH    = 1
	logRailMinWidth   = 96
	logRailWidth      = 30
)

func sectionFrameWidth(bodyW int) int {
	return max(bodyW-2*sectionMarginH, 30)
}

func sectionMargin(content string) string {
	return lipgloss.NewStyle().Margin(0, sectionMarginH).Render(content)
}

func (p *GitPanel) showLogRail() bool {
	bodyW, _ := p.dims()
	return bodyW >= logRailMinWidth
}

func (p *GitPanel) mainWidth(bodyW int) int {
	if p.showLogRail() {
		return bodyW - logRailWidth - 1
	}
	return bodyW
}

// SetPalette updates the panel's palette and restyles any pre-built inputs.
func (p *GitPanel) SetPalette(pal theme.Palette) {
	p.palette = pal
	theme.ApplyTextInputPalette(&p.commitIn, pal)
	theme.ApplyTextInputPalette(&p.remoteIn, pal)
}

func (p *GitPanel) Active() bool { return p.active }

func (p *GitPanel) Open(repoRoot, branch string, hasRemote bool, files []kbrdfs.FileChange, termW, termH int) {
	p.active = true
	p.input = inputNone
	p.focus = focusFiles
	p.rightView = rightDiff
	p.repoRoot = repoRoot
	p.branch = branch
	p.hasRemote = hasRemote
	p.files = files
	p.cursor = 0
	p.thenSync = false
	p.diffCache = map[string]string{}
	p.logContent = "Loading history…"
	p.termW = termW
	p.termH = termH
	p.commitIn = newPanelInput("  message: ", 200, p.palette)
	p.remoteIn = newPanelInput("  remote URL: ", 300, p.palette)
	p.remoteIn.Placeholder = "git@github.com:you/board.git"
	p.rebuild()
}

func newPanelInput(prompt string, charLimit int, pal theme.Palette) textinput.Model {
	ti := textinput.New()
	ti.Prompt = prompt
	ti.CharLimit = charLimit
	ti.SetWidth(60)
	theme.ApplyTextInputPalette(&ti, pal)
	return ti
}

func (p *GitPanel) Refresh(branch string, hasRemote bool, files []kbrdfs.FileChange, termW, termH int) {
	p.branch = branch
	p.hasRemote = hasRemote
	p.files = files
	p.cursor = min(p.cursor, max(len(files)-1, 0))
	p.termW = termW
	p.termH = termH
	p.diffCache = map[string]string{}
	p.rebuild()
}

// dims returns the changes sheet's usable body dimensions.
func (p *GitPanel) dims() (bodyW, bodyH int) {
	bodyW = p.termW - 20
	if bodyW > 112 {
		bodyW = 112
	}
	if bodyW < 52 {
		bodyW = 52
	}
	bodyH = p.termH - 10
	if bodyH > 28 {
		bodyH = 28
	}
	if bodyH < 10 {
		bodyH = 10
	}
	return
}

func (p *GitPanel) fileListHeight() int {
	if len(p.files) == 0 || p.rightView == rightLog {
		return 0
	}
	// Titled top border, rows, and bottom border.
	return min(len(p.files), 5) + 2
}

func (p *GitPanel) rebuild() {
	bodyW, bodyH := p.dims()
	bodyW = p.mainWidth(bodyW)
	// Status, a gap, inspector title, and the selected-file list share the
	// available height. Size the sheet to short diffs, then cap long diffs so
	// the board remains visible behind the overlay.
	maxH := max(bodyH-5-p.fileListHeight(), 5)
	// Keep one cell for the scroll position bar so the diff never reflows as it
	// grows past the viewport height.
	sectionW := theme.RoundedFrameContentWidth(sectionFrameWidth(bodyW), 1)
	vpW := max(sectionW-1, 10)
	vpH := min(maxH, maxInspectorLines)
	vp := viewport.New(viewport.WithWidth(vpW), viewport.WithHeight(vpH))
	vp.SetContent(p.rightContent)
	p.right = vp
	if p.rightTitle == "" {
		p.rightTitle = "Current diff"
	}
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
	p.logContent = ""
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
	p.rightTitle = "Current diff"
	p.rightContent = content
	p.rebuild()
	p.right.GotoTop()
}

func (p *GitPanel) SetLog(content string) {
	p.logContent = content
	if p.showLogRail() {
		return
	}
	p.rightView = rightLog
	p.rightTitle = "History"
	p.rightContent = content
	p.rebuild()
}

func (p *GitPanel) CurrentFile() (kbrdfs.FileChange, bool) {
	if len(p.files) == 0 || p.cursor < 0 || p.cursor >= len(p.files) {
		return kbrdfs.FileChange{}, false
	}
	return p.files[p.cursor], true
}

func (p *GitPanel) RightView() gitPanelRightView { return p.rightView }

func (p *GitPanel) DiffRequestForCurrent() tea.Cmd {
	f, ok := p.CurrentFile()
	if !ok {
		p.rightTitle = "No changes"
		p.rightContent = "No local changes to review."
		p.rebuild()
		return nil
	}
	if cached, hit := p.diffCache[f.Path]; hit {
		p.rightTitle = "Current diff"
		p.rightContent = cached
		p.rebuild()
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

	if p.input == inputCommit {
		switch {
		case key.Matches(km, panelKeys.Cancel):
			p.input = inputNone
			p.commitIn.Blur()
			p.thenSync = false
			return nil
		case km.String() == "enter":
			message, thenSync := p.commitIn.Value(), p.thenSync
			p.commitIn.Blur()
			p.input, p.thenSync = inputNone, false
			return func() tea.Msg { return gitCommitRequestMsg{Message: message, ThenSync: thenSync} }
		}
		var cmd tea.Cmd
		p.commitIn, cmd = p.commitIn.Update(km)
		return cmd
	}

	if p.input == inputRemote {
		switch {
		case key.Matches(km, panelKeys.Cancel):
			p.input = inputNone
			p.remoteIn.Blur()
			return nil
		case km.String() == "enter":
			url := p.remoteIn.Value()
			p.remoteIn.Blur()
			p.input = inputNone
			return func() tea.Msg { return gitAddRemoteRequestMsg{URL: url, ThenSync: true} }
		}
		var cmd tea.Cmd
		p.remoteIn, cmd = p.remoteIn.Update(km)
		return cmd
	}

	if key.Matches(km, panelKeys.Close) {
		return func() tea.Msg { return gitPanelCloseMsg{} }
	}
	if key.Matches(km, panelKeys.FocusToggle) && p.rightView == rightDiff && len(p.files) > 0 {
		p.toggleFocus()
		return nil
	}
	if key.Matches(km, panelKeys.History) {
		if p.showLogRail() {
			return nil
		}
		p.focus = focusInspector
		return func() tea.Msg { return gitLogRequestMsg{} }
	}
	if key.Matches(km, panelKeys.Changes) {
		p.rightView = rightDiff
		p.focus = focusFiles
		p.rebuild()
		return p.DiffRequestForCurrent()
	}
	if !p.hasRemote && key.Matches(km, panelKeys.Connect) {
		p.startRemoteInput()
		return nil
	}
	if p.hasRemote && key.Matches(km, panelKeys.Pull) {
		return func() tea.Msg { return gitPullRequestMsg{} }
	}
	if len(p.files) > 0 && key.Matches(km, panelKeys.Save) {
		p.startCommitInput(p.hasRemote)
		return nil
	}
	if p.focus == focusFiles && p.rightView == rightDiff && len(p.files) > 0 {
		old := p.cursor
		switch {
		case key.Matches(km, panelKeys.Up):
			p.cursor = max(p.cursor-1, 0)
		case key.Matches(km, panelKeys.Down):
			p.cursor = min(p.cursor+1, len(p.files)-1)
		}
		if old != p.cursor {
			return p.DiffRequestForCurrent()
		}
	}
	var cmd tea.Cmd
	p.right, cmd = p.right.Update(km)
	return cmd
}

func (p *GitPanel) toggleFocus() {
	if p.focus == focusFiles {
		p.focus = focusInspector
		return
	}
	p.focus = focusFiles
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

// renderScrollbar renders a fixed-width track with a position-aware thumb.
// It is always present: a diff won't suddenly reflow one cell narrower only
// after it becomes long enough to need scrolling.
func (p *GitPanel) renderScrollbar(height int) string {
	if height < 1 {
		return ""
	}
	track := lipgloss.NewStyle().Foreground(p.palette.FgDim).Render("│")
	total := p.right.TotalLineCount()
	if total <= height || total == 0 {
		return strings.Repeat(track+"\n", height-1) + track
	}
	thumbH := min(max(height*height/total, 1), height)
	thumbStart := min(max(int(float64(height-thumbH)*p.right.ScrollPercent()), 0), height-thumbH)
	thumb := lipgloss.NewStyle().Background(p.palette.Primary).Render(" ")
	lines := make([]string, height)
	for i := range lines {
		if i >= thumbStart && i < thumbStart+thumbH {
			lines[i] = thumb
		} else {
			lines[i] = track
		}
	}
	return strings.Join(lines, "\n")
}

func (p *GitPanel) inspectorTitle() string {
	title := p.rightTitle
	if p.focus == focusInspector {
		title = "› " + title
	}
	if p.right.TotalLineCount() <= p.right.Height() {
		return title
	}
	return fmt.Sprintf("%s · %d%%", title, int(p.right.ScrollPercent()*100))
}

func (p *GitPanel) startCommitInput(thenSync bool) {
	p.input, p.thenSync = inputCommit, thenSync
	p.commitIn.SetValue(time.Now().Format("2006-01-02 15:04:05"))
	p.commitIn.CursorEnd()
	p.commitIn.Focus()
}

func (p *GitPanel) startRemoteInput() {
	p.input = inputRemote
	p.remoteIn.SetValue("")
	p.remoteIn.Focus()
}

func (p *GitPanel) renderFiles(width int) string {
	if len(p.files) == 0 || p.rightView == rightLog {
		return ""
	}
	border := p.palette.BorderMuted
	titleStyle := theme.OverlayTitleStyle(p.palette).Foreground(p.palette.FgMuted)
	title := "Files to save"
	if p.focus == focusFiles {
		border = p.palette.BorderActive
		titleStyle = theme.OverlayTitleStyle(p.palette)
		title = "› " + title
	}
	frameW := sectionFrameWidth(width)
	contentW := theme.RoundedFrameContentWidth(frameW, 1)
	rows := make([]string, 0, min(len(p.files), 5)+1)
	for i, f := range p.files[:min(len(p.files), 5)] {
		path := f.Path
		if f.OrigPath != "" {
			path = f.OrigPath + " → " + f.Path
		}
		stats := ""
		if f.Added > 0 || f.Deleted > 0 {
			stats = fmt.Sprintf("+%d −%d", f.Added, f.Deleted)
		}
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(p.palette.FgMuted)
		if i == p.cursor {
			prefix = "› "
			style = lipgloss.NewStyle().Bold(true).Foreground(p.palette.FgBase)
		}
		status := changeStatus(f.Status)
		rowPrefix := prefix + status + "  "
		pathW := max(contentW-lipgloss.Width(rowPrefix)-lipgloss.Width(stats)-1, 12)
		line := rowPrefix + ansi.Truncate(path, pathW, "…")
		if stats != "" {
			line += strings.Repeat(" ", max(contentW-lipgloss.Width(line)-lipgloss.Width(stats), 1)) + stats
		}
		rows = append(rows, style.Render(line))
	}
	if more := len(p.files) - 5; more > 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.palette.FgDim).Render(fmt.Sprintf("  … %d more", more)))
	}
	return sectionMargin(theme.RoundedFrame(title, titleStyle, strings.Join(rows, "\n"), border, 0, 1, frameW))
}

// changeStatus turns Git's two-column porcelain status into the one thing this
// sheet needs to convey: what will happen to the file. A leading space in a
// worktree-only modification must not become an invisible status marker.
func changeStatus(status string) string {
	switch s := strings.TrimSpace(status); {
	case s == "":
		return "?"
	case s == "??":
		return "A"
	case strings.Contains(s, "D"):
		return "D"
	case strings.Contains(s, "R"):
		return "R"
	default:
		return "M"
	}
}

func (p *GitPanel) stateLine() string {
	primary := lipgloss.NewStyle().Bold(true).Foreground(p.palette.FgBase)
	muted := lipgloss.NewStyle().Foreground(p.palette.FgMuted)
	state := "Working tree clean"
	if n := len(p.files); n > 0 {
		unit := "uncommitted files"
		if n == 1 {
			unit = "uncommitted file"
		}
		state = fmt.Sprintf("%d %s", n, unit)
	}
	destination := "Local only"
	if p.hasRemote {
		destination = "Connected to origin"
	}
	return primary.Render(state) + muted.Render("  ·  "+destination)
}

func (p *GitPanel) renderLogRail(width, height int) string {
	contentW := theme.RoundedFrameContentWidth(width, 1)
	contentH := max(height-2, 4)
	entries := strings.Split(strings.TrimSpace(p.logContent), "\n")
	if len(entries) == 0 || entries[0] == "" {
		entries = []string{"Loading history…"}
	}
	rows := make([]string, 0, contentH)
	for _, entry := range entries[:min(len(entries), min(contentH, 10))] {
		rows = append(rows, ansi.Truncate(entry, contentW, "…"))
	}
	for len(rows) < contentH {
		rows = append(rows, "")
	}
	return theme.RoundedFrame(
		"Recent commits",
		theme.OverlayTitleStyle(p.palette).Foreground(p.palette.FgMuted),
		strings.Join(rows, "\n"),
		p.palette.BorderMuted,
		0,
		1,
		width,
	)
}

func (p *GitPanel) View() string {
	bodyW, _ := p.dims()
	mainW := p.mainWidth(bodyW)
	branch := p.branch
	if branch == "" {
		branch = "(no branch)"
	}

	sections := []string{p.stateLine(), ""}
	if files := p.renderFiles(mainW); files != "" {
		sections = append(sections, files, "")
	}
	inspector := lipgloss.JoinHorizontal(lipgloss.Top, p.right.View(), p.renderScrollbar(p.right.Height()))
	inspectorBorder := p.palette.BorderMuted
	inspectorStyle := theme.OverlayTitleStyle(p.palette).Foreground(p.palette.FgMuted)
	if p.focus == focusInspector {
		inspectorBorder = p.palette.BorderActive
		inspectorStyle = theme.OverlayTitleStyle(p.palette)
	}
	sections = append(sections, sectionMargin(theme.RoundedFrame(p.inspectorTitle(), inspectorStyle, inspector, inspectorBorder, 0, 1, sectionFrameWidth(mainW))))
	main := lipgloss.JoinVertical(lipgloss.Left, sections...)
	body := main
	if p.showLogRail() {
		body = lipgloss.JoinHorizontal(lipgloss.Top, main, " ", p.renderLogRail(logRailWidth, lipgloss.Height(main)))
	}

	hints := []theme.Hint{}
	if len(p.files) > 0 {
		if p.hasRemote {
			hints = append(hints, theme.Hint{Keys: "c", Label: "save+sync"})
		} else {
			hints = append(hints, theme.Hint{Keys: "c", Label: "save"})
		}
	}
	if p.hasRemote {
		hints = append(hints, theme.Hint{Keys: "s", Label: "pull"})
	} else {
		hints = append(hints, theme.Hint{Keys: "a", Label: "remote"})
	}
	if p.rightView == rightLog {
		hints = append(hints, theme.Hint{Keys: "d", Label: "changes"})
	} else if len(p.files) > 0 {
		if p.focus == focusFiles {
			hints = append(hints, theme.Hint{Keys: "↑/↓", Label: "file"}, theme.Hint{Keys: "tab", Label: "diff"})
		} else {
			hints = append(hints, theme.Hint{Keys: "↑/↓", Label: "scroll"}, theme.Hint{Keys: "tab", Label: "files"})
		}
	}
	if !p.showLogRail() {
		hints = append(hints, theme.Hint{Keys: "l", Label: "log"})
	}
	if bodyW < 80 {
		// Keep the focus instructions on one line in an 80-column terminal.
		hints = append(hints, theme.Hint{Keys: "esc", Label: ""})
	} else {
		hints = append(hints, theme.Hint{Keys: "q/esc", Label: "close"})
	}

	panel := theme.OverlayFrame{
		Title:   "Changes · " + branch,
		Body:    body,
		Footer:  theme.RenderHints(p.palette, hints),
		Width:   theme.RoundedFrameWidthForContent(bodyW, theme.OverlayPadH),
		Palette: p.palette,
	}.Render()
	if p.input != inputNone {
		return lipgloss.Place(lipgloss.Width(panel), lipgloss.Height(panel), lipgloss.Center, lipgloss.Center, p.inputDialog(), lipgloss.WithWhitespaceChars(" "))
	}
	return panel
}

func (p *GitPanel) inputDialog() string {
	var title, body, hint string
	switch p.input {
	case inputCommit:
		title = "Save changes"
		confirm := "save changes"
		explanation := "Save a Git commit locally."
		if p.thenSync {
			title = "Save & sync"
			confirm = "save & sync"
			explanation = "Save locally, then sync with origin so this board is available on your other machines."
		}
		body = lipgloss.JoinVertical(lipgloss.Left, p.commitIn.View(), "", lipgloss.NewStyle().Foreground(p.palette.FgMuted).Render(explanation))
		hint = theme.RenderHints(p.palette, []theme.Hint{{Keys: "enter", Label: confirm}, {Keys: "esc", Label: "cancel"}})
	case inputRemote:
		title = "Connect a remote"
		body = lipgloss.JoinVertical(lipgloss.Left, p.remoteIn.View(), "", lipgloss.NewStyle().Foreground(p.palette.FgMuted).Render("Connect this board to a Git remote, then sync it now."))
		hint = theme.RenderHints(p.palette, []theme.Hint{{Keys: "enter", Label: "connect & sync"}, {Keys: "esc", Label: "cancel"}})
	}
	content := lipgloss.JoinVertical(lipgloss.Left, body, "", hint)
	return theme.OverlayFrame{Title: title, Body: content, Palette: p.palette}.Render()
}
