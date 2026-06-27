package model

import (
	"fmt"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"kbrd/theme"
)

type Peek struct {
	active      bool
	title       string
	rawMarkdown string
	lines       []string
	sourceLines []int
	sourceFirst []bool
	markers     map[int]PeekLineMarkerKind
	offset      int
	pageSize    int
	palette     Palette
}

func (p *Peek) Active() bool { return p.active }

type PeekLineMarkerKind int

const (
	PeekLineAdded PeekLineMarkerKind = iota
	PeekLineModified
	PeekLineDeleted
)

type PeekLineMarker struct {
	Line int
	Kind PeekLineMarkerKind
}

const (
	peekTermMargin = 4
	peekMaxWidth   = 120
	peekMinWidth   = 24
	peekMinHeight  = 8

	// One separator row plus one footer row when OverlayFrame joins Body/Footer.
	peekFooterRows = 2
)

// peekScrollbarGutter reserves one gap plus one scrollbar column. Reserved
// unconditionally so the body wraps to the same width whether or not the bar is
// showing, keeping the modal height fixed.
const peekScrollbarGutter = 2
const peekLineMarkerGutter = 2

func (p *Peek) Open(title, markdown string, termWidth int) tea.Cmd {
	p.active = true
	p.title = title
	p.rawMarkdown = markdown
	p.markers = nil
	p.renderMarkdown(termWidth)
	p.offset = 0
	p.pageSize = 0
	return nil
}

func (p *Peek) SetLineMarkers(markers []PeekLineMarker, termWidth int) {
	if len(markers) == 0 {
		p.markers = nil
	} else {
		p.markers = make(map[int]PeekLineMarkerKind, len(markers))
		for _, marker := range markers {
			if marker.Line > 0 {
				p.markers[marker.Line] = marker.Kind
			}
		}
	}
	if p.active {
		p.renderMarkdown(termWidth)
	}
}

func (p *Peek) renderMarkdown(termWidth int) {
	rows := renderMarkdownRows(p.rawMarkdown, peekContentWidth(termWidth, p.hasLineMarkers()))
	if len(rows) == 0 {
		rows = []peekMarkdownRow{{Text: "", SourceLine: 1, FirstForSource: true}}
	}
	p.lines = make([]string, len(rows))
	p.sourceLines = make([]int, len(rows))
	p.sourceFirst = make([]bool, len(rows))
	for i, row := range rows {
		p.lines[i] = row.Text
		p.sourceLines[i] = row.SourceLine
		p.sourceFirst[i] = row.FirstForSource
	}
}

func (p *Peek) hasLineMarkers() bool { return len(p.markers) > 0 }

func (p *Peek) Close() {
	p.active = false
	p.title = ""
	p.rawMarkdown = ""
	p.lines = nil
	p.sourceLines = nil
	p.sourceFirst = nil
	p.markers = nil
	p.offset = 0
}

func (p *Peek) Update(msg tea.KeyPressMsg) {
	page := p.pageSize
	if page <= 0 {
		page = 1
	}
	maxOffset := max(len(p.lines)-page, 0)
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

func (p *Peek) ScrollBy(delta int) {
	page := p.pageSize
	if page <= 0 {
		page = 1
	}
	maxOffset := max(len(p.lines)-page, 0)
	p.offset += delta
	if p.offset < 0 {
		p.offset = 0
	}
	if p.offset > maxOffset {
		p.offset = maxOffset
	}
}

// scrollbar returns height rows for a vertical scrollbar, sized and positioned
// from the current offset. Caller guarantees total > height (content overflows).
func (p *Peek) scrollbar(height, total int) []string {
	thumb := max(height*height/total, 1)
	maxOffset := total - height
	pos := min(max((height-thumb)*p.offset/maxOffset, 0), height-thumb)
	// Match the column scrollbar: a thin │ track with a ┃ thumb.
	track := lipgloss.NewStyle().Foreground(p.palette.FgDim).Render("│")
	bar := lipgloss.NewStyle().Foreground(p.palette.FgMuted).Render("┃")
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

func normalizePeekRows(lines []string, width int) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		if lipgloss.Width(line) > width {
			line = ansi.Truncate(line, width, "…")
		}
		if pad := width - lipgloss.Width(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		out[i] = line
	}
	return out
}

// HandleMouse scrolls the body on wheel events; other mouse input is ignored.
func (p *Peek) HandleMouse(msg tea.MouseMsg) {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		p.ScrollBy(-3)
	case tea.MouseWheelDown:
		p.ScrollBy(3)
	}
}

func peekInnerWidth(termWidth int) int {
	return overlayBodyWidth(peekFrameWidth(termWidth))
}

