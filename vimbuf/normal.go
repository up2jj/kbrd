package vimbuf

// handleNormal runs the normal-mode key dispatcher: the
// {count}{operator}{count}{motion} state machine plus standalone edits. State
// persists across calls in the buffer's pending* fields.
func (b *Buffer) handleNormal(key string) Effect {
	eff := b.dispatchNormal(key)
	if b.mode == ModeNormal && !b.pending() {
		b.finishCommand()
	}
	return eff
}

// pending reports whether a multi-key command is mid-sequence.
func (b *Buffer) pending() bool {
	return b.pendingOp != 0 || b.pendingCount != 0 || b.gPending ||
		b.findPending != 0 || b.textObjPending != 0 || b.replacePending ||
		b.surroundOp != 0
}

// finishCommand closes the current undo group and commits the recorded key
// sequence as the "." change if it mutated the buffer.
func (b *Buffer) finishCommand() {
	b.endGroup()
	if b.replaying || b.rec == nil {
		return
	}
	if b.recMutated {
		b.lastChange = b.rec
	}
	b.rec = nil
	b.recMutated = false
}

// discardRec drops the in-progress recording (for commands that are not
// dot-repeatable, e.g. entering command-line or visual mode).
func (b *Buffer) discardRec() {
	if !b.replaying {
		b.rec = nil
		b.recMutated = false
	}
}

func (b *Buffer) dispatchNormal(key string) Effect {
	// 1. awaiting replacement char for r
	if b.replacePending {
		b.replacePending = false
		b.doReplaceChar(key, b.takeCount())
		return Effect{}
	}
	// 2. awaiting target char for f/F/t/T
	if b.findPending != 0 {
		cmd := b.findPending
		b.findPending = 0
		if r := keyRune(key); r != 0 {
			b.lastFind.cmd = cmd
			b.lastFind.target = r
			b.applyFind(cmd, r, b.takeCount())
		} else {
			b.resetPending()
		}
		return Effect{}
	}
	// 3. awaiting text-object char after op + i/a
	if b.textObjPending != 0 {
		io := b.textObjPending
		b.textObjPending = 0
		if r := keyRune(key); r != 0 {
			b.applyTextObject(io, r)
		}
		b.resetPending()
		return Effect{}
	}
	// 4. leading g
	if b.gPending {
		b.gPending = false
		return b.handleGPrefix(key)
	}
	// 4b. surround: ds<char> / cs<old><new>
	if b.surroundOp == 'd' {
		b.surroundOp = 0
		if r := keyRune(key); r != 0 {
			b.deleteSurround(r)
		}
		b.resetPending()
		return Effect{}
	}
	if b.surroundOp == 'c' {
		if b.surroundOldChar == 0 {
			if r := keyRune(key); r != 0 {
				b.surroundOldChar = r
			} else {
				b.surroundOp = 0
				b.resetPending()
			}
			return Effect{}
		}
		old := b.surroundOldChar
		b.surroundOp, b.surroundOldChar = 0, 0
		if r := keyRune(key); r != 0 {
			b.changeSurround(old, r)
		}
		b.resetPending()
		return Effect{}
	}

	// 5. count digits ("0" is a motion unless a count is already building)
	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
		if !(key == "0" && b.pendingCount == 0) {
			b.pendingCount = b.pendingCount*10 + int(key[0]-'0')
			return Effect{}
		}
	}

	// 6. operators
	switch key {
	case "d", "c", "y", ">", "<":
		op := rune(key[0])
		if b.pendingOp == op { // dd, cc, yy, >>, <<
			b.begin()
			cnt := b.combinedCount()
			target := Pos{min(b.cursor.Row+cnt-1, len(b.lines)-1), 0}
			b.applyOp(op, target, mLinewise)
			b.resetPending()
			return Effect{}
		}
		if b.pendingOp == 0 {
			b.pendingOp = op
			b.opCount = b.pendingCount
			b.pendingCount = 0
			return Effect{}
		}
	}

	// 7. i/a after an operator → text object
	if b.pendingOp != 0 && (key == "i" || key == "a") {
		b.textObjPending = rune(key[0])
		return Effect{}
	}

	// 7a. surround command start: ds / cs (the d/c operator followed by s, which
	// is not a motion — repurposed for vim-surround).
	if (b.pendingOp == 'd' || b.pendingOp == 'c') && key == "s" {
		b.surroundOp = b.pendingOp
		b.pendingOp = 0
		return Effect{}
	}

	// 7b. stateful motion prefixes that await another key. These must return here
	// (not fall through to the motion/standalone steps, which would reset the
	// pending state on the very next dispatch).
	switch key {
	case "f", "F", "t", "T":
		b.findPending = rune(key[0])
		return Effect{}
	case "g":
		b.gPending = true
		return Effect{}
	}

	// vim special-case: `cw`/`cW` change to the end of the word (like `ce`),
	// not up to the start of the next word.
	if b.pendingOp == 'c' && key == "w" {
		key = "e"
	}

	// 8. motions (plain, or operator target)
	if target, kind, ok := b.tryMotion(key); ok {
		if b.pendingOp != 0 {
			b.begin()
			b.applyOp(b.pendingOp, target, kind)
			b.resetPending()
		} else {
			b.moveCursor(target, key)
			b.takeCount()
		}
		return Effect{}
	}

	// 9. standalone commands (only when no operator is pending)
	if b.pendingOp != 0 {
		// unknown key after operator: cancel
		b.resetPending()
		return Effect{}
	}
	return b.standalone(key)
}

