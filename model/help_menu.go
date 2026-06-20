package model

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"kbrd/theme"
)

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
}

// Filtering reports whether the menu is in search mode.
func (m *HelpMenu) Filtering() bool { return m.filtering }

// StartFilter enters search mode with an empty query.
func (m *HelpMenu) StartFilter() {
	m.filtering = true
	m.filter = ""
	m.selected = 0
	m.recompute()
}

// StopFilter leaves search mode and restores the grouped list.
func (m *HelpMenu) StopFilter() {
	m.filtering = false
	m.filter = ""
	m.selected = 0
	m.recompute()
}

// AppendFilter adds typed text to the query; Backspace removes the last rune
// (leaving search mode when the query empties).
func (m *HelpMenu) AppendFilter(s string) {
	m.filter += s
	m.selected = 0
	m.recompute()
}

func (m *HelpMenu) Backspace() {
	if r := []rune(m.filter); len(r) > 0 {
		m.filter = string(r[:len(r)-1])
		m.selected = 0
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
func (m *HelpMenu) Update(msg tea.KeyMsg) {
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

	// Column width for keys, so labels align across every group.
	keyW := 0
	for _, r := range m.rows {
		if !r.header {
			if w := lipgloss.Width(r.entry.Keys); w > keyW {
				keyW = w
			}
		}
	}

	// Keys sit in a right-aligned column so labels line up across every group.
	keyCol := lipgloss.NewStyle().Width(keyW).Align(lipgloss.Right)
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgBase)
	labelStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Primary)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	disabledStyle := lipgloss.NewStyle().Foreground(p.FgDim).Strikethrough(true)
	hiStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight)
	hiSelStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Background(p.Primary)
	gutterSel := lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("▌")

	// Vertical budget: reserve the title border, padding, footer, and desc pane.
	maxBody := max(termHeight-12, 6)
	selRow := -1
	if m.selected < len(m.nav) {
		selRow = m.nav[m.selected]
	}
	start, end := windowRows(len(m.rows), maxBody, selRow)

	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		r := m.rows[i]
		if r.header {
			lines = append(lines, headerStyle.Render("── "+r.title+" ──"))
			continue
		}
		keyCell := keyCol.Render(r.entry.Keys)
		switch {
		case i == selRow:
			label := renderHighlighted(r.entry.Label, r.matchIdx, selStyle, hiSelStyle)
			lines = append(lines, gutterSel+" "+selStyle.Render(keyCell+"  ")+label+selStyle.Render(" "))
		case r.disabled:
			lines = append(lines, "  "+disabledStyle.Render(keyCell+"  "+r.entry.Label))
		default:
			label := renderHighlighted(r.entry.Label, r.matchIdx, labelStyle, hiStyle)
			lines = append(lines, "  "+keyStyle.Render(keyCell)+"  "+label)
		}
	}
	if m.filtering && len(m.nav) == 0 {
		lines = append(lines, "  "+helpDimStyle.Render("no matches"))
	}

	// Footer: hints on the left, "N of M" position indicator on the right.
	var hints string
	if m.filtering {
		hints = RenderInlineHints([]Shortcut{
			{"type", "filter"}, {"↑/↓", "select"}, {"enter", "run"}, {"esc", "clear"},
		})
	} else {
		hints = RenderInlineHints([]Shortcut{
			{"↑/↓", "item"}, {"←/→", "column"}, {"/", "search"}, {"enter/key", "run"}, {"esc", "close"},
		})
	}
	cur := 0
	if len(m.nav) > 0 {
		cur = m.selected + 1
	}
	pos := helpDimStyle.Render(fmt.Sprintf("%d of %d", cur, len(m.nav)))

	// Size the text area to the widest row (and the footer), clamped to the
	// terminal; truncate any line that still overflows so nothing wraps.
	textW := lipgloss.Width(hints) + 2 + lipgloss.Width(pos)
	for _, l := range lines {
		textW = max(textW, lipgloss.Width(l))
	}
	textW = min(textW, termWidth-8)
	for i, l := range lines {
		if lipgloss.Width(l) > textW {
			lines[i] = ansi.Truncate(l, textW, "…")
		}
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)

	// While searching, a filter prompt sits above the results.
	if m.filtering {
		cursor := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Render("> ")
		query := m.filter
		if query == "" {
			query = labelStyle.Render("type to filter…")
		} else {
			query = lipgloss.NewStyle().Foreground(p.Highlight).Render(query)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, cursor+query, "", body)
	}

	gap := max(textW-lipgloss.Width(hints)-lipgloss.Width(pos), 1)
	footer := hints + strings.Repeat(" ", gap) + pos

	title := "Keybindings"
	if m.context != "" {
		title += " · " + m.context
	}
	boxWidth := textW + 2*overlayPadH
	menu := OverlayFrame{
		Title:   title,
		Body:    body,
		Footer:  footer,
		Width:   boxWidth,
		Palette: p,
	}.Render()

	// Description pane: a thin box below the menu, same width, showing the
	// highlighted row's tooltip.
	descBlock := lipgloss.NewStyle().Width(textW).Height(2).Foreground(p.FgMuted).Render(m.SelectedDesc())
	pane := theme.RoundedFrame("", overlayTitleStyle, descBlock, p.BorderMuted, 0, overlayPadH, boxWidth)

	return lipgloss.JoinVertical(lipgloss.Center, menu, pane)
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