func peekOuterWidth(termWidth int) int {
	return max(min(termWidth-peekTermMargin, peekMaxWidth), peekMinWidth)
}

func peekOuterHeight(termHeight int) int {
	return max(termHeight-peekTermMargin, peekMinHeight)
}

func peekFrameWidth(termWidth int) int {
	return theme.RoundedFrameContentWidth(peekOuterWidth(termWidth), 0)
}

func peekBodyWidth(termWidth int) int {
	return max(peekInnerWidth(termWidth)-peekScrollbarGutter, 1)
}

func peekContentWidth(termWidth int, withLineMarkers bool) int {
	width := peekBodyWidth(termWidth)
	if withLineMarkers {
		width -= peekLineMarkerGutter
	}
	return max(width, 1)
}

func peekPageSize(termHeight int) int {
	border := lipgloss.RoundedBorder()
	frameRows := lipgloss.Height(border.Top+"\n"+border.Bottom) + 2*overlayPadV
	return max(peekOuterHeight(termHeight)-frameRows-peekFooterRows, 1)
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

type peekMarkdownRow struct {
	Text           string
	SourceLine     int
	FirstForSource bool
}

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
	rows := renderMarkdownRows(src, width)
	lines := make([]string, len(rows))
	for i, row := range rows {
		lines[i] = row.Text
	}
	return strings.Join(lines, "\n")
}

