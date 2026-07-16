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
	Save, Pull, History, Changes, Review, Connect, Cancel, Close, FocusNext, FocusPrev, Up, Down key.Binding
}{
	Save:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "save & sync")),
	Pull:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "pull")),
	History:   key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "history")),
	Changes:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "changes")),
	Review:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "review changes")),
	Connect:   key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "connect remote")),
	Close:     key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("q/esc", "close")),
	Cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	FocusNext: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next section")),
	FocusPrev: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous section")),
	Up:        key.NewBinding(key.WithKeys("up", "k")),
	Down:      key.NewBinding(key.WithKeys("down", "j")),
}

type gitPanelFocus int

const (
	focusFiles gitPanelFocus = iota
	focusInspector
	focusLog
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
type gitReviewRequestMsg struct{ gitMsg }
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

// GitPanel is a single-flow changes sheet whose visible sections participate
// in one focus ring and keep independent scroll positions.
type GitPanel struct {
	active       bool
	input        gitPanelInput
	focus        gitPanelFocus
	rightView    gitPanelRightView
	repoRoot     string
	branch       string
	hasRemote    bool
	conflicts    int
	files        []kbrdfs.FileChange
	cursor       int
	fileOffset   int
	commitIn     textinput.Model
	remoteIn     textinput.Model
	thenSync     bool
	right        viewport.Model
	log          viewport.Model
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
	maxFileLines      = 5
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

// SetConflictCount controls whether the panel offers its incoming-change review action.
func (p *GitPanel) SetConflictCount(count int) { p.conflicts = max(count, 0) }

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
	p.fileOffset = 0
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
	p.followFileCursor()
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
	return min(len(p.files), maxFileLines) + 2
}

func (p *GitPanel) rebuild() {
	rightOffset := p.right.YOffset()
	logOffset := p.log.YOffset()
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
	vp.SetYOffset(rightOffset)
	p.right = vp
	if p.rightTitle == "" {
		p.rightTitle = "Current diff"
	}

	logW := theme.RoundedFrameContentWidth(logRailWidth, 1)
	logH := max(p.mainHeight()-2, 4)
	log := viewport.New(viewport.WithWidth(max(logW-1, 1)), viewport.WithHeight(logH))
	log.SetContent(p.logViewportContent(max(logW-1, 1)))
	log.SetYOffset(logOffset)
	p.log = log
	p.normalizeFocus()
}

func (p *GitPanel) Close() {
	p.active = false
	p.files = nil
	p.fileOffset = 0
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
		p.rebuild()
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
	if key.Matches(km, panelKeys.FocusNext) {
		p.cycleFocus(1)
		return nil
	}
	if key.Matches(km, panelKeys.FocusPrev) {
		p.cycleFocus(-1)
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
	if p.conflicts > 0 && key.Matches(km, panelKeys.Review) {
		return func() tea.Msg { return gitReviewRequestMsg{} }
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
	if key.Matches(km, panelKeys.Up) {
		return p.scrollFocused(-1)
	}
	if key.Matches(km, panelKeys.Down) {
		return p.scrollFocused(1)
	}
	var cmd tea.Cmd
	if p.focus == focusLog {
		p.log, cmd = p.log.Update(km)
	} else {
		p.right, cmd = p.right.Update(km)
	}
	return cmd
}

func (p *GitPanel) visibleSections() []gitPanelFocus {
	sections := make([]gitPanelFocus, 0, 3)
	if len(p.files) > 0 && p.rightView != rightLog {
		sections = append(sections, focusFiles)
	}
	sections = append(sections, focusInspector)
	if p.showLogRail() {
		sections = append(sections, focusLog)
	}
	return sections
}

func (p *GitPanel) normalizeFocus() {
	sections := p.visibleSections()
	for _, section := range sections {
		if p.focus == section {
			return
		}
	}
	p.focus = sections[0]
}

func (p *GitPanel) cycleFocus(delta int) {
	sections := p.visibleSections()
	current := 0
	for i, section := range sections {
		if p.focus == section {
			current = i
			break
		}
	}
	p.focus = sections[(current+delta+len(sections))%len(sections)]
}

func (p *GitPanel) scrollFocused(delta int) tea.Cmd {
	switch p.focus {
	case focusFiles:
		old := p.cursor
		p.cursor = min(max(p.cursor+delta, 0), max(len(p.files)-1, 0))
		p.followFileCursor()
		if old != p.cursor {
			return p.DiffRequestForCurrent()
		}
	case focusLog:
		if delta < 0 {
			p.log.ScrollUp(-delta)
		} else {
			p.log.ScrollDown(delta)
		}
	default:
		if delta < 0 {
			p.right.ScrollUp(-delta)
		} else {
			p.right.ScrollDown(delta)
		}
	}
	return nil
}

func (p *GitPanel) followFileCursor() {
	height := min(len(p.files), maxFileLines)
	if height == 0 {
		p.fileOffset = 0
		return
	}
	if p.cursor < p.fileOffset {
		p.fileOffset = p.cursor
	}
	if p.cursor >= p.fileOffset+height {
		p.fileOffset = p.cursor - height + 1
	}
	p.fileOffset = min(max(p.fileOffset, 0), max(len(p.files)-height, 0))
}

func (p *GitPanel) HandleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		return p.scrollFocused(-3)
	case tea.MouseWheelDown:
		return p.scrollFocused(3)
	}
	return nil
}

// renderScrollbar renders a fixed-width track with a position-aware thumb.
// It is always present so section content never reflows when it starts to
// overflow.
func (p *GitPanel) renderScrollbar(height, total, offset int) string {
	if height < 1 {
		return ""
	}
	track := lipgloss.NewStyle().Foreground(p.palette.FgDim).Render("│")
	if total <= height || total == 0 {
		return strings.Repeat(track+"\n", height-1) + track
	}
	thumbH := min(max(height*height/total, 1), height)
	maxOffset := total - height
	thumbStart := min(max((height-thumbH)*offset/maxOffset, 0), height-thumbH)
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
	contentW := theme.RoundedFrameContentWidth(frameW, 1) - 1
	height := min(len(p.files), maxFileLines)
	end := min(p.fileOffset+height, len(p.files))
	rows := make([]string, 0, height)
	for i, f := range p.files[p.fileOffset:end] {
		fileIndex := p.fileOffset + i
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
		if fileIndex == p.cursor {
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
		line += strings.Repeat(" ", max(contentW-lipgloss.Width(line), 0))
		rows = append(rows, style.Render(line))
	}
	list := lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(rows, "\n"), p.renderScrollbar(height, len(p.files), p.fileOffset))
	return sectionMargin(theme.RoundedFrame(title, titleStyle, list, border, 0, 1, frameW))
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

func (p *GitPanel) logViewportContent(width int) string {
	entries := strings.Split(strings.TrimSpace(p.logContent), "\n")
	if len(entries) == 0 || entries[0] == "" {
		entries = []string{"Loading history…"}
	}
	rows := make([]string, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, ansi.Truncate(entry, width, "…"))
	}
	return strings.Join(rows, "\n")
}

func (p *GitPanel) mainHeight() int {
	height := 2 + p.right.Height() + 2
	if filesH := p.fileListHeight(); filesH > 0 {
		height += filesH + 1
	}
	return height
}

func (p *GitPanel) renderLogRail(width int) string {
	border := p.palette.BorderMuted
	titleStyle := theme.OverlayTitleStyle(p.palette).Foreground(p.palette.FgMuted)
	title := "Recent commits"
	if p.focus == focusLog {
		border = p.palette.BorderActive
		titleStyle = theme.OverlayTitleStyle(p.palette)
		title = "› " + title
	}
	content := lipgloss.JoinHorizontal(lipgloss.Top, p.log.View(), p.renderScrollbar(p.log.Height(), p.log.TotalLineCount(), p.log.YOffset()))
	return theme.RoundedFrame(
		title,
		titleStyle,
		content,
		border,
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
	inspector := lipgloss.JoinHorizontal(lipgloss.Top, p.right.View(), p.renderScrollbar(p.right.Height(), p.right.TotalLineCount(), p.right.YOffset()))
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
		body = lipgloss.JoinHorizontal(lipgloss.Top, main, " ", p.renderLogRail(logRailWidth))
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
	if p.conflicts > 0 {
		hints = append(hints, theme.Hint{Keys: "r", Label: "review"})
	}
	if p.rightView == rightLog {
		hints = append(hints, theme.Hint{Keys: "d", Label: "changes"}, theme.Hint{Keys: "↑/↓", Label: "scroll"})
	} else {
		label := "scroll"
		if p.focus == focusFiles {
			label = "file"
		}
		hints = append(hints, theme.Hint{Keys: "↑/↓", Label: label})
	}
	if len(p.visibleSections()) > 1 {
		hints = append(hints, theme.Hint{Keys: "tab/shift+tab", Label: "section"})
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
