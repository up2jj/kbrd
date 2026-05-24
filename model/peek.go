package model

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Peek struct {
	active   bool
	title    string
	lines    []string
	offset   int
	pageSize int
	palette  Palette
}

func (p *Peek) Active() bool { return p.active }

func (p *Peek) Open(title, markdown string, termWidth int) tea.Cmd {
	innerWidth := peekInnerWidth(termWidth)
	rendered := renderMarkdown(markdown, innerWidth)
	rendered = strings.TrimRight(rendered, "\n")
	p.active = true
	p.title = title
	p.lines = strings.Split(rendered, "\n")
	p.offset = 0
	p.pageSize = 0
	return nil
}

func (p *Peek) Close() {
	p.active = false
	p.title = ""
	p.lines = nil
	p.offset = 0
}

func (p *Peek) Update(msg tea.KeyMsg) {
	page := p.pageSize
	if page <= 0 {
		page = 1
	}
	maxOffset := len(p.lines) - page
	if maxOffset < 0 {
		maxOffset = 0
	}
	switch {
	case key.Matches(msg, Keys.PeekClose):
		p.Close()
	case key.Matches(msg, Keys.PeekPageDown):
		next := p.offset + page
		if next >= len(p.lines) {
			p.Close()
			return
		}
		p.offset = next
	case key.Matches(msg, Keys.PeekDown):
		if p.offset < maxOffset {
			p.offset++
		}
	case key.Matches(msg, Keys.PeekUp):
		if p.offset > 0 {
			p.offset--
		}
	case key.Matches(msg, Keys.PeekTop):
		p.offset = 0
	case key.Matches(msg, Keys.PeekBottom):
		p.offset = maxOffset
	}
}

func peekInnerWidth(termWidth int) int {
	w := termWidth - 4
	if w > 120 {
		w = 120
	}
	if w < 20 {
		w = 20
	}
	// account for borders (2) + padding (2*2) = 6
	return w - 6
}

// --- lightweight markdown renderer ---------------------------------------

var (
	mdH1Style       lipgloss.Style
	mdH2Style       lipgloss.Style
	mdH3Style       lipgloss.Style
	mdH4Style       lipgloss.Style
	mdBoldStyle     lipgloss.Style
	mdItalicStyle   lipgloss.Style
	mdStrikeStyle   lipgloss.Style
	mdCodeStyle     lipgloss.Style
	mdCodeBlock     lipgloss.Style
	mdCodeLangStyle lipgloss.Style
	mdQuoteStyle    lipgloss.Style
	mdLinkStyle     lipgloss.Style
	mdRuleStyle     lipgloss.Style
	mdBulletStyle   lipgloss.Style
	mdTaskDone      lipgloss.Style
	mdTaskTodo      lipgloss.Style
	mdTableHeader   lipgloss.Style
	mdTableBorder   lipgloss.Style

	reBold     = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reItalic   = regexp.MustCompile(`(^|[^*])\*([^*\n]+)\*`)
	reCode     = regexp.MustCompile("`([^`]+)`")
	reLink     = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reStrike   = regexp.MustCompile(`~~([^~]+)~~`)
	reAutolink = regexp.MustCompile(`(^|[\s(])(https?://[^\s)]+)`)
	reOrdered  = regexp.MustCompile(`^(\d+)\.\s+(.*)$`)
	reTaskBox  = regexp.MustCompile(`^\[([ xX])\]\s+(.*)$`)
)

