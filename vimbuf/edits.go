package vimbuf

import (
	"regexp"
	"strings"
)

// StartInsert puts a freshly opened buffer directly into insert mode (used for
// the append/prepend/journal "add text" editor states). It opens an undo group
// so leaving insert and undoing restores the empty starting buffer.
func (b *Buffer) StartInsert() {
	b.begin()
	b.enterInsert()
}

// enterInsert switches to insert mode, allowing the cursor to rest one past the
// last rune. Assumes an undo group is already open.
//
// Entering insert ends the count-accumulation phase, so any pending count is
// cleared here. We don't implement counted insert (e.g. 3iX → XXX), and without
// this the count would survive the insert session — handleNormal's finishCommand
// only runs in normal mode — and leak into the next command (2iX<esc>x would
// delete two chars). Operator paths consume their count before this point.
func (b *Buffer) enterInsert() {
	b.pendingCount = 0
	b.opCount = 0
	b.mode = ModeInsert
	b.clampCursorInsert()
	b.scrollToCursor()
}

func (b *Buffer) openLineBelow() {
	row := b.cursor.Row
	newLine := []rune(b.listPrefix(row))
	b.lines = insertLine(b.lines, row+1, newLine)
	b.cursor = Pos{row + 1, len(newLine)}
	b.scrollToCursor()
	b.recordEdit()
}

func (b *Buffer) openLineAbove() {
	row := b.cursor.Row
	newLine := []rune(b.listPrefix(row))
	b.lines = insertLine(b.lines, row, newLine)
	b.cursor = Pos{row, len(newLine)}
	b.scrollToCursor()
	b.recordEdit()
}

func (b *Buffer) deleteCharsUnderCursor(count int) {
	line := b.curLine()
	if len(line) == 0 {
		return // nothing to delete; don't open an undo group for a no-op x
	}
	b.begin()
	col := b.cursor.Col
	end := min(col+count, len(line))
	b.reg = string(line[col:end])
	b.regLinewise = false
	nl := append(append([]rune{}, line[:col]...), line[end:]...)
	b.lines[b.cursor.Row] = nl
	b.clampCursor()
	b.recordEdit()
}

func (b *Buffer) changeLine() {
	b.begin()
	b.reg = string(b.curLine()) + "\n"
	b.regLinewise = true
	changed := len(b.lines[b.cursor.Row]) > 0
	b.lines[b.cursor.Row] = []rune{}
	b.cursor.Col = 0
	b.enterInsert()
	if changed {
		b.recordEdit()
	} else {
		b.recMutated = true
	}
}

func (b *Buffer) changeToEnd() {
	b.begin()
	line := b.curLine()
	col := b.cursor.Col
	if col < len(line) {
		b.reg = string(line[col:])
		b.regLinewise = false
		b.lines[b.cursor.Row] = append([]rune{}, line[:col]...)
		b.recordEdit()
	}
	b.enterInsert()
	b.recMutated = true
}

func (b *Buffer) deleteToEnd() {
	b.begin()
	line := b.curLine()
	col := b.cursor.Col
	if col < len(line) {
		b.reg = string(line[col:])
		b.regLinewise = false
		b.lines[b.cursor.Row] = append([]rune{}, line[:col]...)
		b.recordEdit()
	}
	b.clampCursor()
	b.recMutated = true
}

func (b *Buffer) joinLines(count int) {
	if count < 2 {
		count = 2 // J with no count joins the next line (2 lines total)
	}
	b.begin()
	joins := count - 1
	changed := false
	for range joins {
		if b.cursor.Row >= len(b.lines)-1 {
			break
		}
		cur := b.lines[b.cursor.Row]
		next := b.lines[b.cursor.Row+1]
		// strip leading whitespace of the joined line; separate with a space
		trimmed := next
		for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\t') {
			trimmed = trimmed[1:]
		}
		joinCol := len(cur)
		merged := append([]rune{}, cur...)
		if len(cur) > 0 && len(trimmed) > 0 {
			merged = append(merged, ' ')
		}
		merged = append(merged, trimmed...)
		b.lines[b.cursor.Row] = merged
		b.lines = append(b.lines[:b.cursor.Row+1], b.lines[b.cursor.Row+2:]...)
		b.cursor.Col = joinCol
		changed = true
	}
	b.clampCursor()
	if changed {
		b.recordEdit()
	} else {
		b.recMutated = true
	}
}