func renderMarkdownRows(src string, width int) []peekMarkdownRow {
	if width < 10 {
		width = 10
	}
	rawLines := strings.Split(src, "\n")
	var rows []peekMarkdownRow
	appendRows := func(rendered []string, sourceLine int) {
		for i, row := range rendered {
			rows = append(rows, peekMarkdownRow{
				Text:           row,
				SourceLine:     sourceLine,
				FirstForSource: i == 0,
			})
		}
	}
	appendRow := func(rendered string, sourceLine int) {
		appendRows([]string{rendered}, sourceLine)
	}
	inCode := false
	i := 0
	for i < len(rawLines) {
		line := rawLines[i]
		sourceLine := i + 1
		trimmed := strings.TrimRight(line, " \t")

		// fenced code blocks (``` or ~~~) with optional language hint
		if t := strings.TrimSpace(trimmed); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			if !inCode {
				lang := strings.TrimSpace(strings.TrimLeft(t, "`~"))
				if lang != "" {
					appendRow(mdCodeLangStyle.Render(lang), sourceLine)
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
			appendRow(mdCodeBlock.Render(padded), sourceLine)
			i++
			continue
		}

		// GFM tables — `| col | col |` followed by alignment row `| --- | --- |`
		if strings.HasPrefix(strings.TrimSpace(trimmed), "|") && i+1 < len(rawLines) && isTableSeparator(rawLines[i+1]) {
			tableLines, consumed := collectTable(rawLines[i:])
			appendRows(renderTable(tableLines, width), sourceLine)
			i += consumed
			continue
		}

		// horizontal rule
		if t := strings.TrimSpace(trimmed); t == "---" || t == "***" || t == "___" {
			appendRow(mdRuleStyle.Render(strings.Repeat("─", width)), sourceLine)
			i++
			continue
		}

		// headings (#### / ### / ## / #)
		if h, ok := strings.CutPrefix(trimmed, "#### "); ok {
			appendRow(mdH4Style.Render(h), sourceLine)
			i++
			continue
		}
		if h, ok := strings.CutPrefix(trimmed, "### "); ok {
			appendRow(mdH3Style.Render(h), sourceLine)
			i++
			continue
		}
		if h, ok := strings.CutPrefix(trimmed, "## "); ok {
			appendRow(mdH2Style.Render(h), sourceLine)
			i++
			continue
		}
		if h, ok := strings.CutPrefix(trimmed, "# "); ok {
			appendRow(mdH1Style.Render(h), sourceLine)
			i++
			continue
		}

		// blockquote
		if after, ok := strings.CutPrefix(trimmed, "> "); ok {
			body := after
			styled := mdQuoteStyle.Render("│ " + applyInline(body))
			appendRows(wrapAnsiLine(styled, width), sourceLine)
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
					appendRows(wrapAnsiLine(prefix+mdStrikeStyle.Render(body), width), sourceLine)
					i++
					continue
				}
			} else {
				prefix = strings.Repeat(" ", indent) + mdBulletStyle.Render("• ")
			}
			appendRows(wrapAnsiLine(prefix+applyInline(body), width), sourceLine)
			i++
			continue
		}
		if m := reOrdered.FindStringSubmatch(t); m != nil {
			prefix := strings.Repeat(" ", indent) + mdBulletStyle.Render(m[1]+". ")
			appendRows(wrapAnsiLine(prefix+applyInline(m[2]), width), sourceLine)
			i++
			continue
		}

		// blank line
		if trimmed == "" {
			appendRow("", sourceLine)
			i++
			continue
		}

		// default paragraph
		appendRows(wrapAnsiLine(applyInline(trimmed), width), sourceLine)
		i++
	}
	for len(rows) > 1 && rows[len(rows)-1].Text == "" {
		rows = rows[:len(rows)-1]
	}
	if len(rows) == 0 {
		rows = []peekMarkdownRow{{Text: "", SourceLine: 1, FirstForSource: true}}
	}
	return rows
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
		for i := range cols {
			val := ""
			if i < len(r) {
				val = applyInline(r[i])
			}
			if header {
				val = mdTableHeader.Render(val)
			}
			pad := max(colW[i]-lipgloss.Width(val), 0)
			cells[i] = val + strings.Repeat(" ", pad)
		}
		sep := mdTableBorder.Render(" │ ")
		return strings.Join(cells, sep)
	}
	separator := func() string {
		segs := make([]string, cols)
		for i := range cols {
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

	pageSize := peekPageSize(termHeight)
	p.pageSize = pageSize

	maxOffset := max(len(p.lines)-pageSize, 0)
	if p.offset < 0 {
		p.offset = 0
	}
	if p.offset > maxOffset {
		p.offset = maxOffset
	}
	end := min(p.offset+pageSize, len(p.lines))
	visible := p.lines[p.offset:end]
	visibleSources := p.sourceLines[p.offset:end]
	visibleFirst := p.sourceFirst[p.offset:end]

	bodyWidth := peekContentWidth(termWidth, p.hasLineMarkers())
	blankRow := strings.Repeat(" ", max(bodyWidth, 1))
	for len(visible) < pageSize {
		visible = append(visible, blankRow)
		visibleSources = append(visibleSources, 0)
		visibleFirst = append(visibleFirst, false)
	}
	visible = normalizePeekRows(visible, bodyWidth)

	totalPages := max((len(p.lines)+pageSize-1)/pageSize, 1)
	currentPage := min(p.offset/pageSize+1, totalPages)

	// The scrollbar gutter is always reserved (see peekScrollbarGutter), so the
	// body wraps to a constant width and the modal height never changes; the bar
	// itself only appears once content overflows a page.
	bodyBlock := strings.Join(p.renderMarkedRows(visible, visibleSources, visibleFirst), "\n")
	blankGutter := make([]string, pageSize)
	for i := range blankGutter {
		blankGutter[i] = " "
	}
	bar := strings.Join(blankGutter, "\n")
	if len(p.lines) > pageSize {
		bar = strings.Join(p.scrollbar(pageSize, len(p.lines)), "\n")
	}
	bodyBlock = lipgloss.JoinHorizontal(lipgloss.Top, bodyBlock, " ", bar)

	hints := []Shortcut{
		{"j/k", "scroll"},
		{"g/G", "top/bot"},
		{"enter", "page"},
		{"e", "edit"},
		{"a/p", "append/prepend"},
		{"b", "journal"},
		{"q/esc", "close"},
	}
	footerLeft := RenderInlineHints(hints)
	footerRight := helpDimStyle.Render(fmt.Sprintf("%d/%d", currentPage, totalPages))
	gap := max(peekInnerWidth(termWidth)-lipgloss.Width(footerLeft)-lipgloss.Width(footerRight), 1)
	footer := footerLeft + strings.Repeat(" ", gap) + footerRight

	return OverlayFrame{
		Title:   p.title,
		Body:    bodyBlock,
		Footer:  footer,
		Width:   peekFrameWidth(termWidth),
		Palette: p.palette,
	}.Render()
}

func (p *Peek) renderMarkedRows(lines []string, sources []int, first []bool) []string {
	if !p.hasLineMarkers() {
		return lines
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		marker := " "
		if i < len(sources) && i < len(first) && first[i] {
			if kind, ok := p.markers[sources[i]]; ok {
				marker = p.renderLineMarker(kind)
			}
		}
		if lipgloss.Width(marker) < 1 {
			marker = " "
		}
		out[i] = marker + " " + line
	}
	return out
}

func (p *Peek) renderLineMarker(kind PeekLineMarkerKind) string {
	switch kind {
	case PeekLineAdded:
		return lipgloss.NewStyle().Foreground(p.palette.Success).Bold(true).Render("+")
	case PeekLineModified:
		return lipgloss.NewStyle().Foreground(p.palette.Warning).Bold(true).Render("~")
	case PeekLineDeleted:
		return lipgloss.NewStyle().Foreground(p.palette.Danger).Bold(true).Render("-")
	default:
		return " "
	}
}
