package vimbuf

// handleVisual processes keys in charwise (ModeVisual) and linewise
// (ModeVisualLine) visual modes: motions extend the selection from the anchor,
// and operators act on the selected span.
func (b *Buffer) handleVisual(key string) Effect {
	// awaiting the wrap char for a visual S (surround)
	if b.awaitSurround {
		b.awaitSurround = false
		if r := keyRune(key); r != 0 {
			b.surroundSelection(r)
		} else {
			b.exitVisual()
		}
		return Effect{}
	}
	// awaiting object char for an in-visual text object (viw, ci"-style select)
	if b.textObjPending != 0 {
		io := b.textObjPending
		b.textObjPending = 0
		if r := keyRune(key); r != 0 {
			if from, to, linewise, ok := b.resolveTextObject(io, r); ok {
				if linewise {
					b.mode = ModeVisualLine
					b.anchor = Pos{from.Row, 0}
					b.cursor = Pos{to.Row, 0}
				} else {
					b.anchor = from
					b.cursor = Pos{to.Row, max(to.Col-1, 0)}
				}
				b.clampCursor()
				b.scrollToCursor()
			}
		}
		return Effect{}
	}
	// awaiting target char for an in-visual f/F/t/T
	if b.findPending != 0 {
		cmd := b.findPending
		b.findPending = 0
		if r := keyRune(key); r != 0 {
			b.lastFind.cmd = cmd
			b.lastFind.target = r
			if p, _, ok := b.findChar(cmd, r, b.takeCount(), false); ok {
				b.cursor = p
				b.clampCursor()
				b.scrollToCursor()
			}
		}
		return Effect{}
	}

	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		b.pendingCount = b.pendingCount*10 + int(key[0]-'0')
		return Effect{}
	}
	if b.gPending {
		b.gPending = false
		if key == "g" {
			b.cursor = b.gotoFirstLine(b.takeCount())
			b.clampCursor()
			b.scrollToCursor()
		}
		return Effect{}
	}

	switch key {
	case "esc", "ctrl+[":
		b.exitVisual()
		return Effect{}
	case "v":
		if b.mode == ModeVisual {
			b.exitVisual()
		} else {
			b.mode = ModeVisual
		}
		return Effect{}
	case "V":
		if b.mode == ModeVisualLine {
			b.exitVisual()
		} else {
			b.mode = ModeVisualLine
		}
		return Effect{}
	case "o":
		b.cursor, b.anchor = b.anchor, b.cursor
		b.clampCursor()
		b.scrollToCursor()
		return Effect{}
	case "g":
		b.gPending = true
		return Effect{}
	case "f", "F", "t", "T":
		b.findPending = rune(key[0])
		return Effect{}
	case "d", "x":
		b.visualDelete(false)
		return Effect{}
	case "y":
		b.visualYank()
		return Effect{}
	case "c", "s":
		b.visualDelete(true)
		return Effect{}
	case "S":
		b.awaitSurround = true
		return Effect{}
	case "i", "a":
		b.textObjPending = rune(key[0])
		return Effect{}
	case ">", "<":
		f, t := orderPos(b.anchor, b.cursor)
		b.begin()
		b.indentLines(f.Row, t.Row, key == ">")
		b.cursor = Pos{f.Row, firstNonBlank(b.lineAt(f.Row))}
		b.endGroup()
		b.exitVisual()
		return Effect{}
	case "u", "U", "~":
		f, t := orderPos(b.anchor, b.cursor)
		b.begin()
		kind := mInclusive
		if b.mode == ModeVisualLine {
			kind = mLinewise
		}
		b.caseRange(rune(key[0]), f, t, kind)
		b.endGroup()
		b.exitVisual()
		return Effect{}
	case ":":
		// enter ex command-line pre-seeded with the visual line range
		f, t := orderPos(b.anchor, b.cursor)
		b.visualRange = &Range{Start: f.Row, End: t.Row}
		b.mode = ModeCommand
		b.cmdline = []rune("'<,'>")
		b.resetCompletion()
		b.takeCount()
		return Effect{}
	}

	// motions extend the selection
	if target, _, ok := b.motion(key, b.effCount()); ok {
		b.cursor = target
		b.clampCursor()
		b.moveCursor(target, key)
		b.takeCount()
	}
	return Effect{}
}

func (b *Buffer) exitVisual() {
	b.mode = ModeNormal
	b.clampCursor()
	b.takeCount()
}

func (b *Buffer) visualYank() {
	f, t := orderPos(b.anchor, b.cursor)
	if b.mode == ModeVisualLine {
		b.reg = b.copyLines(f.Row, t.Row)
		b.regLinewise = true
		b.cursor = Pos{f.Row, 0}
	} else {
		end := Pos{t.Row, t.Col + 1}
		b.reg = b.copyRange(f, end)
		b.regLinewise = false
		b.cursor = f
	}
	b.justYanked = b.reg
	b.exitVisual()
}

func (b *Buffer) visualDelete(change bool) {
	f, t := orderPos(b.anchor, b.cursor)
	b.begin()
	if b.mode == ModeVisualLine {
		b.reg = b.deleteLines(f.Row, t.Row)
		b.regLinewise = true
		if change {
			b.lines = insertLine(b.lines, f.Row, []rune{})
			b.cursor = Pos{f.Row, 0}
			b.enterInsert()
		} else {
			b.cursor.Row = f.Row
			b.clampCursor()
			b.cursor.Col = firstNonBlank(b.curLine())
		}
	} else {
		end := Pos{t.Row, t.Col + 1}
		b.reg = b.deleteRange(f, end)
		b.regLinewise = false
		b.cursor = f
		if change {
			b.enterInsert()
		} else {
			b.clampCursor()
		}
	}
	b.recMutated = true
	if !change {
		b.endGroup()
		b.mode = ModeNormal
	} else {
		b.mode = ModeInsert
	}
	b.scrollToCursor()
}
