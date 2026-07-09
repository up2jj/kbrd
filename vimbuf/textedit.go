package vimbuf

import (
	"strconv"
	"strings"
)

// TaskPrefix is the literal markdown unchecked-task marker inserted by ctrl+t.
const TaskPrefix = "- [ ] "

// incrementNumber finds the number at or after the cursor on the current line
// and adds delta to it (ctrl+a / ctrl+x), leaving the cursor on the last digit.
func (b *Buffer) incrementNumber(delta int) {
	line := b.curLine()
	i := b.cursor.Col
	for i < len(line) && !isDigit(line[i]) {
		i++
	}
	if i >= len(line) {
		return
	}
	start := i
	for start > 0 && isDigit(line[start-1]) {
		start--
	}
	if start > 0 && line[start-1] == '-' {
		start-- // include a leading minus so the value parses as negative
	}
	end := i
	for end < len(line) && isDigit(line[end]) {
		end++
	}
	n, err := strconv.Atoi(string(line[start:end]))
	if err != nil {
		return
	}
	n += delta
	repl := []rune(strconv.Itoa(n))
	b.begin()
	nl := append(append(append([]rune{}, line[:start]...), repl...), line[end:]...)
	b.lines[b.cursor.Row] = nl
	b.cursor.Col = start + len(repl) - 1
	b.clampCursor()
	b.recordEdit()
}

// InsertText inserts s (which may contain newlines) at the cursor as one undo
// step, leaving the cursor after the inserted text. Used for clipboard and
// bracketed paste. Works in any mode (the cursor stays where it lands).
func (b *Buffer) InsertText(s string) {
	if s == "" {
		return
	}
	b.begin()
	for i, ln := range strings.Split(s, "\n") {
		if i > 0 {
			b.insertNewline()
		}
		b.insertRunes([]rune(ln))
	}
	b.recMutated = true
	b.scrollToCursor()
	// InsertText is called directly by the host (clipboard/bracketed paste),
	// bypassing HandleKey's finishCommand, so close the undo group here. Otherwise
	// grouping stays open and the next edit folds into the paste's undo step — one
	// u would revert both.
	b.endGroup()
}

// InsertTaskPrefix toggles an existing task marker on the current line, inserts
// an unchecked markdown task marker at the cursor, or prefixes every line
// touched by a visual selection after its indentation.
func (b *Buffer) InsertTaskPrefix() {
	if b.mode != ModeVisual && b.mode != ModeVisualLine {
		if b.mode == ModeNormal {
			b.resetPending()
			b.discardRec()
		}
		if isTaskLine(b.curLine()) {
			b.toggleCheckbox()
			b.endGroup()
			b.scrollToCursor()
			return
		}
		b.InsertText(TaskPrefix)
		return
	}
	f, t := orderPos(b.anchor, b.cursor)
	changed := false
	for row := f.Row; row <= t.Row; row++ {
		line := b.lineAt(row)
		if isTaskLine(line) {
			continue
		}
		if !changed {
			b.begin()
			changed = true
		}
		i := len(leadingWhitespace(line))
		nl := append([]rune{}, line[:i]...)
		nl = append(nl, []rune(TaskPrefix)...)
		nl = append(nl, line[i:]...)
		b.lines[row] = nl
	}
	if changed {
		b.endGroup()
		b.recordEdit()
	}
	b.cursor = Pos{f.Row, len(leadingWhitespace(b.lineAt(f.Row))) + len([]rune(TaskPrefix))}
	b.exitVisual()
	b.scrollToCursor()
}

func isTaskLine(line []rune) bool {
	i := len(leadingWhitespace(line))
	return i+5 <= len(line) &&
		(line[i] == '-' || line[i] == '*' || line[i] == '+') &&
		line[i+1] == ' ' &&
		line[i+2] == '[' &&
		(line[i+3] == ' ' || line[i+3] == 'x' || line[i+3] == 'X') &&
		line[i+4] == ']' &&
		(i+5 == len(line) || line[i+5] == ' ')
}

// toggleCheckbox flips a markdown task checkbox ("[ ]" <-> "[x]") on the current
// line. No-op when the line has no checkbox.
func (b *Buffer) toggleCheckbox() {
	line := string(b.curLine())
	var nl string
	switch {
	case strings.Contains(line, "[ ]"):
		nl = strings.Replace(line, "[ ]", "[x]", 1)
	case strings.Contains(line, "[x]"):
		nl = strings.Replace(line, "[x]", "[ ]", 1)
	case strings.Contains(line, "[X]"):
		nl = strings.Replace(line, "[X]", "[ ]", 1)
	default:
		return
	}
	b.begin()
	b.lines[b.cursor.Row] = []rune(nl)
	b.clampCursor()
	b.recordEdit()
}
