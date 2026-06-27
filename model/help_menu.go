package model

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"kbrd/theme"
)

const helpScrollbarGutter = 2

// HelpMenu is the interactive `?` keybindings overlay: grouped, navigable rows
// with a selection bar, a position indicator, a description pane, disabled rows
// for actions that don't apply, and execute-on-Enter (handled by the board).
type HelpMenu struct {
	active  bool
	palette Palette

	groups   []HelpGroup // board-composed source, re-filtered on the fly
	rows     []helpRow   // visible lines (headers + entries), in display order
	nav      []int       // indices into rows that are selectable (enabled entries)
	selected int         // index into nav
	scroll   int         // top row of the visible viewport
	follow   bool        // true when keyboard navigation should keep selection visible
	context  string      // focused column name, shown in the title

	// filtering is true while the user is searching (entered with `/`); filter
	// holds the query. While filtering, rows are a flat, fuzzy-ranked list.
	filtering bool
	filter    string
}

// helpRow is one visible line: a section header or a keybinding entry. matchIdx
// holds the fuzzy-matched rune offsets into the label (for highlighting).
type helpRow struct {
	header   bool
	title    string
	entry    HelpEntry
	disabled bool
	matchIdx []int
}

func (m *HelpMenu) Active() bool { return m.active }

func (m *HelpMenu) Close() { m.active = false }

func (m *HelpMenu) SetPalette(p Palette) { m.palette = p }

// SetContext sets the focused-column label shown in the menu title.
func (m *HelpMenu) SetContext(s string) { m.context = s }

// Open displays the board-composed groups. Disabled rows are rendered
// struck-through and skipped in navigation. The board decides what goes in the
// menu (local column commands, which rows are disabled) — the menu just renders.
func (m *HelpMenu) Open(groups []HelpGroup) {
	m.active = true
	m.groups = groups
	m.filtering = false
	m.filter = ""
	m.selected = 0
	m.scroll = 0
	m.follow = true
	m.recompute()
}

// recompute rebuilds the visible rows and the navigable set from the groups and
// the current filter. Unfiltered: grouped headers + every entry. Filtered: a
// flat, fuzzy-ranked list of runnable entries with match offsets for highlight.
func (m *HelpMenu) recompute() {
	m.rows = m.rows[:0]
	m.nav = m.nav[:0]

	if m.filtering && m.filter != "" {
		var cands []HelpEntry
		for _, g := range m.groups {
			for _, e := range g.Items {
				if e.Disabled || (e.RunKey == "" && e.CmdID == "") {
					continue // only runnable rows are searchable
				}
				cands = append(cands, e)
			}
		}
		matches := filterFuzzy(len(cands), m.filter, func(i int) string { return cands[i].Label })
		for _, mt := range matches {
			m.rows = append(m.rows, helpRow{entry: cands[mt.Index], matchIdx: mt.MatchedIndexes})
			m.nav = append(m.nav, len(m.rows)-1)
		}
	} else {
		for _, g := range m.groups {
			m.rows = append(m.rows, helpRow{header: true, title: g.Title})
			for _, e := range g.Items {
				m.rows = append(m.rows, helpRow{entry: e, disabled: e.Disabled})
				if !e.Disabled && (e.RunKey != "" || e.CmdID != "") {
					m.nav = append(m.nav, len(m.rows)-1)
				}
			}
		}
	}

	m.selected = min(max(m.selected, 0), max(len(m.nav)-1, 0))
	m.scroll = min(max(m.scroll, 0), max(len(m.rows)-1, 0))
}

// Filtering reports whether the menu is in search mode.
func (m *HelpMenu) Filtering() bool { return m.filtering }

// StartFilter enters search mode with an empty query.
func (m *HelpMenu) StartFilter() {
	m.filtering = true
	m.filter = ""
	m.selected = 0
	m.scroll = 0
	m.follow = true
	m.recompute()
}

// StopFilter leaves search mode and restores the grouped list.
func (m *HelpMenu) StopFilter() {
	m.filtering = false
	m.filter = ""
	m.selected = 0
	m.scroll = 0
	m.follow = true
	m.recompute()
}

