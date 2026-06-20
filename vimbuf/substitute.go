package vimbuf

import (
	"regexp"
	"strings"
)

// subSpec records the last :s for the & repeat command.
type subSpec struct {
	pat    string
	repl   string
	global bool
}

// isWordByte reports whether c could be part of an identifier, used to tell the
// `s` substitute command (s/.../.../) apart from other commands.
func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// runSubstitute parses and applies an :s command: s/pat/repl/[g]. The delimiter
// is whatever non-word byte follows `s`. rng (when set) bounds the lines; without
// a range it operates on the current line.
func (b *Buffer) runSubstitute(cmd string, rng *Range) Effect {
	delim := cmd[1]
	parts := splitUnescaped(cmd[2:], rune(delim))
	if len(parts) < 2 {
		return Effect{Status: "usage: :s/pat/repl/[g]"}
	}
	pat, repl := parts[0], parts[1]
	flags := ""
	if len(parts) >= 3 {
		flags = parts[2]
	}
	if pat == "" {
		pat = b.lastSub.pat // empty pattern reuses the last one
	}
	if pat == "" {
		// No pattern given and none remembered — an empty regex would match the
		// empty string at every position, so reject it instead.
		return Effect{Status: "no previous substitute pattern"}
	}
	global := strings.Contains(flags, "g")

	start, end := b.cursor.Row, b.cursor.Row
	if rng != nil {
		start, end = rng.Start, rng.End
	}
	n, err := b.substitute(start, end, pat, repl, global)
	if err != nil {
		return Effect{Status: "bad pattern: " + err.Error()}
	}
	if n == 0 {
		return Effect{Status: "pattern not found: " + pat}
	}
	b.lastSub = subSpec{pat: pat, repl: repl, global: global}
	return Effect{}
}

// substitute applies the regexp replacement over lines [start,end] as one undo
// step, returning the number of replacements.
func (b *Buffer) substitute(start, end int, pat, repl string, global bool) (int, error) {
	re, err := regexp.Compile(pat)
	if err != nil {
		return 0, err
	}
	if start < 0 {
		start = 0
	}
	if end >= len(b.lines) {
		end = len(b.lines) - 1
	}
	tmpl := vimReplToGo(repl)
	count := 0
	lastRow := -1
	// Compute every replacement first so a no-match :s doesn't push an undo step
	// (which would force an extra `u` to revert the prior real edit).
	type edit struct {
		row  int
		text string
	}
	var edits []edit
	for r := start; r <= end; r++ {
		s := string(b.lines[r])
		locs := re.FindAllStringSubmatchIndex(s, -1)
		if len(locs) == 0 {
			continue
		}
		if !global {
			locs = locs[:1]
		}
		var out strings.Builder
		prev := 0
		for _, loc := range locs {
			out.WriteString(s[prev:loc[0]])
			out.Write(re.ExpandString(nil, tmpl, s, loc))
			prev = loc[1]
			count++
		}
		out.WriteString(s[prev:])
		edits = append(edits, edit{r, out.String()})
		lastRow = r
	}
	if count == 0 {
		return 0, nil
	}
	b.pushUndo()
	for _, e := range edits {
		b.lines[e.row] = []rune(e.text)
	}
	b.cursor.Row = lastRow
	b.clampCursor()
	b.scrollToCursor()
	b.recMutated = true
	return count, nil
}

// vimReplToGo translates a vim-style replacement (\1 groups, & whole match) into
// a Go regexp ExpandString template ($1, $0), escaping literal $.
func vimReplToGo(repl string) string {
	var sb strings.Builder
	for i := 0; i < len(repl); i++ {
		c := repl[i]
		switch {
		case c == '\\' && i+1 < len(repl):
			n := repl[i+1]
			switch {
			case n >= '0' && n <= '9':
				sb.WriteString("${" + string(n) + "}")
			case n == '&':
				sb.WriteString("&")
			case n == '\\':
				sb.WriteByte('\\')
			default:
				sb.WriteByte(n)
			}
			i++
		case c == '&':
			sb.WriteString("${0}")
		case c == '$':
			sb.WriteString("$$")
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// splitUnescaped splits s on delim, treating "\delim" as a literal delim and
// dropping the escaping backslash. Returns at most the natural number of fields.
func splitUnescaped(s string, delim rune) []string {
	var parts []string
	var cur strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) && runes[i+1] == delim {
			cur.WriteRune(delim)
			i++
			continue
		}
		if runes[i] == delim {
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(runes[i])
	}
	parts = append(parts, cur.String())
	return parts
}
