package vimbuf

// handleInsert processes a key in insert mode: printable runes insert at the
// cursor; esc returns to normal mode (committing the insert session as one undo
// step and one "." change).
func (b *Buffer) handleInsert(key string) Effect {
	switch key {
	case "esc", "ctrl+[":
		b.mode = ModeNormal
		if b.cursor.Col > 0 {
			b.cursor.Col--
		}
		b.clampCursor()
		b.desiredCol = b.cursor.Col
		b.finishCommand()
		return Effect{}
	case "enter":
		b.insertNewlineSmart()
		b.recMutated = true
		return Effect{}
	case "backspace", "ctrl+h":
		b.insertBackspace()
		b.recMutated = true
		return Effect{}
	case "tab":
		b.insertRunes([]rune(spaces(b.indentWidth)))
		b.recMutated = true
		return Effect{}
	case "space":
		b.insertRunes([]rune{' '})
		b.recMutated = true
		return Effect{}
	case "left":
		if b.cursor.Col > 0 {
			b.cursor.Col--
		}
		return Effect{}
	case "right":
		if b.cursor.Col < len(b.curLine()) {
			b.cursor.Col++
		}
		return Effect{}
	case "up":
		if b.cursor.Row > 0 {
			b.cursor.Row--
			b.clampCursorInsert()
		}
		b.scrollToCursor()
		return Effect{}
	case "down":
		if b.cursor.Row < len(b.lines)-1 {
			b.cursor.Row++
			b.clampCursorInsert()
		}
		b.scrollToCursor()
		return Effect{}
	}
	if r := []rune(key); len(r) == 1 {
		b.insertRunes(r)
		b.recMutated = true
	}
	return Effect{}
}

func (b *Buffer) insertRunes(rs []rune) {
	line := b.curLine()
	col := min(b.cursor.Col, len(line))
	nl := append(append(append([]rune{}, line[:col]...), rs...), line[col:]...)
	b.lines[b.cursor.Row] = nl
	b.cursor.Col = col + len(rs)
	b.desiredCol = b.cursor.Col
	b.scrollToCursor()
	b.markChanged()
}

func (b *Buffer) insertNewline() {
	line := b.curLine()
	col := min(b.cursor.Col, len(line))
	head := append([]rune{}, line[:col]...)
	tail := append([]rune{}, line[col:]...)
	b.lines[b.cursor.Row] = head
	b.lines = insertLine(b.lines, b.cursor.Row+1, tail)
	b.cursor = Pos{b.cursor.Row + 1, 0}
	b.desiredCol = 0
	b.scrollToCursor()
	b.markChanged()
}

func (b *Buffer) insertBackspace() {
	col := b.cursor.Col
	if col > 0 {
		line := b.curLine()
		nl := append(append([]rune{}, line[:col-1]...), line[col:]...)
		b.lines[b.cursor.Row] = nl
		b.cursor.Col--
		b.desiredCol = b.cursor.Col
		b.markChanged()
		return
	}
	// join with previous line
	if b.cursor.Row == 0 {
		return
	}
	prev := b.lines[b.cursor.Row-1]
	cur := b.curLine()
	joinCol := len(prev)
	merged := append(append([]rune{}, prev...), cur...)
	b.lines[b.cursor.Row-1] = merged
	b.lines = append(b.lines[:b.cursor.Row], b.lines[b.cursor.Row+1:]...)
	b.cursor = Pos{b.cursor.Row - 1, joinCol}
	b.desiredCol = joinCol
	b.scrollToCursor()
	b.markChanged()
}

func spaces(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = ' '
	}
	return string(out)
}