// AppendFilter adds typed text to the query; Backspace removes the last rune
// (leaving search mode when the query empties).
func (m *HelpMenu) AppendFilter(s string) {
	m.filter += s
	m.selected = 0
	m.scroll = 0
	m.follow = true
	m.recompute()
}

func (m *HelpMenu) Backspace() {
	if r := []rune(m.filter); len(r) > 0 {
		m.filter = string(r[:len(r)-1])
		m.selected = 0
		m.scroll = 0
		m.follow = true
		m.recompute()
	} else {
		m.StopFilter()
	}
}

// SelectedRunKey returns the rune the highlighted row injects, or "" if none.
func (m *HelpMenu) SelectedRunKey() string {
	return m.SelectedEntry().RunKey
}

// SelectedEntry returns the highlighted entry (zero value if none selected).
func (m *HelpMenu) SelectedEntry() HelpEntry {
	if m.selected < 0 || m.selected >= len(m.nav) {
		return HelpEntry{}
	}
	return m.rows[m.nav[m.selected]].entry
}

// Update moves the item selection in response to a navigation key. The board
// calls it only after direct-key execution has had first claim, so a key that
// names a runnable row runs it and never reaches navigation. ↑/↓ and vim j/k
// move within the list; ←/→ (handled by the board) switch the focused column.
func (m *HelpMenu) Update(msg tea.KeyPressMsg) {
	if len(m.nav) == 0 {
		return
	}
	switch msg.String() {
	case "down", "j", "ctrl+n", "tab":
		m.selected = min(m.selected+1, len(m.nav)-1)
	case "up", "k", "ctrl+p", "shift+tab":
		m.selected = max(m.selected-1, 0)
	case "g", "home":
		m.selected = 0
	case "G", "end":
		m.selected = len(m.nav) - 1
	case "pgdown", "ctrl+d":
		m.selected = min(m.selected+10, len(m.nav)-1)
	case "pgup", "ctrl+u":
		m.selected = max(m.selected-10, 0)
	}
	m.follow = true
}

func (m *HelpMenu) ScrollBy(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.scroll = min(max(m.scroll+delta, 0), max(len(m.rows)-1, 0))
	m.follow = false
}

func (m *HelpMenu) HandleMouse(msg tea.MouseMsg) {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		m.ScrollBy(-3)
	case tea.MouseWheelDown:
		m.ScrollBy(3)
	}
}

// RunKeyFor returns the rune to inject when a runnable row's key is pressed
// directly (e.g. `e` runs edit), or "" if no enabled row uses that key.
func (m *HelpMenu) RunKeyFor(key string) string {
	for _, idx := range m.nav {
		if m.rows[idx].entry.RunKey == key {
			return key
		}
	}
	return ""
}