func (b *Buffer) toggleCharCase(count int) {
	b.begin()
	line := append([]rune{}, b.curLine()...)
	col := b.cursor.Col
	changed := false
	for range count {
		if col >= len(line) {
			break
		}
		next := caseRune('~', line[col])
		if next != line[col] {
			changed = true
		}
		line[col] = next
		col++
	}
	b.lines[b.cursor.Row] = line
	b.cursor.Col = col
	b.clampCursor()
	if changed {
		b.recordEdit()
	} else {
		b.recMutated = true
	}
}

func (b *Buffer) doReplaceChar(key string, count int) {
	r := keyRune(key)
	if r == 0 {
		return
	}
	line := b.curLine()
	col := b.cursor.Col
	if col+count > len(line) {
		return // not enough chars to replace
	}
	b.begin()
	nl := append([]rune{}, line...)
	for i := range count {
		nl[col+i] = r
	}
	b.lines[b.cursor.Row] = nl
	b.cursor.Col = col + count - 1
	b.clampCursor()
	b.recordEdit()
}

func (b *Buffer) paste(after bool, count int) {
	if b.reg == "" {
		return
	}
	b.begin()
	if b.regLinewise {
		text := strings.TrimSuffix(b.reg, "\n")
		src := strings.Split(text, "\n")
		var block [][]rune
		for range count {
			for _, s := range src {
				block = append(block, []rune(s))
			}
		}
		at := b.cursor.Row
		if after {
			at++
		}
		tail := append([][]rune{}, b.lines[at:]...)
		b.lines = append(append(b.lines[:at:at], block...), tail...)
		b.cursor = Pos{at, firstNonBlank(b.lineAt(at))}
	} else {
		text := strings.Repeat(b.reg, count)
		line := b.curLine()
		col := b.cursor.Col
		if after && len(line) > 0 {
			col++
		}
		if col > len(line) {
			col = len(line)
		}
		ins := []rune(text)
		nl := append(append(append([]rune{}, line[:col]...), ins...), line[col:]...)
		b.lines[b.cursor.Row] = nl
		b.cursor.Col = col + len(ins) - 1
	}
	b.clampCursor()
	b.recMutated = true
	b.scrollToCursor()
	b.markChanged()
}

// --- text objects -----------------------------------------------------------

// applyTextObject resolves an i/a object and applies the pending operator over
// it. io is 'i' (inner) or 'a' (a/around); obj is the object char.
func (b *Buffer) applyTextObject(io, obj rune) {
	if b.pendingOp == 0 {
		return
	}
	from, to, linewise, ok := b.resolveTextObject(io, obj)
	if !ok {
		return
	}
	b.begin()
	op := b.pendingOp
	if linewise {
		b.applyOp(op, Pos{to.Row, 0}, mLinewise)
		// applyOp linewise uses cursor.Row..target.Row; set cursor to from row
		// first so the span is correct.
		return
	}
	// charwise: deleteRange uses [from,to) exclusive; emulate by setting cursor
	// to from and applying with an exclusive synthetic motion.
	b.cursor = from
	switch op {
	case 'y':
		b.reg = b.copyRange(from, to)
		b.regLinewise = false
		b.justYanked = b.reg
		b.cursor = from
	case 'd':
		b.reg = b.deleteRange(from, to)
		b.regLinewise = false
		b.cursor = from
		b.recMutated = true
	case 'c':
		b.reg = b.deleteRange(from, to)
		b.regLinewise = false
		b.cursor = from
		b.enterInsert()
		b.recMutated = true
	case 'u', 'U', '~':
		b.caseRange(op, from, Pos{to.Row, to.Col - 1}, mInclusive)
		b.recMutated = true
	}
	b.clampCursor()
}

