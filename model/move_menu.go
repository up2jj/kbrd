package model

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type moveMenuEntry struct {
	Column columnRef
	Label  string
	Desc   string
}

type moveMenuRow struct {
	header   bool
	title    string
	entry    moveMenuEntry
	matchIdx []int
}

// MoveMenu is the grouped, fuzzy-searchable destination picker for card moves.
// The selected cards are captured when the menu opens so a marked batch moves
// as one operation when the user confirms a destination.
type MoveMenu struct {
	active  bool
	palette Palette
	source  columnRef
	targets []itemActionTarget
	rows    []moveMenuRow
	groupedPicker
}

func (m *MoveMenu) Active() bool { return m.active }

func (m *MoveMenu) SetPalette(p Palette) { m.palette = p }

func (m *MoveMenu) Close() {
	m.active = false
	m.source = columnRef{}
	m.targets = nil
	m.rows = nil
	m.groupedPicker.Reset()
}

func (m *MoveMenu) Open(source columnRef, sourceIndex int, columns []*Column, targets []itemActionTarget) {
	m.active = true
	m.source = source
	m.targets = append([]itemActionTarget(nil), targets...)
	m.groupedPicker.Reset()
	m.rows = m.rows[:0]
	m.recompute(source, sourceIndex, columns)
}

func (m *MoveMenu) recompute(source columnRef, sourceIndex int, columns []*Column) {
	m.rows = m.rows[:0]
	m.groupedMenuNav.BeginRebuild()

	if m.filtering && m.filter != "" {
		entries := m.entries(source, sourceIndex, columns)
		matches := filterFuzzy(len(entries), m.filter, func(i int) string {
			return entries[i].Label
		})
		for _, mt := range matches {
			m.rows = append(m.rows, moveMenuRow{entry: entries[mt.Index], matchIdx: mt.MatchedIndexes})
			m.nav = append(m.nav, len(m.rows)-1)
		}
	} else {
		m.appendGroup("Earlier stages", m.entriesBefore(source, sourceIndex, columns))
		m.appendGroup("Later stages", m.entriesAfter(source, sourceIndex, columns))
	}

	m.groupedMenuNav.Clamp(len(m.rows))
}

func (m *MoveMenu) entries(source columnRef, sourceIndex int, columns []*Column) []moveMenuEntry {
	entries := make([]moveMenuEntry, 0, max(len(columns)-1, 0))
	for i, col := range columns {
		if i == sourceIndex || col.Virtual || refForColumn(col) == source {
			continue
		}
		entries = append(entries, moveMenuEntry{
			Column: refForColumn(col),
			Label:  col.Name,
			Desc:   "stage " + formatMoveStage(i+1, len(columns)),
		})
	}
	return entries
}

func (m *MoveMenu) entriesBefore(source columnRef, sourceIndex int, columns []*Column) []moveMenuEntry {
	return m.entriesInRange(source, sourceIndex, columns, 0, sourceIndex)
}

func (m *MoveMenu) entriesAfter(source columnRef, sourceIndex int, columns []*Column) []moveMenuEntry {
	return m.entriesInRange(source, sourceIndex, columns, sourceIndex+1, len(columns))
}

func (m *MoveMenu) entriesInRange(source columnRef, sourceIndex int, columns []*Column, start, end int) []moveMenuEntry {
	entries := make([]moveMenuEntry, 0, max(end-start, 0))
	for i := max(start, 0); i < min(end, len(columns)); i++ {
		col := columns[i]
		if col.Virtual || i == sourceIndex || refForColumn(col) == source {
			continue
		}
		entries = append(entries, moveMenuEntry{
			Column: refForColumn(col),
			Label:  col.Name,
			Desc:   "stage " + formatMoveStage(i+1, len(columns)),
		})
	}
	return entries
}

func formatMoveStage(index, total int) string {
	return strings.Join([]string{strconv.Itoa(index), "of", strconv.Itoa(total)}, " ")
}