func setMarkdownStyles(p Palette) {
	mdH1Style = lipgloss.NewStyle().Bold(true).Foreground(p.Primary)
	mdH2Style = lipgloss.NewStyle().Bold(true).Foreground(p.AccentSoft)
	mdH3Style = lipgloss.NewStyle().Bold(true).Foreground(p.FgSoft)
	mdH4Style = lipgloss.NewStyle().Bold(true).Foreground(p.FgMuted)
	mdBoldStyle = lipgloss.NewStyle().Bold(true).Foreground(p.FgEmphasis)
	mdItalicStyle = lipgloss.NewStyle().Italic(true).Foreground(p.FgSoft)
	mdStrikeStyle = lipgloss.NewStyle().Strikethrough(true).Foreground(p.FgMuted)
	mdCodeStyle = lipgloss.NewStyle().Foreground(p.WarningSoft).Background(p.BgCodeInline)
	mdCodeBlock = lipgloss.NewStyle().Foreground(p.FgCodeBlock).Background(p.BgCodeBlock)
	mdCodeLangStyle = lipgloss.NewStyle().Italic(true).Foreground(p.FgSubtle)
	mdQuoteStyle = lipgloss.NewStyle().Italic(true).Foreground(p.FgMuted)
	mdLinkStyle = lipgloss.NewStyle().Underline(true).Foreground(p.Link)
	mdRuleStyle = lipgloss.NewStyle().Foreground(p.FgDim)
	mdBulletStyle = lipgloss.NewStyle().Foreground(p.Primary)
	mdTaskDone = lipgloss.NewStyle().Foreground(p.Success)
	mdTaskTodo = lipgloss.NewStyle().Foreground(p.FgSubtle)
	mdTableHeader = lipgloss.NewStyle().Bold(true).Foreground(p.FgBase)
	mdTableBorder = lipgloss.NewStyle().Foreground(p.FgDim)
}

func renderMarkdown(src string, width int) string {
	if width < 10 {
		width = 10
	}
	rawLines := strings.Split(src, "\n")
	var out []string
	inCode := false
	i := 0
	for i < len(rawLines) {
		line := rawLines[i]
		trimmed := strings.TrimRight(line, " \t")

		// fenced code blocks (``` or ~~~) with optional language hint
		if t := strings.TrimSpace(trimmed); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			if !inCode {
				lang := strings.TrimSpace(strings.TrimLeft(t, "`~"))
				if lang != "" {
					out = append(out, mdCodeLangStyle.Render(lang))
				}
				inCode = true
			} else {
				inCode = false
			}
			i++
			continue
		}
		if inCode {
			padded := trimmed
			if w := lipgloss.Width(padded); w < width {
				padded = padded + strings.Repeat(" ", width-w)
			}
			out = append(out, mdCodeBlock.Render(padded))
			i++
			continue
		}

		// GFM tables — `| col | col |` followed by alignment row `| --- | --- |`
		if strings.HasPrefix(strings.TrimSpace(trimmed), "|") && i+1 < len(rawLines) && isTableSeparator(rawLines[i+1]) {
			tableLines, consumed := collectTable(rawLines[i:])
			out = append(out, renderTable(tableLines, width)...)
			i += consumed
			continue
		}

		// horizontal rule
		if t := strings.TrimSpace(trimmed); t == "---" || t == "***" || t == "___" {
			out = append(out, mdRuleStyle.Render(strings.Repeat("─", width)))
			i++
			continue
		}

		// headings (#### / ### / ## / #)
		if h, ok := strings.CutPrefix(trimmed, "#### "); ok {
			out = append(out, mdH4Style.Render(h))
			i++
			continue
		}
		if h, ok := strings.CutPrefix(trimmed, "### "); ok {
			out = append(out, mdH3Style.Render(h))
			i++
			continue
		}
		if h, ok := strings.CutPrefix(trimmed, "## "); ok {
			out = append(out, mdH2Style.Render(h))
			i++
			continue
		}
		if h, ok := strings.CutPrefix(trimmed, "# "); ok {
			out = append(out, mdH1Style.Render(h))
			i++
			continue
		}

		// blockquote
		if strings.HasPrefix(trimmed, "> ") {
			body := strings.TrimPrefix(trimmed, "> ")
			styled := mdQuoteStyle.Render("│ " + applyInline(body))
			out = append(out, wrapAnsiLine(styled, width)...)
			i++
			continue
		}

		// list items (bullet, ordered, task)
		t := strings.TrimLeft(trimmed, " ")
		indent := len(trimmed) - len(t)
		if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ ") {
			body := t[2:]
			var prefix string
			// task list: - [ ] body / - [x] body
			if m := reTaskBox.FindStringSubmatch(body); m != nil {
				marker := mdTaskTodo.Render("☐ ")
				if strings.EqualFold(m[1], "x") {
					marker = mdTaskDone.Render("☑ ")
				}
				prefix = strings.Repeat(" ", indent) + marker
				body = m[2]
				if strings.EqualFold(m[1], "x") {
					out = append(out, wrapAnsiLine(prefix+mdStrikeStyle.Render(body), width)...)
					i++
					continue
				}
			} else {
				prefix = strings.Repeat(" ", indent) + mdBulletStyle.Render("• ")
			}
			out = append(out, wrapAnsiLine(prefix+applyInline(body), width)...)
			i++
			continue
		}
		if m := reOrdered.FindStringSubmatch(t); m != nil {
			prefix := strings.Repeat(" ", indent) + mdBulletStyle.Render(m[1]+". ")
			out = append(out, wrapAnsiLine(prefix+applyInline(m[2]), width)...)
			i++
			continue
		}

		// blank line
		if trimmed == "" {
			out = append(out, "")
			i++
			continue
		}

		// default paragraph
		out = append(out, wrapAnsiLine(applyInline(trimmed), width)...)
		i++
	}
	return strings.Join(out, "\n")
}

