package vimbuf

import "strings"

func posLess(a, p Pos) bool {
	if a.Row != p.Row {
		return a.Row < p.Row
	}
	return a.Col < p.Col
}

func orderPos(a, p Pos) (Pos, Pos) {
	if posLess(p, a) {
		return p, a
	}
	return a, p
}

func rowSpan(a, p Pos) (int, int) {
	if p.Row < a.Row {
		return p.Row, a.Row
	}
	return a.Row, p.Row
}

// deleteRange removes runes in [from, to) (exclusive end) joining lines as
// needed, and returns the removed text.
func (b *Buffer) deleteRange(from, to Pos) string {
	from, to = orderPos(from, to)
	if from.Row == to.Row {
		line := b.lines[from.Row]
		if from.Col < 0 {
			from.Col = 0
		}
		if to.Col > len(line) {
			to.Col = len(line)
		}
		if from.Col > len(line) {
			from.Col = len(line)
		}
		del := string(line[from.Col:to.Col])
		b.lines[from.Row] = append(append([]rune{}, line[:from.Col]...), line[to.Col:]...)
		return del
	}
	first := b.lines[from.Row]
	last := b.lines[to.Row]
	if from.Col > len(first) {
		from.Col = len(first)
	}
	if to.Col > len(last) {
		to.Col = len(last)
	}
	var sb strings.Builder
	sb.WriteString(string(first[from.Col:]))
	sb.WriteString("\n")
	for r := from.Row + 1; r < to.Row; r++ {
		sb.WriteString(string(b.lines[r]))
		sb.WriteString("\n")
	}
	sb.WriteString(string(last[:to.Col]))
	merged := append(append([]rune{}, first[:from.Col]...), last[to.Col:]...)
	newLines := append([][]rune{}, b.lines[:from.Row]...)
	newLines = append(newLines, merged)
	newLines = append(newLines, b.lines[to.Row+1:]...)
	b.lines = newLines
	return sb.String()
}

func (b *Buffer) copyRange(from, to Pos) string {
	from, to = orderPos(from, to)
	if from.Row == to.Row {
		line := b.lines[from.Row]
		if to.Col > len(line) {
			to.Col = len(line)
		}
		if from.Col > len(line) {
			from.Col = len(line)
		}
		return string(line[from.Col:to.Col])
	}
	first := b.lines[from.Row]
	last := b.lines[to.Row]
	if to.Col > len(last) {
		to.Col = len(last)
	}
	var sb strings.Builder
	sb.WriteString(string(first[from.Col:]))
	sb.WriteString("\n")
	for r := from.Row + 1; r < to.Row; r++ {
		sb.WriteString(string(b.lines[r]))
		sb.WriteString("\n")
	}
	sb.WriteString(string(last[:to.Col]))
	return sb.String()
}

// deleteLines removes whole lines [r1,r2] (inclusive) and returns the removed
// text with a trailing newline (linewise register convention).
func (b *Buffer) deleteLines(r1, r2 int) string {
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	if r1 < 0 {
		r1 = 0
	}
	if r2 >= len(b.lines) {
		r2 = len(b.lines) - 1
	}
	text := b.copyLines(r1, r2)
	newLines := append([][]rune{}, b.lines[:r1]...)
	newLines = append(newLines, b.lines[r2+1:]...)
	b.lines = newLines
	if len(b.lines) == 0 {
		b.lines = [][]rune{{}}
	}
	return text
}

func (b *Buffer) copyLines(r1, r2 int) string {
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	if r1 < 0 {
		r1 = 0
	}
	if r2 >= len(b.lines) {
		r2 = len(b.lines) - 1
	}
	parts := make([]string, 0, r2-r1+1)
	for r := r1; r <= r2; r++ {
		parts = append(parts, string(b.lines[r]))
	}
	return strings.Join(parts, "\n") + "\n"
}

func (b *Buffer) indentLines(r1, r2 int, indent bool) {
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	pad := []rune(strings.Repeat(" ", b.indentWidth))
	for r := r1; r <= r2 && r < len(b.lines); r++ {
		if indent {
			if len(b.lines[r]) == 0 {
				continue
			}
			b.lines[r] = append(append([]rune{}, pad...), b.lines[r]...)
		} else {
			line := b.lines[r]
			n := 0
			for n < len(line) && n < b.indentWidth && line[n] == ' ' {
				n++
			}
			b.lines[r] = append([]rune{}, line[n:]...)
		}
	}
}

