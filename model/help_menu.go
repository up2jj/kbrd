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

	groups []HelpGroup // board-composed source, re-filtered on the fly
	rows   []helpRow   // visible lines (headers + entries), in display order
	groupedPicker
	context string // focused column name, shown in the title
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
	m.groupedPicker.Reset()
	m.recompute()
}

// recompute rebuilds the visible rows and the navigable set from the groups and
// the current filter. Unfiltered: grouped headers + every entry. Filtered: a
// flat, fuzzy-ranked list of runnable entries with match offsets for highlight.
func (m *HelpMenu) recompute() {
	m.rows = m.rows[:0]
	m.groupedMenuNav.BeginRebuild()

	if m.filtering && m.filter != "" {
		var cands []HelpEntry
		for _, g := range m.groups {
			for _, e := range g.Items {
				if e.Disabled || (e.RunKey == "" && e.CmdID == "" && e.ActionID == "") {
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
				if !e.Disabled && (e.RunKey != "" || e.CmdID != "" || e.ActionID != "") {
					m.nav = append(m.nav, len(m.rows)-1)
				}
			}
		}
	}

	m.groupedMenuNav.Clamp(len(m.rows))
}

// Filtering reports whether the menu is in search mode.
func (m *HelpMenu) Filtering() bool { return m.filtering }

// StartFilter enters search mode with an empty query.
func (m *HelpMenu) StartFilter() {
	m.groupedPicker.StartFilter()
	m.recompute()
}

// StopFilter leaves search mode and restores the grouped list.
func (m *HelpMenu) StopFilter() {
	m.groupedPicker.StopFilter()
	m.recompute()
}

// AppendFilter adds typed text to the query; Backspace removes the last rune
// (leaving search mode when the query empties).
func (m *HelpMenu) AppendFilter(s string) {
	m.groupedPicker.AppendFilter(s)
	m.recompute()
}

func (m *HelpMenu) Backspace() {
	if m.groupedPicker.Backspace() {
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
	row, ok := m.groupedMenuNav.SelectedRow()
	if !ok {
		return HelpEntry{}
	}
	return m.rows[row].entry
}

// Update moves the item selection in response to a navigation key. The board
// calls it only after direct-key execution has had first claim, so a key that
// names a runnable row runs it and never reaches navigation. ↑/↓ and vim j/k
// move within the list; ←/→ (handled by the board) switch the focused column.
func (m *HelpMenu) Update(msg tea.KeyPressMsg) {
	m.groupedMenuNav.UpdateKey(msg.String())
}

func (m *HelpMenu) ScrollBy(delta int) {
	m.groupedMenuNav.ScrollBy(len(m.rows), delta)
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

	// Footer: hints on the left, "N of M" position indicator on the right.
	hints := m.footerHints()
	textW := m.contentWidth(termWidth, keyCol)
	body, pos := renderGroupedPickerBody(groupedPickerBody{
		Palette: p, Rows: len(m.rows), TermHeight: termHeight, TextWidth: textW,
		Filtering: m.filtering, Filter: m.filter, Nav: &m.groupedMenuNav,
		RenderRow: func(row int, selected bool) string { return m.renderRow(m.rows[row], selected, keyCol) },
	})

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
	pane := theme.RoundedFrame("", theme.OverlayTitleStyle(p), descBlock, p.BorderMuted, 0, overlayPadH, boxWidth)

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
			if !e.Disabled && (e.RunKey != "" || e.CmdID != "" || e.ActionID != "") {
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

func (m *HelpMenu) selectedDescBlock(width int) string {
	desc := m.SelectedDesc()
	if lipgloss.Width(desc) > width {
		return ansi.Truncate(desc, width, "…")
	}
	return desc
}

// SelectedDesc returns the tooltip of the highlighted row.
func (m *HelpMenu) SelectedDesc() string {
	row, ok := m.groupedMenuNav.SelectedRow()
	if !ok {
		return ""
	}
	return m.rows[row].entry.Desc
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