// resolveTextObject returns the span [from,to) (exclusive end for charwise) of a
// text object. For paragraph objects it returns linewise=true with from/to rows.
func (b *Buffer) resolveTextObject(io, obj rune) (from, to Pos, linewise, ok bool) {
	switch obj {
	case 'w':
		return b.wordObject(io)
	case '"', '\'', '`':
		return b.quoteObject(io, obj)
	case '(', ')', 'b':
		return b.pairObject(io, '(', ')')
	case '[', ']':
		return b.pairObject(io, '[', ']')
	case '{', '}':
		return b.pairObject(io, '{', '}')
	case 'p':
		return b.paragraphObject(io)
	}
	return from, to, false, false
}

func (b *Buffer) wordObject(io rune) (Pos, Pos, bool, bool) {
	line := b.curLine()
	col := b.cursor.Col
	if len(line) == 0 {
		return Pos{}, Pos{}, false, false
	}
	if col >= len(line) {
		col = len(line) - 1
	}
	cl := classOf(line[col])
	start := col
	for start > 0 && classOf(line[start-1]) == cl {
		start--
	}
	end := col
	for end < len(line)-1 && classOf(line[end+1]) == cl {
		end++
	}
	endExcl := end + 1
	if io == 'a' { // include trailing whitespace
		for endExcl < len(line) && classOf(line[endExcl]) == clSpace {
			endExcl++
		}
	}
	return Pos{b.cursor.Row, start}, Pos{b.cursor.Row, endExcl}, false, true
}

func (b *Buffer) quoteObject(io, q rune) (Pos, Pos, bool, bool) {
	line := b.curLine()
	col := b.cursor.Col
	// find the pair surrounding or after the cursor on this line
	open, close := -1, -1
	// search left for an opening quote
	for i := col; i >= 0; i-- {
		if i < len(line) && line[i] == q {
			open = i
			break
		}
	}
	if open >= 0 {
		for i := open + 1; i < len(line); i++ {
			if line[i] == q {
				close = i
				break
			}
		}
	}
	if open < 0 || close < 0 {
		// search forward for a full pair
		first := -1
		for i := col; i < len(line); i++ {
			if line[i] == q {
				if first < 0 {
					first = i
				} else {
					open, close = first, i
					break
				}
			}
		}
	}
	if open < 0 || close < 0 {
		return Pos{}, Pos{}, false, false
	}
	if io == 'i' {
		return Pos{b.cursor.Row, open + 1}, Pos{b.cursor.Row, close}, false, true
	}
	return Pos{b.cursor.Row, open}, Pos{b.cursor.Row, close + 1}, false, true
}

func (b *Buffer) pairObject(io, openCh, closeCh rune) (Pos, Pos, bool, bool) {
	line := b.curLine()
	col := b.cursor.Col
	// find matching open to the left (same line, with nesting)
	depth := 0
	open := -1
	for i := col; i >= 0; i-- {
		if i >= len(line) {
			continue
		}
		if line[i] == closeCh && i != col {
			depth++
		} else if line[i] == openCh {
			if depth == 0 {
				open = i
				break
			}
			depth--
		}
	}
	if open < 0 {
		return Pos{}, Pos{}, false, false
	}
	depth = 0
	close := -1
	for i := open + 1; i < len(line); i++ {
		if line[i] == openCh {
			depth++
		} else if line[i] == closeCh {
			if depth == 0 {
				close = i
				break
			}
			depth--
		}
	}
	if close < 0 {
		return Pos{}, Pos{}, false, false
	}
	if io == 'i' {
		return Pos{b.cursor.Row, open + 1}, Pos{b.cursor.Row, close}, false, true
	}
	return Pos{b.cursor.Row, open}, Pos{b.cursor.Row, close + 1}, false, true
}

