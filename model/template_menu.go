package model

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"kbrd/template"
)

type templateMenuAction int

const (
	templateMenuNone templateMenuAction = iota
	templateMenuAuthor
	templateMenuUse
	templateMenuEdit
	templateMenuRemove
)

type templateMenuEntryKind int

const (
	templateMenuEntryAuthor templateMenuEntryKind = iota
	templateMenuEntryTemplate
	templateMenuEntryEmpty
)

type templateMenuEntry struct {
	Kind     templateMenuEntryKind
	Label    string
	Desc     string
	Template template.Template
}

type templateMenuRow struct {
	header   bool
	title    string
	entry    templateMenuEntry
	disabled bool
	matchIdx []int
}

type TemplateMenu struct {
	active    bool
	palette   Palette
	column    columnRef
	colIndex  int
	context   string
	templates []template.Template
	rows      []templateMenuRow
	nav       []int
	selected  int
	scroll    int
	follow    bool
	filtering bool
	filter    string
}

func (m *TemplateMenu) Active() bool { return m.active }

func (m *TemplateMenu) Close() { m.active = false }

func (m *TemplateMenu) SetPalette(p Palette) { m.palette = p }

func (m *TemplateMenu) Open(colIdx int, column columnRef, templates []template.Template) {
	m.active = true
	m.column = column
	m.colIndex = colIdx
	m.context = column.Name
	m.templates = templates
	m.filtering = false
	m.filter = ""
	m.selected = 0
	m.scroll = 0
	m.follow = true
	m.recompute()
}

func (m *TemplateMenu) recompute() {
	m.rows = m.rows[:0]
	m.nav = m.nav[:0]

	if m.filtering && m.filter != "" {
		entries := m.entries()
		matches := filterFuzzy(len(entries), m.filter, func(i int) string {
			e := entries[i]
			if e.Desc != "" {
				return e.Label + "  " + e.Desc
			}
			return e.Label
		})
		for _, mt := range matches {
			m.rows = append(m.rows, templateMenuRow{entry: entries[mt.Index], matchIdx: mt.MatchedIndexes})
			m.nav = append(m.nav, len(m.rows)-1)
		}
	} else {
		m.appendGroup("Template authoring", []templateMenuEntry{newTemplateAuthorEntry()})
		m.appendGroup("Column templates", m.templateEntries(template.ScopeColumn))
		m.appendGroup("Board templates", m.templateEntries(template.ScopeBoard))
	}

	m.selected = min(max(m.selected, 0), max(len(m.nav)-1, 0))
	m.scroll = min(max(m.scroll, 0), max(len(m.rows)-1, 0))
}

func (m *TemplateMenu) appendGroup(title string, entries []templateMenuEntry) {
	m.rows = append(m.rows, templateMenuRow{header: true, title: title})
	if len(entries) == 0 {
		m.rows = append(m.rows, templateMenuRow{
			entry:    templateMenuEntry{Kind: templateMenuEntryEmpty, Label: "No templates", Desc: "Nothing in this scope"},
			disabled: true,
		})
		return
	}
	for _, entry := range entries {
		m.rows = append(m.rows, templateMenuRow{entry: entry})
		m.nav = append(m.nav, len(m.rows)-1)
	}
}

func (m *TemplateMenu) entries() []templateMenuEntry {
	entries := []templateMenuEntry{newTemplateAuthorEntry()}
	entries = append(entries, m.templateEntries(template.ScopeColumn)...)
	entries = append(entries, m.templateEntries(template.ScopeBoard)...)
	return entries
}

func (m *TemplateMenu) templateEntries(scope string) []templateMenuEntry {
	var entries []templateMenuEntry
	for _, tmpl := range m.templates {
		if tmpl.Scope != scope {
			continue
		}
		desc := "Template from this column"
		if scope == template.ScopeBoard {
			desc = "Board template"
		}
		entries = append(entries, templateMenuEntry{
			Kind:     templateMenuEntryTemplate,
			Label:    tmpl.Name,
			Desc:     desc,
			Template: tmpl,
		})
	}
	return entries
}

func newTemplateAuthorEntry() templateMenuEntry {
	return templateMenuEntry{
		Kind:  templateMenuEntryAuthor,
		Label: "New column template",
		Desc:  "Create a reusable template for this column",
	}
}

func (m *TemplateMenu) Filtering() bool { return m.filtering }

