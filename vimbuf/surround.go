package vimbuf

// surroundPair returns the opening and closing strings for a surround char. For
// brackets the matching pair is used; for quotes/markdown markers the same string
// is used on both sides.
func surroundPair(c rune) (open, close string, ok bool) {
	switch c {
	case '(', ')', 'b':
		return "(", ")", true
	case '[', ']':
		return "[", "]", true
	case '{', '}':
		return "{", "}", true
	case '<', '>':
		return "<", ">", true
	case '"', '\'', '`':
		return string(c), string(c), true
	case '*':
		return "*", "*", true
	case '_':
		return "_", "_", true
	}
	return "", "", false
}

// surroundSelection wraps the current visual selection with the pair for c, as
// one undo step, then returns to Normal mode. Charwise wraps the span; linewise
// wraps each selected line's content.
func (b *Buffer) surroundSelection(c rune) {
	open, close, ok := surroundPair(c)
	if !ok {
		b.exitVisual()
		return
	}
	f, t := orderPos(b.anchor, b.cursor)
	b.begin()
	if b.mode == ModeVisualLine {
		for r := f.Row; r <= t.Row && r < len(b.lines); r++ {
			b.lines[r] = []rune(open + string(b.lines[r]) + close)
		}
		b.cursor = Pos{f.Row, 0}
	} else {
		end := Pos{t.Row, t.Col + 1}
		if f.Row == end.Row {
			line := b.lines[f.Row]
			if end.Col > len(line) {
				end.Col = len(line)
			}
			nl := string(line[:f.Col]) + open + string(line[f.Col:end.Col]) + close + string(line[end.Col:])
			b.lines[f.Row] = []rune(nl)
		} else {
			// multi-line charwise: insert open at start, close at end
			b.lines[end.Row] = []rune(string(b.lines[end.Row][:min(end.Col, len(b.lines[end.Row]))]) + close + string(b.lines[end.Row][min(end.Col, len(b.lines[end.Row])):]))
			b.lines[f.Row] = []rune(string(b.lines[f.Row][:f.Col]) + open + string(b.lines[f.Row][f.Col:]))
		}
		b.cursor = f
	}
	b.recMutated = true
	b.endGroup()
	b.mode = ModeNormal
	b.clampCursor()
}

// deleteSurround implements ds<char>: removes the nearest surrounding pair for
// the char on the current line.
func (b *Buffer) deleteSurround(c rune) {
	open, _, ok := surroundPair(c)
	if !ok {
		return
	}
	oc, cc := []rune(open)[0], lastRune(b.closingOf(c))
	li, ri := b.findSurround(oc, cc)
	if li < 0 || ri < 0 {
		return
	}
	line := b.curLine()
	b.begin()
	nl := append([]rune{}, line[:li]...)
	nl = append(nl, line[li+1:ri]...)
	nl = append(nl, line[ri+1:]...)
	b.lines[b.cursor.Row] = nl
	if b.cursor.Col > li {
		b.cursor.Col--
	}
	b.clampCursor()
	b.recMutated = true
}

// changeSurround implements cs<old><new>: replaces the surrounding pair.
func (b *Buffer) changeSurround(oldc, newc rune) {
	if _, _, ok := surroundPair(oldc); !ok {
		return
	}
	open, close, ok := surroundPair(newc)
	if !ok {
		return
	}
	oc, cc := []rune(b.openingOf(oldc))[0], lastRune(b.closingOf(oldc))
	li, ri := b.findSurround(oc, cc)
	if li < 0 || ri < 0 {
		return
	}
	line := b.curLine()
	b.begin()
	nl := append([]rune{}, line[:li]...)
	nl = append(nl, []rune(open)...)
	nl = append(nl, line[li+1:ri]...)
	nl = append(nl, []rune(close)...)
	nl = append(nl, line[ri+1:]...)
	b.lines[b.cursor.Row] = nl
	b.clampCursor()
	b.recMutated = true
}

func (b *Buffer) openingOf(c rune) string {
	o, _, _ := surroundPair(c)
	return o
}
func (b *Buffer) closingOf(c rune) string {
	_, cl, _ := surroundPair(c)
	return cl
}

func lastRune(s string) rune {
	r := []rune(s)
	if len(r) == 0 {
		return 0
	}
	return r[len(r)-1]
}

// findSurround locates the open/close runes surrounding the cursor on the current
// line. For matched brackets it respects nesting; for same-char quotes it picks
// the nearest pair around the cursor.
func (b *Buffer) findSurround(oc, cc rune) (li, ri int) {
	line := b.curLine()
	col := b.cursor.Col
	if oc == cc {
		// nearest pair on the line straddling/after the cursor
		left := -1
		for i := col; i >= 0; i-- {
			if i < len(line) && line[i] == oc {
				left = i
				break
			}
		}
		if left < 0 {
			for i := range line {
				if line[i] == oc {
					left = i
					break
				}
			}
		}
		if left < 0 {
			return -1, -1
		}
		right := -1
		for i := left + 1; i < len(line); i++ {
			if line[i] == cc {
				right = i
				break
			}
		}
		return left, right
	}
	// matched brackets with nesting
	depth := 0
	left := -1
	for i := col; i >= 0; i-- {
		if i >= len(line) {
			continue
		}
		if line[i] == cc && i != col {
			depth++
		} else if line[i] == oc {
			if depth == 0 {
				left = i
				break
			}
			depth--
		}
	}
	if left < 0 {
		return -1, -1
	}
	depth = 0
	for i := left + 1; i < len(line); i++ {
		if line[i] == oc {
			depth++
		} else if line[i] == cc {
			if depth == 0 {
				return left, i
			}
			depth--
		}
	}
	return left, -1
}
