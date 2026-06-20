package vimbuf

import (
	"strconv"
	"strings"
)

// listContinuation inspects a line for a markdown list marker and returns the
// prefix to start the next item with (checkbox reset to unchecked, ordered
// number incremented), the marker's length in the original line, and whether the
// line has no content after the marker. ok is false when there is no marker.
func listContinuation(line string) (next string, markerLen int, empty bool, ok bool) {
	rs := []rune(line)
	i := 0
	for i < len(rs) && (rs[i] == ' ' || rs[i] == '\t') {
		i++
	}
	indent := string(rs[:i])
	if i >= len(rs) {
		return "", 0, false, false
	}

	// unordered: - / * / + followed by a space
	if (rs[i] == '-' || rs[i] == '*' || rs[i] == '+') && i+1 < len(rs) && rs[i+1] == ' ' {
		bullet := string(rs[i])
		j := i + 2
		// optional task checkbox: [ ] / [x] / [X]
		if j+2 < len(rs) && rs[j] == '[' && (rs[j+1] == ' ' || rs[j+1] == 'x' || rs[j+1] == 'X') && rs[j+2] == ']' {
			k := j + 3
			if k < len(rs) && rs[k] == ' ' {
				k++
			}
			markerLen = k
			next = indent + bullet + " [ ] "
			empty = strings.TrimSpace(string(rs[markerLen:])) == ""
			return next, markerLen, empty, true
		}
		markerLen = j
		next = indent + bullet + " "
		empty = strings.TrimSpace(string(rs[markerLen:])) == ""
		return next, markerLen, empty, true
	}

	// ordered: digits . space
	d := i
	for d < len(rs) && rs[d] >= '0' && rs[d] <= '9' {
		d++
	}
	if d > i && d < len(rs) && rs[d] == '.' && d+1 < len(rs) && rs[d+1] == ' ' {
		num, _ := strconv.Atoi(string(rs[i:d]))
		markerLen = d + 2
		next = indent + strconv.Itoa(num+1) + ". "
		empty = strings.TrimSpace(string(rs[markerLen:])) == ""
		return next, markerLen, empty, true
	}
	return "", 0, false, false
}

// insertNewlineSmart is the insert-mode <enter> handler with markdown list
// awareness: pressing enter on a list item continues the list; pressing it on an
// empty item removes the marker instead.
func (b *Buffer) insertNewlineSmart() {
	line := string(b.curLine())
	next, markerLen, empty, ok := listContinuation(line)
	if ok && empty && b.cursor.Col >= markerLen {
		// Empty marker: clear the line and stay (end the list).
		b.lines[b.cursor.Row] = []rune{}
		b.cursor.Col = 0
		return
	}
	b.insertNewline()
	if ok && !empty {
		b.insertRunes([]rune(next))
	}
}

// listPrefix returns the continuation prefix for `o`/`O` on a line (the next
// marker for a list line, else just the leading whitespace).
func (b *Buffer) listPrefix(row int) string {
	line := string(b.lineAt(row))
	if next, _, _, ok := listContinuation(line); ok {
		return next
	}
	return string(leadingWhitespace(b.lineAt(row)))
}
