package vimbuf

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"kbrd/theme"
)

// vrow is one visual row: a wrapped segment [start,end) of a logical line, with
// first marking the segment that carries the line number in the gutter.
type vrow struct {
	row, start, end int
	first           bool
}

// View renders the visible viewport. Long lines soft-wrap to multiple visual
// rows; the gutter shows the line number on a line's first row only, and a
// vertical scrollbar on the right edge reflects the position in visual rows.
func (b *Buffer) View(p theme.Palette) string {
	gutterStyle := lipgloss.NewStyle().Foreground(p.FgDim)
	curGutterStyle := lipgloss.NewStyle().Foreground(p.Primary)
	selStyle := lipgloss.NewStyle().Background(p.BgSelectedDetail)
	cursorStyle := lipgloss.NewStyle().Background(p.Highlight).Foreground(p.FgInverse)
	if b.mode == ModeInsert {
		cursorStyle = lipgloss.NewStyle().Underline(true).Foreground(p.FgBase)
	}
	thumbStyle := lipgloss.NewStyle().Foreground(p.Primary)
	trackStyle := lipgloss.NewStyle().Foreground(p.FgDim)

	height := b.height
	if height <= 0 {
		height = len(b.lines)
	}
	width := b.width
	if width <= 0 {
		width = 80
	}
	gutterW := b.gutterWidth()
	textW := b.textWidth()

	// Lay out the visible visual rows by wrapping logical lines from top.
	rows := make([]vrow, 0, height)
	for r := b.top; r < len(b.lines) && len(rows) < height; r++ {
		n := len(b.lines[r])
		if n == 0 {
			rows = append(rows, vrow{r, 0, 0, true})
			continue
		}
		for s := 0; s < n && len(rows) < height; s += textW {
			rows = append(rows, vrow{r, s, min(s+textW, n), s == 0})
		}
		// A full-width final segment needs an extra row for an EOL cursor.
		if len(rows) < height && r == b.cursor.Row && b.cursor.Col == n && n%textW == 0 {
			rows = append(rows, vrow{r, n, n, false})
		}
	}

	// Scrollbar thumb in visual-row units.
	totalVis := b.totalVisualRows()
	showBar := totalVis > height
	thumbStart, thumbEnd := b.visualThumb(height, totalVis)

	var sb strings.Builder
	for i := 0; i < height; i++ {
		gut := strings.Repeat(" ", gutterW)
		body := ""
		if i < len(rows) {
			v := rows[i]
			if v.first {
				num := strconv.Itoa(v.row + 1)
				gs := gutterStyle
				if v.row == b.cursor.Row {
					gs = curGutterStyle
				}
				gut = gs.Render(strings.Repeat(" ", max(gutterW-1-len(num), 0)) + num + " ")
			} else {
				gut = gutterStyle.Render(gut)
			}
			body = b.renderSegment(v, selStyle, cursorStyle)
		} else {
			gut = gutterStyle.Render(gut)
		}
		sb.WriteString(gut)
		sb.WriteString(body)
		// pad to the scrollbar lane, then the bar cell
		if gap := width - 1 - gutterW - lipgloss.Width(body); gap > 0 {
			sb.WriteString(strings.Repeat(" ", gap))
		}
		switch {
		case !showBar:
			sb.WriteByte(' ')
		case i >= thumbStart && i < thumbEnd:
			sb.WriteString(thumbStyle.Render("█"))
		default:
			sb.WriteString(trackStyle.Render("│"))
		}
		if i < height-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func (b *Buffer) totalVisualRows() int {
	total := 0
	for r := range b.lines {
		total += b.lineRows(r)
	}
	return total
}

// visualThumb returns the [start,end) thumb range over total visual rows, using
// the visual offset of the top logical line.
func (b *Buffer) visualThumb(height, total int) (int, int) {
	if total <= height || height <= 0 {
		return 0, height
	}
	offset := 0
	for r := 0; r < b.top && r < len(b.lines); r++ {
		offset += b.lineRows(r)
	}
	thumb := max(height*height/total, 1)
	maxOff := total - height
	start := 0
	if maxOff > 0 {
		start = offset * (height - thumb) / maxOff
	}
	start = min(max(start, 0), height-thumb)
	return start, start + thumb
}

// renderSegment renders one wrapped segment [v.start,v.end) of a line with the
// cursor and selection styling, including an EOL cursor cell when the cursor
// sits one past the last rune on this segment.
func (b *Buffer) renderSegment(v vrow, selStyle, cursorStyle lipgloss.Style) string {
	line := b.lines[v.row]
	n := len(line)
	selStart, selEnd, selActive := b.selectionForRow(v.row)
	cursorActive := b.mode != ModeCommand && v.row == b.cursor.Row

	var sb strings.Builder
	for col := v.start; col < v.end; col++ {
		ch := string(line[col])
		switch {
		case cursorActive && col == b.cursor.Col:
			sb.WriteString(cursorStyle.Render(ch))
		case selActive && col >= selStart && col <= selEnd:
			sb.WriteString(selStyle.Render(ch))
		default:
			sb.WriteString(ch)
		}
	}
	// EOL cursor: a cell one past the last rune, on this line's final segment.
	// Only when the segment has room — a full-width final segment puts the EOL
	// cursor on an extra wrapped row instead (see View).
	if cursorActive && b.cursor.Col == v.end && v.end == n && v.end-v.start < b.textWidth() {
		sb.WriteString(cursorStyle.Render(" "))
	}
	return sb.String()
}

// selectionForRow returns the inclusive column span selected on a row in visual
// mode, plus whether any selection is active for that row.
func (b *Buffer) selectionForRow(row int) (start, end int, active bool) {
	// Keep the linewise selection visible while typing a range ":" command so the
	// user can see what `:'<,'>lua` will operate on.
	if b.mode == ModeCommand && b.visualRange != nil {
		if row >= b.visualRange.Start && row <= b.visualRange.End {
			return 0, len(b.lineAt(row)), true
		}
		return 0, 0, false
	}
	if b.mode != ModeVisual && b.mode != ModeVisualLine {
		return 0, 0, false
	}
	f, t := orderPos(b.anchor, b.cursor)
	if row < f.Row || row > t.Row {
		return 0, 0, false
	}
	lineLen := len(b.lineAt(row))
	if b.mode == ModeVisualLine {
		return 0, lineLen, true
	}
	switch {
	case f.Row == t.Row:
		return f.Col, t.Col, true
	case row == f.Row:
		return f.Col, lineLen, true
	case row == t.Row:
		return 0, t.Col, true
	default:
		return 0, lineLen, true
	}
}

// ModeName returns the human-readable mode label for the status line.
func (b *Buffer) ModeName() string {
	switch b.mode {
	case ModeInsert:
		return "INSERT"
	case ModeVisual:
		return "VISUAL"
	case ModeVisualLine:
		return "V-LINE"
	case ModeCommand:
		return "COMMAND"
	default:
		return "NORMAL"
	}
}
