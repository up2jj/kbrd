package vimbuf

// motionKind classifies how a motion's span is treated by an operator. vim's
// delete/change math depends on it: `dw` is exclusive (stops before the next
// word) while `de` is inclusive (eats the last char of the word).
type motionKind int

const (
	mExclusive motionKind = iota
	mInclusive
	mLinewise
)

// charClass partitions runes for word motions: words are runs of word-chars or
// runs of punctuation, separated by whitespace.
type charClass int

const (
	clSpace charClass = iota
	clWord
	clPunct
)

func classOf(r rune) charClass {
	switch {
	case r == ' ' || r == '\t':
		return clSpace
	case r == '_' || isLetter(r) || isDigit(r) || r > 127:
		return clWord
	default:
		return clPunct
	}
}

func isLetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }
func isDigit(r rune) bool  { return r >= '0' && r <= '9' }

// motion computes the target of a simple (non char-find) motion from the current
// cursor, applied count times. ok is false for an unknown motion key.
func (b *Buffer) motion(key string, count int) (Pos, motionKind, bool) {
	if count < 1 {
		count = 1
	}
	switch key {
	case "h", "left":
		p := b.cursor
		for range count {
			if p.Col > 0 {
				p.Col--
			}
		}
		return p, mExclusive, true
	case "l", "right", "space":
		p := b.cursor
		for range count {
			if p.Col < len(b.lineAt(p.Row)) {
				p.Col++
			}
		}
		return p, mExclusive, true
	case "j", "down":
		p := b.cursor
		for range count {
			if p.Row < len(b.lines)-1 {
				p.Row++
			}
		}
		p.Col = b.desiredCol
		return p, mLinewise, true
	case "k", "up":
		p := b.cursor
		for range count {
			if p.Row > 0 {
				p.Row--
			}
		}
		p.Col = b.desiredCol
		return p, mLinewise, true
	case "0":
		return Pos{b.cursor.Row, 0}, mExclusive, true
	case "$":
		p := b.cursor
		for range count - 1 {
			if p.Row < len(b.lines)-1 {
				p.Row++
			}
		}
		p.Col = lastCol(b.lineAt(p.Row))
		return p, mInclusive, true
	case "^":
		return Pos{b.cursor.Row, firstNonBlank(b.lineAt(b.cursor.Row))}, mExclusive, true
	case "w":
		p := b.cursor
		for range count {
			p = b.wordForward(p)
		}
		return p, mExclusive, true
	case "b":
		p := b.cursor
		for range count {
			p = b.wordBackward(p)
		}
		return p, mExclusive, true
	case "e":
		p := b.cursor
		for range count {
			p = b.wordEnd(p)
		}
		return p, mInclusive, true
	case "}":
		p := b.cursor
		for range count {
			p = b.paragraphForward(p)
		}
		return p, mExclusive, true
	case "{":
		p := b.cursor
		for range count {
			p = b.paragraphBackward(p)
		}
		return p, mExclusive, true
	}
	return b.cursor, mExclusive, false
}

// gotoLine resolves gg (count given = that line, else first) / G handled above.
func (b *Buffer) gotoFirstLine(count int) Pos {
	row := 0
	if count > 0 {
		row = count - 1
	}
	if row >= len(b.lines) {
		row = len(b.lines) - 1
	}
	if row < 0 {
		row = 0
	}
	return Pos{row, firstNonBlank(b.lineAt(row))}
}

// findChar resolves f/F/t/T to a target column on the current line. ok is false
// when the target rune is not found. skipAdjacent is set when repeating via ; / ,
// so a t/T that is already parked next to the target advances past it instead of
// standing still.
func (b *Buffer) findChar(cmd, target rune, count int, skipAdjacent bool) (Pos, motionKind, bool) {
	if count < 1 {
		count = 1
	}
	line := b.curLine()
	col := b.cursor.Col
	switch cmd {
	case 'f':
		c := col
		for range count {
			found := indexForward(line, target, c+1)
			if found < 0 {
				return b.cursor, mInclusive, false
			}
			c = found
		}
		return Pos{b.cursor.Row, c}, mInclusive, true
	case 'F':
		c := col
		for range count {
			found := indexBackward(line, target, c-1)
			if found < 0 {
				return b.cursor, mExclusive, false
			}
			c = found
		}
		return Pos{b.cursor.Row, c}, mExclusive, true
	case 't':
		c := col
		for i := range count {
			start := c + 1
			if i == 0 && skipAdjacent {
				start = c + 2
			}
			found := indexForward(line, target, start)
			if found < 0 {
				return b.cursor, mInclusive, false
			}
			c = found
		}
		return Pos{b.cursor.Row, c - 1}, mInclusive, true
	case 'T':
		c := col
		for i := range count {
			start := c - 1
			if i == 0 && skipAdjacent {
				start = c - 2
			}
			found := indexBackward(line, target, start)
			if found < 0 {
				return b.cursor, mExclusive, false
			}
			c = found
		}
		return Pos{b.cursor.Row, c + 1}, mExclusive, true
	}
	return b.cursor, mInclusive, false
}