func (m *MoveMenu) appendGroup(title string, entries []moveMenuEntry) {
	if len(entries) == 0 {
		return
	}
	m.rows = append(m.rows, moveMenuRow{header: true, title: title})
	for _, entry := range entries {
		m.rows = append(m.rows, moveMenuRow{entry: entry})
		m.nav = append(m.nav, len(m.rows)-1)
	}
}

func (m *MoveMenu) Filtering() bool { return m.filtering }

func (m *MoveMenu) StartFilter() { m.groupedPicker.StartFilter() }

func (m *MoveMenu) StopFilter() { m.groupedPicker.StopFilter() }

func (m *MoveMenu) AppendFilter(s string) { m.groupedPicker.AppendFilter(s) }

func (m *MoveMenu) Backspace() bool { return m.groupedPicker.Backspace() }

func (m *MoveMenu) Update(msg tea.KeyPressMsg) { m.groupedMenuNav.UpdateKey(msg.String()) }

func (m *MoveMenu) SelectedEntry() (moveMenuEntry, bool) {
	row, ok := m.groupedMenuNav.SelectedRow()
	if !ok || row < 0 || row >= len(m.rows) || m.rows[row].header {
		return moveMenuEntry{}, false
	}
	return m.rows[row].entry, true
}

func (m *MoveMenu) View(termWidth, termHeight int) string {
	if !m.active {
		return ""
	}
	p := m.palette
	footer := moveMenuFooter(m.filtering)
	textW := m.contentWidth(termWidth)
	body, pos := renderGroupedPickerBody(groupedPickerBody{
		Palette: p, Rows: len(m.rows), TermHeight: termHeight, TextWidth: textW,
		Filtering: m.filtering, Filter: m.filter, Nav: &m.groupedMenuNav,
		RenderRow: func(row int, selected bool) string { return m.renderRow(m.rows[row], selected) },
	})
	gap := max(textW-lipgloss.Width(footer)-lipgloss.Width(pos), 1)
	return OverlayFrame{
		Title:   "Move card to…",
		Body:    body,
		Footer:  footer + strings.Repeat(" ", gap) + pos,
		Width:   overlayWidthForBody(textW),
		Palette: p,
	}.Render()
}

func moveMenuFooter(filtering bool) string {
	if filtering {
		return RenderInlineHints([]Shortcut{{"type", "filter"}, {"↑/↓", "select"}, {"enter", "move"}, {"esc", "clear"}})
	}
	return RenderInlineHints([]Shortcut{{"↑/↓", "select"}, {"/", "search"}, {"enter", "move"}, {"esc/q", "close"}})
}

func (m *MoveMenu) contentWidth(termWidth int) int {
	textW := lipgloss.Width(moveMenuFooter(false)) + 8
	textW = max(textW, lipgloss.Width(moveMenuFooter(true))+8)
	for _, row := range m.sizingRows() {
		textW = max(textW, lipgloss.Width(m.renderRow(row, false))+helpScrollbarGutter)
		textW = max(textW, lipgloss.Width(m.renderRow(row, true))+helpScrollbarGutter)
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

func (m *MoveMenu) sizingRows() []moveMenuRow {
	return m.rows
}

func (m *MoveMenu) renderRow(row moveMenuRow, selected bool) string {
	p := m.palette
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Primary)
	labelStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	descStyle := lipgloss.NewStyle().Foreground(p.FgSubtle)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	hiStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight)
	hiSelStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Background(p.Primary)
	if row.header {
		return headerStyle.Render("── " + row.title + " ──")
	}
	labelIdx, descIdx := splitLabelDescMatchIndexes(row.entry.Label, row.matchIdx)
	if selected {
		labelStyle, descStyle = selStyle, selStyle
		hiStyle = hiSelStyle
	}
	label := renderHighlighted(row.entry.Label, labelIdx, labelStyle, hiStyle)
	label += descStyle.Render("  —  ")
	label += renderHighlighted(row.entry.Desc, descIdx, descStyle, hiStyle)
	if selected {
		label = selStyle.Render(" ") + label + selStyle.Render(" ")
	}
	gutter := " "
	if selected {
		gutter = lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("▌")
	}
	return gutter + " " + label
}