func (b *Buffer) paragraphObject(io rune) (Pos, Pos, bool, bool) {
	row := b.cursor.Row
	blank := len(b.lineAt(row)) == 0
	start := row
	for start > 0 && (len(b.lineAt(start-1)) == 0) == blank {
		start--
	}
	end := row
	for end < len(b.lines)-1 && (len(b.lineAt(end+1)) == 0) == blank {
		end++
	}
	if io == 'a' {
		for end < len(b.lines)-1 && len(b.lineAt(end+1)) == 0 {
			end++
		}
	}
	b.cursor.Row = start
	return Pos{start, 0}, Pos{end, 0}, true, true
}

// --- search -----------------------------------------------------------------

func (b *Buffer) searchNext(sameDir bool, count int) Effect {
	if b.lastSearch == "" {
		return Effect{Status: "no previous search"}
	}
	forward := b.searchForward
	if !sameDir {
		forward = !forward
	}
	return b.runSearch(b.lastSearch, forward, count, false)
}

func (b *Buffer) runSearch(term string, forward bool, count int, remember bool) Effect {
	if term == "" {
		if b.lastSearch == "" {
			return Effect{Status: "no previous search"}
		}
		term = b.lastSearch
	}
	re, err := regexp.Compile(term)
	if err != nil {
		return Effect{Status: "bad search: " + err.Error()}
	}
	if count <= 0 {
		count = 1
	}
	orig := b.cursor
	if remember {
		b.lastSearch = term
		b.searchForward = forward
	}
	for range count {
		if !b.doSearch(re, forward) {
			b.cursor = orig
			b.desiredCol = orig.Col
			b.clampCursor()
			b.scrollToCursor()
			return Effect{Status: "pattern not found: " + term}
		}
	}
	return Effect{}
}

func (b *Buffer) doSearch(re *regexp.Regexp, forward bool) bool {
	start := b.cursor
	total := len(b.lines)
	if forward {
		for off := 0; off <= total; off++ {
			row := (start.Row + off) % total
			line := string(b.lines[row])
			from := 0 // byte offset
			if off == 0 {
				from = byteIndexOfRune(line, start.Col+1)
			}
			if idx := regexpIndexFrom(re, line, from); idx >= 0 {
				b.cursor = Pos{row, runeLen(line[:idx])}
				b.desiredCol = b.cursor.Col
				b.clampCursor()
				b.scrollToCursor()
				return true
			}
		}
	} else {
		for off := 0; off <= total; off++ {
			row := ((start.Row-off)%total + total) % total
			line := string(b.lines[row])
			limit := len(line)
			if off == 0 {
				limit = byteIndexOfRune(line, start.Col)
			}
			if idx := regexpLastIndexBefore(re, line, limit); idx >= 0 {
				b.cursor = Pos{row, runeLen(line[:idx])}
				b.desiredCol = b.cursor.Col
				b.clampCursor()
				b.scrollToCursor()
				return true
			}
		}
	}
	return false
}

// --- low-level line helpers -------------------------------------------------

func insertLine(lines [][]rune, at int, line []rune) [][]rune {
	if at < 0 {
		at = 0
	}
	if at > len(lines) {
		at = len(lines)
	}
	tail := append([][]rune{}, lines[at:]...)
	return append(append(lines[:at:at], line), tail...)
}

func leadingWhitespace(line []rune) []rune {
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	return line[:n]
}

func regexpIndexFrom(re *regexp.Regexp, s string, from int) int {
	if from > len(s) {
		return -1
	}
	if from < 0 {
		from = 0
	}
	loc := re.FindStringIndex(s[from:])
	if loc == nil {
		return -1
	}
	return from + loc[0]
}

func regexpLastIndexBefore(re *regexp.Regexp, s string, limit int) int {
	if limit > len(s) {
		limit = len(s)
	}
	if limit < 0 {
		return -1
	}
	locs := re.FindAllStringIndex(s[:limit], -1)
	for i := len(locs) - 1; i >= 0; i-- {
		if locs[i][0] < limit {
			return locs[i][0]
		}
	}
	return -1
}

func runeLen(s string) int { return len([]rune(s)) }

func byteIndexOfRune(s string, runeIdx int) int {
	n := 0
	for i := range s {
		if n == runeIdx {
			return i
		}
		n++
	}
	return len(s)
}