func (m *TemplateMenu) StartFilter() {
	m.filtering = true
	m.filter = ""
	m.selected = 0
	m.scroll = 0
	m.follow = true
	m.recompute()
}

func (m *TemplateMenu) StopFilter() {
	m.filtering = false
	m.filter = ""
	m.selected = 0
	m.scroll = 0
	m.follow = true
	m.recompute()
}

func (m *TemplateMenu) AppendFilter(s string) {
	m.filter += s
	m.selected = 0
	m.scroll = 0
	m.follow = true
	m.recompute()
}

func (m *TemplateMenu) Backspace() {
	if r := []rune(m.filter); len(r) > 0 {
		m.filter = string(r[:len(r)-1])
		m.selected = 0
		m.scroll = 0
		m.follow = true
		m.recompute()
		return
	}
	m.StopFilter()
}

func (m *TemplateMenu) Update(msg tea.KeyPressMsg) {
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

func (m *TemplateMenu) SelectedEntry() templateMenuEntry {
	if m.selected < 0 || m.selected >= len(m.nav) {
		return templateMenuEntry{}
	}
	return m.rows[m.nav[m.selected]].entry
}

func (m *TemplateMenu) SelectAction(action templateMenuAction) (templateMenuEntry, bool) {
	entry := m.SelectedEntry()
	switch entry.Kind {
	case templateMenuEntryAuthor:
		return entry, action == templateMenuAuthor || action == templateMenuUse
	case templateMenuEntryTemplate:
		return entry, action == templateMenuUse || action == templateMenuEdit || action == templateMenuRemove
	}
	return templateMenuEntry{}, false
}

func (m *TemplateMenu) View(termWidth, termHeight int) string {
	if !m.active {
		return ""
	}
	p := m.palette
	maxBody := max(termHeight-12, 6)
	resultRows := maxBody
	if m.filtering {
		resultRows = max(maxBody-2, 1)
	}
	if m.follow {
		m.ensureSelectedVisible(resultRows)
	}
	start, end := m.viewportRows(resultRows)

	footer := m.footerHints()
	cur := 0
	if len(m.nav) > 0 {
		cur = m.selected + 1
	}
	pos := helpDimStyle.Render(fmt.Sprintf("%d of %d", cur, len(m.nav)))
	textW := m.contentWidth(termWidth)
	rowW := max(textW-helpScrollbarGutter, 1)

	selRow := -1
	if m.selected < len(m.nav) {
		selRow = m.nav[m.selected]
	}
	lines := make([]string, 0, resultRows)
	for i := start; i < end; i++ {
		lines = append(lines, m.renderRow(m.rows[i], i == selRow))
	}
	if m.filtering && len(m.nav) == 0 {
		lines = append(lines, "  "+helpDimStyle.Render("no matches"))
	}
	for len(lines) < resultRows {
		lines = append(lines, " ")
	}
	for i, line := range lines {
		if lipgloss.Width(line) > rowW {
			lines[i] = ansi.Truncate(line, rowW, "…")
		}
	}
	bodyBlock := lipgloss.NewStyle().Width(rowW).Height(resultRows).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	if len(m.rows) > resultRows {
		bar := strings.Join(m.scrollbar(end-start, len(m.rows), start), "\n")
		bodyBlock = lipgloss.JoinHorizontal(lipgloss.Top, bodyBlock, " ", bar)
	} else {
		bodyBlock = lipgloss.JoinHorizontal(lipgloss.Top, bodyBlock, strings.Repeat(" ", helpScrollbarGutter))
	}
	body := bodyBlock
	if m.filtering {
		cursor := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Render("> ")
		query := m.filter
		if query == "" {
			query = lipgloss.NewStyle().Foreground(p.FgMuted).Render("type to filter…")
		} else {
			query = lipgloss.NewStyle().Foreground(p.Highlight).Render(query)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, lipgloss.NewStyle().Width(textW).Render(cursor+query), "", body)
	}
	body = lipgloss.NewStyle().Width(textW).Height(maxBody).Render(body)
	gap := max(textW-lipgloss.Width(footer)-lipgloss.Width(pos), 1)
	title := "Templates"
	if m.context != "" {
		title += " · " + m.context
	}
	return OverlayFrame{
		Title:   title,
		Body:    body,
		Footer:  footer + strings.Repeat(" ", gap) + pos,
		Width:   overlayWidthForBody(textW),
		Palette: p,
	}.Render()
}

func (m *TemplateMenu) footerHints() string {
	if m.filtering {
		return templateMenuFilterHints()
	}
	return templateMenuDefaultHints()
}

func templateMenuDefaultHints() string {
	return RenderInlineHints([]Shortcut{{"↑/↓", "select"}, {"/", "search"}, {"enter/u", "use"}, {"e", "edit"}, {"d", "remove"}, {"a", "new"}, {"esc/q", "close"}})
}

func templateMenuFilterHints() string {
	return RenderInlineHints([]Shortcut{{"type", "filter"}, {"↑/↓", "select"}, {"enter", "use"}, {"esc", "clear"}})
}

func (m *TemplateMenu) contentWidth(termWidth int) int {
	textW := lipgloss.Width(templateMenuDefaultHints()) + 8
	textW = max(textW, lipgloss.Width(templateMenuFilterHints())+8)
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

func (m *TemplateMenu) sizingRows() []templateMenuRow {
	if len(m.templates) == 0 && len(m.rows) == 0 {
		return m.rows
	}
	var rows []templateMenuRow
	rows = append(rows, templateMenuRow{header: true, title: "Template authoring"})
	rows = append(rows, templateMenuRow{entry: newTemplateAuthorEntry()})
	rows = append(rows, templateMenuRow{header: true, title: "Column templates"})
	if entries := m.templateEntries(template.ScopeColumn); len(entries) > 0 {
		for _, entry := range entries {
			rows = append(rows, templateMenuRow{entry: entry})
		}
	} else {
		rows = append(rows, templateMenuRow{entry: templateMenuEntry{Kind: templateMenuEntryEmpty, Label: "No templates", Desc: "Nothing in this scope"}, disabled: true})
	}
	rows = append(rows, templateMenuRow{header: true, title: "Board templates"})
	if entries := m.templateEntries(template.ScopeBoard); len(entries) > 0 {
		for _, entry := range entries {
			rows = append(rows, templateMenuRow{entry: entry})
		}
	} else {
		rows = append(rows, templateMenuRow{entry: templateMenuEntry{Kind: templateMenuEntryEmpty, Label: "No templates", Desc: "Nothing in this scope"}, disabled: true})
	}
	return rows
}

func (m *TemplateMenu) renderRow(row templateMenuRow, selected bool) string {
	p := m.palette
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Primary)
	nameStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	descStyle := lipgloss.NewStyle().Foreground(p.FgSubtle)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	disabledStyle := lipgloss.NewStyle().Foreground(p.FgDim).Italic(true)
	hiStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight)
	hiSelStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Background(p.Primary)
	gutterSel := lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("▌")
	if row.header {
		return headerStyle.Render("── " + row.title + " ──")
	}
	if row.disabled {
		return "  " + disabledStyle.Render(row.entry.Label+"  —  "+row.entry.Desc)
	}
	labelIdx, descIdx := splitLabelDescMatchIndexes(row.entry.Label, row.matchIdx)
	labelStyle, detailStyle := nameStyle, descStyle
	hiLabel, hiDesc := hiStyle, hiStyle
	gutter := " "
	if selected {
		labelStyle, detailStyle = selStyle, selStyle
		hiLabel, hiDesc = hiSelStyle, hiSelStyle
		gutter = gutterSel
	}
	label := renderHighlighted(row.entry.Label, labelIdx, labelStyle, hiLabel)
	if row.entry.Desc != "" {
		sep := "  —  "
		if selected {
			label += selStyle.Render(sep)
		} else {
			label += descStyle.Render(sep)
		}
		label += renderHighlighted(row.entry.Desc, descIdx, detailStyle, hiDesc)
	}
	if selected {
		label = selStyle.Render(" ") + label + selStyle.Render(" ")
	}
	return gutter + " " + label
}

func (m *TemplateMenu) scrollbar(height, total, start int) []string {
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

func (m *TemplateMenu) viewportRows(size int) (int, int) {
	if len(m.rows) <= size {
		m.scroll = 0
		return 0, len(m.rows)
	}
	start := min(max(m.scroll, 0), len(m.rows)-size)
	m.scroll = start
	return start, start + size
}

func (m *TemplateMenu) ensureSelectedVisible(size int) {
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