func isTableSeparator(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "|") {
		return false
	}
	// each cell must be `:?-+:?`
	cells := splitTableRow(t)
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		c = strings.TrimSpace(c)
		c = strings.TrimPrefix(c, ":")
		c = strings.TrimSuffix(c, ":")
		if c == "" || strings.Trim(c, "-") != "" {
			return false
		}
	}
	return true
}

func splitTableRow(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	parts := strings.Split(t, "|")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

func collectTable(lines []string) ([][]string, int) {
	var rows [][]string
	rows = append(rows, splitTableRow(lines[0])) // header
	// skip separator
	consumed := 2
	for ; consumed < len(lines); consumed++ {
		l := strings.TrimSpace(lines[consumed])
		if !strings.HasPrefix(l, "|") {
			break
		}
		rows = append(rows, splitTableRow(lines[consumed]))
	}
	return rows, consumed
}

func renderTable(rows [][]string, width int) []string {
	if len(rows) == 0 {
		return nil
	}
	cols := len(rows[0])
	colW := make([]int, cols)
	for _, r := range rows {
		for i, c := range r {
			if i >= cols {
				break
			}
			if w := lipgloss.Width(applyInline(c)); w > colW[i] {
				colW[i] = w
			}
		}
	}
	// total width = sum + 3 per col (" | ") + 2 outer
	formatRow := func(r []string, header bool) string {
		cells := make([]string, cols)
		for i := 0; i < cols; i++ {
			val := ""
			if i < len(r) {
				val = applyInline(r[i])
			}
			if header {
				val = mdTableHeader.Render(val)
			}
			pad := colW[i] - lipgloss.Width(val)
			if pad < 0 {
				pad = 0
			}
			cells[i] = val + strings.Repeat(" ", pad)
		}
		sep := mdTableBorder.Render(" │ ")
		return strings.Join(cells, sep)
	}
	separator := func() string {
		segs := make([]string, cols)
		for i := 0; i < cols; i++ {
			segs[i] = strings.Repeat("─", colW[i])
		}
		return mdTableBorder.Render(strings.Join(segs, "─┼─"))
	}
	var out []string
	out = append(out, formatRow(rows[0], true))
	out = append(out, separator())
	for _, r := range rows[1:] {
		out = append(out, formatRow(r, false))
	}
	return out
}

func applyInline(s string) string {
	s = reCode.ReplaceAllStringFunc(s, func(m string) string {
		inner := m[1 : len(m)-1]
		return mdCodeStyle.Render(inner)
	})
	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		sub := reLink.FindStringSubmatch(m)
		return mdLinkStyle.Render(sub[1])
	})
	s = reAutolink.ReplaceAllStringFunc(s, func(m string) string {
		sub := reAutolink.FindStringSubmatch(m)
		return sub[1] + mdLinkStyle.Render(sub[2])
	})
	s = reStrike.ReplaceAllStringFunc(s, func(m string) string {
		return mdStrikeStyle.Render(m[2 : len(m)-2])
	})
	s = reBold.ReplaceAllStringFunc(s, func(m string) string {
		inner := m[2 : len(m)-2]
		return mdBoldStyle.Render(inner)
	})
	s = reItalic.ReplaceAllStringFunc(s, func(m string) string {
		sub := reItalic.FindStringSubmatch(m)
		return sub[1] + mdItalicStyle.Render(sub[2])
	})
	return s
}