func (m *HelpMenu) View(termWidth, termHeight int) string {
	if !m.active {
		return ""
	}
	p := m.palette

	// Keys sit in a right-aligned column so labels line up across every group.
	keyCol := lipgloss.NewStyle().Width(m.keyColumnWidth()).Align(lipgloss.Right)
	labelStyle := lipgloss.NewStyle().Foreground(p.FgMuted)

	// Vertical budget: reserve the title border, padding, footer, and desc pane.
	maxBody := max(termHeight-12, 6)
	resultRows := maxBody
	if m.filtering {
		// The filter prompt and spacer live inside the normal body budget.
		resultRows = max(maxBody-2, 1)
	}
	selRow := -1
	if m.selected < len(m.nav) {
		selRow = m.nav[m.selected]
	}
	if m.follow {
		m.ensureSelectedVisible(resultRows)
	}
	start, end := m.viewportRows(resultRows)

	// Footer: hints on the left, "N of M" position indicator on the right.
	hints := m.footerHints()
	cur := 0
	if len(m.nav) > 0 {
		cur = m.selected + 1
	}
	pos := helpDimStyle.Render(fmt.Sprintf("%d of %d", cur, len(m.nav)))

	textW := m.contentWidth(termWidth, keyCol)
	rowW := max(textW-helpScrollbarGutter, 1)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, m.renderRow(m.rows[i], i == selRow, keyCol))
	}
	if m.filtering && len(m.nav) == 0 {
		lines = append(lines, "  "+helpDimStyle.Render("no matches"))
	}
	for len(lines) < resultRows {
		lines = append(lines, " ")
	}

	for i, l := range lines {
		if lipgloss.Width(l) > rowW {
			lines[i] = ansi.Truncate(l, rowW, "…")
		}
	}
	bodyBlock := lipgloss.NewStyle().Width(rowW).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	for lipgloss.Height(bodyBlock) < resultRows {
		bodyBlock += "\n" + strings.Repeat(" ", rowW)
	}
	if len(m.rows) > resultRows {
		bar := strings.Join(m.scrollbar(end-start, len(m.rows), start), "\n")
		bodyBlock = lipgloss.JoinHorizontal(lipgloss.Top, bodyBlock, " ", bar)
	} else {
		bodyBlock = lipgloss.JoinHorizontal(lipgloss.Top, bodyBlock, strings.Repeat(" ", helpScrollbarGutter))
	}
	body := bodyBlock

	// While searching, a filter prompt sits above the results.
	if m.filtering {
		cursor := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Render("> ")
		query := m.filter
		if query == "" {
			query = labelStyle.Render("type to filter…")
		} else {
			query = lipgloss.NewStyle().Foreground(p.Highlight).Render(query)
		}
		prompt := cursor + query
		if lipgloss.Width(prompt) > textW {
			prompt = ansi.Truncate(prompt, textW, "…")
		}
		body = lipgloss.JoinVertical(lipgloss.Left, lipgloss.NewStyle().Width(textW).Render(prompt), "", body)
	}

	gap := max(textW-lipgloss.Width(hints)-lipgloss.Width(pos)-2, 1)
	footer := hints + strings.Repeat(" ", gap) + pos

	title := "Keybindings"
	if m.context != "" {
		title += " · " + m.context
	}
	boxWidth := overlayWidthForBody(textW)
	menu := OverlayFrame{
		Title:   title,
		Body:    body,
		Footer:  footer,
		Width:   boxWidth,
		Palette: p,
	}.Render()

	// Description pane: a thin box below the menu, same width, showing the
	// highlighted row's tooltip.
	descBlock := lipgloss.NewStyle().Width(textW).Height(2).Foreground(p.FgMuted).Render(m.selectedDescBlock(textW))
	pane := theme.RoundedFrame("", overlayTitleStyle, descBlock, p.BorderMuted, 0, overlayPadH, boxWidth)

	return lipgloss.JoinVertical(lipgloss.Center, menu, pane)
}

func (m *HelpMenu) keyColumnWidth() int {
	keyW := 0
	for _, r := range m.sizingRows() {
		if !r.header {
			if w := lipgloss.Width(r.entry.Keys); w > keyW {
				keyW = w
			}
		}
	}
	return keyW
}

func (m *HelpMenu) footerHints() string {
	if m.filtering {
		return helpFilterHints()
	}
	return helpDefaultHints()
}

func helpDefaultHints() string {
	return RenderInlineHints([]Shortcut{
		{"↑/↓", "item"}, {"←/→", "column"}, {"/", "search"}, {"enter/key", "run"}, {"esc", "close"},
	})
}

func helpFilterHints() string {
	return RenderInlineHints([]Shortcut{
		{"type", "filter"}, {"↑/↓", "select"}, {"enter", "run"}, {"esc", "clear"},
	})
}

func (m *HelpMenu) contentWidth(termWidth int, keyCol lipgloss.Style) int {
	maxPos := helpDimStyle.Render(fmt.Sprintf("%d of %d", m.maxNavCount(), m.maxNavCount()))
	textW := lipgloss.Width(helpDefaultHints()) + 2 + lipgloss.Width(maxPos)
	textW = max(textW, lipgloss.Width(helpFilterHints())+2+lipgloss.Width(maxPos))
	for _, r := range m.sizingRows() {
		textW = max(textW, lipgloss.Width(m.renderRow(r, false, keyCol))+helpScrollbarGutter)
		textW = max(textW, lipgloss.Width(m.renderRow(r, true, keyCol))+helpScrollbarGutter)
	}
	if m.filtering {
		query := m.filter
		if query == "" {
			query = "type to filter…"
		}
		textW = max(textW, lipgloss.Width("> "+query))
	}
	if m.filtering && len(m.nav) == 0 {
		textW = max(textW, lipgloss.Width("  no matches")+helpScrollbarGutter)
	}
	if termWidth > 0 {
		textW = min(textW, max(termWidth-8, 1))
	}
	return max(textW, 1)
}