// tryMotion resolves the simple motions plus the f/t repeat keys ; and ,. The
// stateful prefixes f/F/t/T and g are handled earlier in dispatchNormal.
func (b *Buffer) tryMotion(key string) (Pos, motionKind, bool) {
	switch key {
	case "G":
		if b.pendingCount > 0 {
			row := min(max(b.pendingCount-1, 0), len(b.lines)-1)
			return Pos{row, firstNonBlank(b.lineAt(row))}, mLinewise, true
		}
		row := len(b.lines) - 1
		return Pos{row, firstNonBlank(b.lineAt(row))}, mLinewise, true
	case ";":
		if b.lastFind.cmd != 0 {
			return b.findChar(b.lastFind.cmd, b.lastFind.target, b.effCount(), true)
		}
		return b.cursor, mExclusive, false
	case ",":
		if b.lastFind.cmd != 0 {
			return b.findChar(flipFind(b.lastFind.cmd), b.lastFind.target, b.effCount(), true)
		}
		return b.cursor, mExclusive, false
	}
	return b.motion(key, b.effCount())
}

// applyFind resolves an f/F/t/T after its target char arrived, either moving the
// cursor or applying a pending operator.
func (b *Buffer) applyFind(cmd, target rune, count int) {
	p, kind, ok := b.findChar(cmd, target, count, false)
	if !ok {
		b.resetPending()
		return
	}
	if b.pendingOp != 0 {
		b.begin()
		b.applyOp(b.pendingOp, p, kind)
		b.resetPending()
		return
	}
	b.cursor = p
	b.desiredCol = b.cursor.Col
	b.clampCursor()
	b.scrollToCursor()
}

func (b *Buffer) handleGPrefix(key string) Effect {
	switch key {
	case "g": // gg
		target := b.gotoFirstLine(b.takeCount())
		if b.pendingOp != 0 {
			b.begin()
			b.applyOp(b.pendingOp, target, mLinewise)
			b.resetPending()
		} else {
			b.cursor = target
			b.desiredCol = b.cursor.Col
			b.clampCursor()
			b.scrollToCursor()
		}
	case "u", "U", "~":
		// gu/gU/g~ are case operators; set as pending op awaiting a motion.
		b.pendingOp = rune(key[0])
		b.opCount = b.pendingCount
		b.pendingCount = 0
	default:
		b.resetPending()
	}
	return Effect{}
}