// wrapAnsiLine wraps a line that may contain ANSI escapes to width, splitting
// at spaces when possible. Uses lipgloss.Width to ignore escape sequences.
func wrapAnsiLine(line string, width int) []string {
	if lipgloss.Width(line) <= width {
		return []string{line}
	}
	// fall back to lipgloss's word-wrap-aware Width by chunking on visible runes
	var out []string
	current := ""
	curW := 0
	flush := func() {
		if current != "" {
			out = append(out, current)
		}
		current = ""
		curW = 0
	}
	words := strings.Split(line, " ")
	for i, w := range words {
		if i > 0 {
			if curW+1+lipgloss.Width(w) > width {
				flush()
			} else {
				current += " "
				curW++
			}
		}
		ww := lipgloss.Width(w)
		if ww > width {
			// hard split overlong token
			if current != "" {
				flush()
			}
			out = append(out, w)
			continue
		}
		current += w
		curW += ww
	}
	flush()
	return out
}

func (p *Peek) View(termWidth, termHeight int) string {
	if !p.active {
		return ""
	}

	outerWidth := termWidth - 4
	if outerWidth > 120 {
		outerWidth = 120
	}
	if outerWidth < 24 {
		outerWidth = 24
	}
	outerHeight := termHeight - 4
	if outerHeight < 8 {
		outerHeight = 8
	}

	innerWidth := outerWidth - 6
	pageSize := outerHeight - 2 /*borders*/ - 2 /*padding*/ - 4 /*title+blanks+footer*/
	if pageSize < 1 {
		pageSize = 1
	}
	p.pageSize = pageSize

	if p.offset >= len(p.lines) {
		p.offset = 0
	}
	end := p.offset + pageSize
	if end > len(p.lines) {
		end = len(p.lines)
	}
	visible := p.lines[p.offset:end]

	for len(visible) < pageSize {
		visible = append(visible, "")
	}

	totalPages := (len(p.lines) + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	currentPage := p.offset/pageSize + 1
	if currentPage > totalPages {
		currentPage = totalPages
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(p.palette.Primary)
	body := strings.Join(visible, "\n")
	bodyStyle := lipgloss.NewStyle().Width(innerWidth)

	hints := []Shortcut{
		{"j/k", "scroll"},
		{"g/G", "top/bot"},
		{"enter", "page"},
		{"q/esc", "close"},
	}
	footerLeft := RenderInlineHints(hints)
	footerRight := helpDimStyle.Render(fmt.Sprintf("%d/%d", currentPage, totalPages))
	gap := innerWidth - lipgloss.Width(footerLeft) - lipgloss.Width(footerRight)
	if gap < 1 {
		gap = 1
	}
	footer := footerLeft + strings.Repeat(" ", gap) + footerRight

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(p.title),
		"",
		bodyStyle.Render(body),
		"",
		footer,
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.palette.BorderActive).
		Padding(1, 2).
		Width(outerWidth - 2).
		Render(content)
}