func (m *HelpMenu) sizingRows() []helpRow {
	if len(m.groups) == 0 {
		return m.rows
	}
	rows := make([]helpRow, 0, len(m.rows))
	for _, g := range m.groups {
		rows = append(rows, helpRow{header: true, title: g.Title})
		for _, e := range g.Items {
			rows = append(rows, helpRow{entry: e, disabled: e.Disabled})
		}
	}
	return rows
}

func (m *HelpMenu) maxNavCount() int {
	maxCount := len(m.nav)
	if len(m.groups) == 0 {
		return max(maxCount, 1)
	}
	count := 0
	for _, g := range m.groups {
		for _, e := range g.Items {
			if !e.Disabled && (e.RunKey != "" || e.CmdID != "") {
				count++
			}
		}
	}
	return max(max(maxCount, count), 1)
}

func (m *HelpMenu) renderRow(r helpRow, selected bool, keyCol lipgloss.Style) string {
	p := m.palette
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgBase)
	labelStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Primary)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	disabledStyle := lipgloss.NewStyle().Foreground(p.FgDim).Strikethrough(true)
	hiStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight)
	hiSelStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Background(p.Primary)
	gutterSel := lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("▌")

	if r.header {
		return headerStyle.Render("── " + r.title + " ──")
	}
	keyCell := keyCol.Render(r.entry.Keys)
	switch {
	case selected:
		label := renderHighlighted(r.entry.Label, r.matchIdx, selStyle, hiSelStyle)
		return gutterSel + " " + selStyle.Render(keyCell+"  ") + label + selStyle.Render(" ")
	case r.disabled:
		return "  " + disabledStyle.Render(keyCell+"  "+r.entry.Label)
	default:
		label := renderHighlighted(r.entry.Label, r.matchIdx, labelStyle, hiStyle)
		return "  " + keyStyle.Render(keyCell) + "  " + label
	}
}

func (m *HelpMenu) scrollbar(height, total, start int) []string {
	thumb := max(height*height/total, 1)
	maxStart := total - height
	pos := min(max((height-thumb)*start/maxStart, 0), height-thumb)
	track := lipgloss.NewStyle().Foreground(m.palette.FgDim).Render("│")
	bar := lipgloss.NewStyle().Foreground(m.palette.FgMuted).Render("┃")
	rows := make([]string, height)
	for i := range rows {
		if i >= pos && i < pos+thumb {
			rows[i] = bar
		} else {
			rows[i] = track
		}
	}
	return rows
}

func (m *HelpMenu) viewportRows(size int) (int, int) {
	if len(m.rows) <= size {
		m.scroll = 0
		return 0, len(m.rows)
	}
	start := min(max(m.scroll, 0), len(m.rows)-size)
	m.scroll = start
	return start, start + size
}

func (m *HelpMenu) ensureSelectedVisible(size int) {
	if size <= 0 || m.selected < 0 || m.selected >= len(m.nav) {
		return
	}
	selRow := m.nav[m.selected]
	if selRow < m.scroll {
		m.scroll = selRow
		return
	}
	if selRow >= m.scroll+size {
		m.scroll = selRow - size + 1
	}
}

func (m *HelpMenu) selectedDescBlock(width int) string {
	desc := m.SelectedDesc()
	if lipgloss.Width(desc) > width {
		return ansi.Truncate(desc, width, "…")
	}
	return desc
}

// SelectedDesc returns the tooltip of the highlighted row.
func (m *HelpMenu) SelectedDesc() string {
	if m.selected < 0 || m.selected >= len(m.nav) {
		return ""
	}
	return m.rows[m.nav[m.selected]].entry.Desc
}

// windowRows returns a [start,end) slice of total rows of at most size, keeping
// row `keep` visible. It scrolls as a block rather than line-by-line.
func windowRows(total, size, keep int) (int, int) {
	if total <= size {
		return 0, total
	}
	start := 0
	if keep >= 0 {
		// Center the kept row in the window, then clamp to bounds.
		start = keep - size/2
		start = max(start, 0)
		start = min(start, total-size)
	}
	return start, start + size
}