// caseRange applies a case operator over a span. op is 'u' (lower), 'U' (upper),
// or '~' (toggle).
func (b *Buffer) caseRange(op rune, from, to Pos, kind motionKind) {
	if kind == mLinewise {
		r1, r2 := rowSpan(from, to)
		for r := r1; r <= r2 && r < len(b.lines); r++ {
			b.lines[r] = mapCase(op, b.lines[r])
		}
		b.cursor = Pos{r1, 0}
		return
	}
	f, t := orderPos(from, to)
	if kind == mInclusive {
		t.Col++
	}
	if f.Row == t.Row {
		b.caseLineSpan(op, f.Row, f.Col, t.Col)
	} else {
		// Multi-line charwise span: first row from f.Col to EOL, whole middle
		// rows, last row up to t.Col. (Previously this branch was missing, so a
		// charwise u/U/~ across a line boundary silently did nothing.)
		b.caseLineSpan(op, f.Row, f.Col, len(b.lineAt(f.Row)))
		for r := f.Row + 1; r < t.Row; r++ {
			b.caseLineSpan(op, r, 0, len(b.lineAt(r)))
		}
		b.caseLineSpan(op, t.Row, 0, t.Col)
	}
	b.cursor = f
}

// caseLineSpan applies a case op to rune columns [from,to) of one line, clamping
// the bounds to the line. Out-of-range rows are ignored.
func (b *Buffer) caseLineSpan(op rune, row, from, to int) {
	if row < 0 || row >= len(b.lines) {
		return
	}
	line := b.lines[row]
	if from < 0 {
		from = 0
	}
	if to > len(line) {
		to = len(line)
	}
	for i := from; i < to; i++ {
		line[i] = caseRune(op, line[i])
	}
}

func mapCase(op rune, line []rune) []rune {
	out := make([]rune, len(line))
	for i, r := range line {
		out[i] = caseRune(op, r)
	}
	return out
}

func caseRune(op, r rune) rune {
	switch op {
	case 'u':
		return toLower(r)
	case 'U':
		return toUpper(r)
	default: // '~'
		if l := toLower(r); l != r {
			return l
		}
		return toUpper(r)
	}
}

func toLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

func toUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}

// applyOp runs operator op over the motion span from the cursor to target.
// It assumes an undo group is already open (caller invoked b.begin()).
func (b *Buffer) applyOp(op rune, target Pos, kind motionKind) {
	switch op {
	case '>', '<':
		r1, r2 := rowSpan(b.cursor, target)
		b.indentLines(r1, r2, op == '>')
		b.cursor = Pos{r1, firstNonBlank(b.lineAt(r1))}
		b.recMutated = true
	case 'u', 'U', '~':
		b.caseRange(op, b.cursor, target, kind)
		b.recMutated = true
	case 'd', 'c', 'y':
		if kind == mLinewise {
			b.applyLineOp(op, target)
		} else {
			b.applyCharOp(op, target, kind)
		}
	}
}

func (b *Buffer) applyLineOp(op rune, target Pos) {
	r1, r2 := rowSpan(b.cursor, target)
	switch op {
	case 'y':
		b.reg = b.copyLines(r1, r2)
		b.regLinewise = true
		b.justYanked = b.reg
		b.cursor.Row = r1
		b.clampCursor()
	case 'd':
		b.reg = b.deleteLines(r1, r2)
		b.regLinewise = true
		b.cursor.Row = r1
		b.clampCursor()
		b.cursor.Col = firstNonBlank(b.curLine())
		b.recMutated = true
	case 'c':
		b.reg = b.copyLines(r1, r2)
		b.regLinewise = true
		// replace the spanned lines with a single empty line, enter insert
		newLines := append([][]rune{}, b.lines[:r1]...)
		newLines = append(newLines, []rune{})
		if r2+1 <= len(b.lines) {
			newLines = append(newLines, b.lines[r2+1:]...)
		}
		b.lines = newLines
		b.cursor = Pos{r1, 0}
		b.enterInsert()
		b.recMutated = true
	}
}

func (b *Buffer) applyCharOp(op rune, target Pos, kind motionKind) {
	f, t := orderPos(b.cursor, target)
	if kind == mInclusive {
		// Include the target rune. deleteRange/copyRange clamp t.Col to the line
		// length, so this is also correct when the inclusive motion (e, $ with a
		// count) lands on a different row — previously the +1 was skipped across
		// rows, leaving the last char of the span behind.
		t.Col++
	}
	switch op {
	case 'y':
		b.reg = b.copyRange(f, t)
		b.regLinewise = false
		b.justYanked = b.reg
		b.cursor = f
		b.clampCursor()
	case 'd':
		b.reg = b.deleteRange(f, t)
		b.regLinewise = false
		b.cursor = f
		b.clampCursor()
		b.recMutated = true
	case 'c':
		b.reg = b.deleteRange(f, t)
		b.regLinewise = false
		b.cursor = f
		b.enterInsert()
		b.recMutated = true
	}
}
