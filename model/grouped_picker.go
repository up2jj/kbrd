package model

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// groupedPicker is the common state for menus that show groups normally and a
// flat fuzzy-ranked result list while filtering. Hosts own their entries and
// actions; this type owns only the interaction mechanics shared by Help,
// Templates, and Create item.
type groupedPicker struct {
	groupedMenuNav
	filtering bool
	filter    string
}

func (p *groupedPicker) Reset() {
	p.filtering = false
	p.filter = ""
	p.groupedMenuNav.Reset()
}

func (p *groupedPicker) StartFilter() {
	p.filtering = true
	p.filter = ""
	p.ResetSelection()
}

func (p *groupedPicker) StopFilter() {
	p.filtering = false
	p.filter = ""
	p.ResetSelection()
}

func (p *groupedPicker) AppendFilter(s string) {
	if s == "" {
		return
	}
	p.filter += s
	p.ResetSelection()
}

// Backspace removes a query rune. It reports false when there was no query,
// allowing the host to leave filter mode when that is its desired UX.
func (p *groupedPicker) Backspace() bool {
	r := []rune(p.filter)
	if len(r) == 0 {
		return false
	}
	p.filter = string(r[:len(r)-1])
	p.ResetSelection()
	return true
}

type groupedPickerBody struct {
	Palette    Palette
	Rows       int
	TermHeight int
	TextWidth  int
	Filtering  bool
	Filter     string
	Nav        *groupedMenuNav
	RenderRow  func(row int, selected bool) string
}

// renderGroupedPickerBody renders the shared fixed-height result pane. It
// deliberately returns the position label separately so callers can compose
// different footers and auxiliary panes without duplicating viewport math.
func renderGroupedPickerBody(s groupedPickerBody) (body, position string) {
	maxBody := max(s.TermHeight-12, 6)
	resultRows := maxBody
	if s.Filtering {
		resultRows = max(maxBody-2, 1)
	}
	if s.Nav.follow {
		s.Nav.EnsureSelectedVisible(resultRows)
	}
	start, end := s.Nav.Viewport(s.Rows, resultRows)

	rowW := max(s.TextWidth-helpScrollbarGutter, 1)
	selRow := -1
	if row, ok := s.Nav.SelectedRow(); ok {
		selRow = row
	}
	lines := make([]string, 0, resultRows)
	for i := start; i < end; i++ {
		lines = append(lines, s.RenderRow(i, i == selRow))
	}
	if s.Filtering && len(s.Nav.nav) == 0 {
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
	if s.Rows > resultRows {
		track := lipgloss.NewStyle().Foreground(s.Palette.FgDim).Render("│")
		thumb := lipgloss.NewStyle().Foreground(s.Palette.FgMuted).Render("┃")
		bar := strings.Join(groupedMenuScrollbar(end-start, s.Rows, start, track, thumb), "\n")
		bodyBlock = lipgloss.JoinHorizontal(lipgloss.Top, bodyBlock, " ", bar)
	} else {
		bodyBlock = lipgloss.JoinHorizontal(lipgloss.Top, bodyBlock, strings.Repeat(" ", helpScrollbarGutter))
	}

	body = bodyBlock
	if s.Filtering {
		cursor := lipgloss.NewStyle().Bold(true).Foreground(s.Palette.Highlight).Render("> ")
		query := s.Filter
		if query == "" {
			query = lipgloss.NewStyle().Foreground(s.Palette.FgMuted).Render("type to filter…")
		} else {
			query = lipgloss.NewStyle().Foreground(s.Palette.Highlight).Render(query)
		}
		prompt := ansi.Truncate(cursor+query, s.TextWidth, "…")
		body = lipgloss.JoinVertical(lipgloss.Left, lipgloss.NewStyle().Width(s.TextWidth).Render(prompt), "", body)
	}
	body = lipgloss.NewStyle().Width(s.TextWidth).Height(maxBody).Render(body)
	cur, total := s.Nav.Position()
	return body, helpDimStyle.Render(fmt.Sprintf("%d of %d", cur, total))
}