// standalone handles single-key normal commands that are not motions/operators.
func (b *Buffer) standalone(key string) Effect {
	switch key {
	case "i":
		b.begin()
		b.enterInsert()
	case "I":
		b.begin()
		b.cursor.Col = firstNonBlank(b.curLine())
		b.enterInsert()
	case "a":
		b.begin()
		if len(b.curLine()) > 0 {
			b.cursor.Col++
		}
		b.enterInsert()
	case "A":
		b.begin()
		b.cursor.Col = len(b.curLine())
		b.enterInsert()
	case "o":
		b.begin()
		b.openLineBelow()
		b.enterInsert()
	case "O":
		b.begin()
		b.openLineAbove()
		b.enterInsert()
	case "x":
		b.deleteCharsUnderCursor(b.takeCount())
	case "s":
		b.begin()
		b.deleteCharsUnderCursor(b.takeCount())
		b.enterInsert()
	case "S", "cc":
		b.changeLine()
	case "C":
		b.changeToEnd()
	case "D":
		b.deleteToEnd()
	case "r":
		b.replacePending = true
	case "J":
		b.joinLines(b.effCount())
		b.takeCount()
	case "~":
		b.toggleCharCase(b.takeCount())
	case "p":
		b.paste(true, b.effCount())
		b.takeCount()
	case "P":
		b.paste(false, b.effCount())
		b.takeCount()
	case "u", "ctrl+z":
		b.Undo()
		b.takeCount()
	case "ctrl+r", "ctrl+y":
		b.Redo()
		b.takeCount()
	case "ctrl+a":
		b.incrementNumber(b.takeCount())
	case "ctrl+x":
		b.incrementNumber(-b.takeCount())
	case "tab":
		b.toggleCheckbox()
		b.takeCount()
	case "&":
		if b.lastSub.pat != "" {
			b.substitute(b.cursor.Row, b.cursor.Row, b.lastSub.pat, b.lastSub.repl, b.lastSub.global)
		}
		b.takeCount()
	case ".":
		cnt := b.takeCount()
		b.repeatLastChange(cnt)
		// "." is not itself a recordable change — drop its recording so the
		// replayed change stays the last change (and "." never records ".").
		b.discardRec()
	case "v":
		b.discardRec()
		b.anchor = b.cursor
		b.mode = ModeVisual
		b.takeCount()
	case "V":
		b.discardRec()
		b.anchor = b.cursor
		b.mode = ModeVisualLine
		b.takeCount()
	case ":":
		b.discardRec()
		b.mode = ModeCommand
		b.cmdline = b.cmdline[:0]
		b.visualRange = nil
		b.resetCompletion()
		b.takeCount()
	case "/", "?":
		b.discardRec()
		b.mode = ModeCommand
		b.cmdline = []rune(key) // search uses the command-line buffer prefixed by / or ?
		b.takeCount()
	case "n":
		b.searchNext(true)
		b.takeCount()
	case "N":
		b.searchNext(false)
		b.takeCount()
	default:
		b.resetPending()
	}
	return Effect{}
}

// --- count helpers ----------------------------------------------------------

// effCount returns the active count for a motion (operator count × motion count
// when an operator is pending), defaulting to 1.
func (b *Buffer) effCount() int {
	return b.combinedCount()
}

func (b *Buffer) combinedCount() int {
	oc := max(b.opCount, 1)
	mc := max(b.pendingCount, 1)
	return oc * mc
}

// takeCount returns the pending count (default 1) and clears the count state.
func (b *Buffer) takeCount() int {
	c := b.pendingCount
	b.pendingCount = 0
	if c < 1 {
		c = 1
	}
	return c
}

func (b *Buffer) resetPending() {
	b.pendingOp = 0
	b.pendingCount = 0
	b.opCount = 0
	b.gPending = false
	b.findPending = 0
	b.textObjPending = 0
	b.replacePending = false
}

// moveCursor applies a plain (non-operator) motion result, updating desiredCol
// except for vertical motions (which preserve it).
func (b *Buffer) moveCursor(target Pos, key string) {
	b.cursor = target
	b.clampCursor()
	switch key {
	case "j", "k", "up", "down":
		// preserve desiredCol
	case "$":
		b.desiredCol = 1 << 30
	default:
		b.desiredCol = b.cursor.Col
	}
	b.scrollToCursor()
}

func keyRune(key string) rune {
	r := []rune(key)
	if len(r) == 1 {
		return r[0]
	}
	if key == "space" {
		return ' '
	}
	return 0
}

func flipFind(cmd rune) rune {
	switch cmd {
	case 'f':
		return 'F'
	case 'F':
		return 'f'
	case 't':
		return 'T'
	case 'T':
		return 't'
	}
	return cmd
}