func indexForward(line []rune, target rune, from int) int {
	for i := from; i < len(line); i++ {
		if line[i] == target {
			return i
		}
	}
	return -1
}

func indexBackward(line []rune, target rune, from int) int {
	for i := from; i >= 0; i-- {
		if line[i] == target {
			return i
		}
	}
	return -1
}

// --- word/paragraph primitives ---------------------------------------------

func (b *Buffer) wordForward(p Pos) Pos {
	line := b.lineAt(p.Row)
	// from inside the current word, skip to its end+1
	if p.Col < len(line) {
		cl := classOf(line[p.Col])
		if cl != clSpace {
			for p.Col < len(line) && classOf(line[p.Col]) == cl {
				p.Col++
			}
		}
	}
	// skip whitespace, wrapping lines
	for {
		line = b.lineAt(p.Row)
		if p.Col >= len(line) {
			if p.Row >= len(b.lines)-1 {
				p.Col = len(line)
				return p
			}
			p.Row++
			p.Col = 0
			// an empty line counts as a word start
			if len(b.lineAt(p.Row)) == 0 {
				return p
			}
			continue
		}
		if classOf(line[p.Col]) == clSpace {
			p.Col++
			continue
		}
		return p
	}
}

func (b *Buffer) wordBackward(p Pos) Pos {
	// step back one
	if p.Col == 0 {
		if p.Row == 0 {
			return p
		}
		p.Row--
		p.Col = len(b.lineAt(p.Row))
	} else {
		p.Col--
	}
	// skip whitespace backwards
	for {
		line := b.lineAt(p.Row)
		if p.Col >= len(line) {
			if len(line) == 0 {
				// empty line is a word
				return p
			}
			p.Col = len(line) - 1
		}
		if p.Col < 0 {
			if p.Row == 0 {
				return Pos{0, 0}
			}
			p.Row--
			p.Col = len(b.lineAt(p.Row))
			continue
		}
		if classOf(line[p.Col]) == clSpace {
			p.Col--
			if p.Col < 0 {
				if p.Row == 0 {
					return Pos{0, 0}
				}
				p.Row--
				p.Col = len(b.lineAt(p.Row))
			}
			continue
		}
		break
	}
	// move to start of this word
	line := b.lineAt(p.Row)
	if p.Col >= len(line) {
		return p
	}
	cl := classOf(line[p.Col])
	for p.Col > 0 && classOf(line[p.Col-1]) == cl {
		p.Col--
	}
	return p
}

func (b *Buffer) wordEnd(p Pos) Pos {
	line := b.lineAt(p.Row)
	// step forward one
	p.Col++
	for {
		line = b.lineAt(p.Row)
		if p.Col >= len(line) {
			if p.Row >= len(b.lines)-1 {
				p.Col = lastCol(line)
				return p
			}
			p.Row++
			p.Col = 0
			continue
		}
		if classOf(line[p.Col]) == clSpace {
			p.Col++
			continue
		}
		break
	}
	// advance to end of this word
	line = b.lineAt(p.Row)
	cl := classOf(line[p.Col])
	for p.Col < len(line)-1 && classOf(line[p.Col+1]) == cl {
		p.Col++
	}
	return p
}

func (b *Buffer) paragraphForward(p Pos) Pos {
	row := p.Row + 1
	for row < len(b.lines)-1 && len(b.lines[row]) != 0 {
		row++
	}
	// land on the next blank line, or last line
	for row < len(b.lines) && len(b.lines[row]) != 0 {
		row++
	}
	if row >= len(b.lines) {
		row = len(b.lines) - 1
	}
	return Pos{row, 0}
}

func (b *Buffer) paragraphBackward(p Pos) Pos {
	row := p.Row - 1
	for row > 0 && len(b.lines[row]) != 0 {
		row--
	}
	for row > 0 && len(b.lines[row]) == 0 && row == p.Row-1 {
		// already on a blank directly above; keep going up to previous para break
		row--
	}
	if row < 0 {
		row = 0
	}
	return Pos{row, 0}
}

func lastCol(line []rune) int {
	if len(line) == 0 {
		return 0
	}
	return len(line) - 1
}

func firstNonBlank(line []rune) int {
	for i, r := range line {
		if r != ' ' && r != '\t' {
			return i
		}
	}
	return 0
}
