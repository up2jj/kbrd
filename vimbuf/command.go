package vimbuf

import (
	"strconv"
	"strings"
)

// exCommands are the static ex-command names offered for first-token completion.
var exCommands = []string{"w", "q", "q!", "wq", "x", "lua"}

// handleCommand processes keys while the ":" / "/" / "?" command-line is active.
func (b *Buffer) handleCommand(key string) Effect {
	switch key {
	case "esc", "ctrl+[":
		b.leaveCommand()
		return Effect{}
	case "enter":
		return b.runCommand()
	case "backspace", "ctrl+h":
		if len(b.cmdline) > 0 {
			b.cmdline = b.cmdline[:len(b.cmdline)-1]
		}
		b.resetCompletion()
		if len(b.cmdline) == 0 {
			b.leaveCommand()
		}
		return Effect{}
	case "tab":
		b.completeCmdline()
		return Effect{}
	case "space":
		b.cmdline = append(b.cmdline, ' ')
		b.resetCompletion()
		return Effect{}
	}
	if r := []rune(key); len(r) == 1 {
		b.cmdline = append(b.cmdline, r...)
		b.resetCompletion()
	}
	return Effect{}
}

func (b *Buffer) leaveCommand() {
	b.mode = ModeNormal
	b.cmdline = b.cmdline[:0]
	b.visualRange = nil
	b.resetCompletion()
	b.clampCursor()
}

// runCommand parses and executes the command-line on <enter>, returning the host
// Effect (save/quit/eval) and leaving command mode.
func (b *Buffer) runCommand() Effect {
	line := string(b.cmdline)
	vr := b.visualRange
	b.leaveCommand()

	if strings.HasPrefix(line, "/") || strings.HasPrefix(line, "?") {
		forward := line[0] == '/'
		term := line[1:]
		return b.runSearch(term, forward, 1, true)
	}

	cmd := strings.TrimSpace(line)
	// A leading range marker selects/operates on a line span: the visual marker
	// "'<,'>" (captured when ':' was pressed in linewise visual), "%" for the
	// whole file, "N,M" for a range, or "Ncmd" for a single addressed line.
	var rng *Range
	if strings.HasPrefix(cmd, "%") {
		rng = &Range{Start: 0, End: len(b.lines) - 1}
		cmd = strings.TrimSpace(cmd[1:])
	} else if strings.HasPrefix(cmd, "'<,'>") {
		if vr != nil {
			r := *vr
			rng = &r
		}
		cmd = strings.TrimSpace(cmd[len("'<,'>"):])
	} else if s, e, rest, ok := b.parseNumericRange(cmd); ok {
		rng = &Range{Start: s, End: e}
		cmd = rest
		// "N,M" with no command selects those lines in linewise visual so the
		// span is visible (and operable with d/y/>/:lua, etc.).
		if cmd == "" {
			b.anchor = Pos{s, 0}
			b.cursor = Pos{e, 0}
			b.clampCursor()
			b.mode = ModeVisualLine
			b.scrollToCursor()
			return Effect{}
		}
	} else if s, rest, ok := b.parseNumericAddress(cmd); ok {
		if rest == "" {
			b.gotoLineNumber(s + 1)
			return Effect{}
		}
		rng = &Range{Start: s, End: s}
		cmd = rest
	}

	switch {
	case cmd == "w":
		return Effect{Submit: true}
	case cmd == "q":
		return Effect{Quit: true}
	case cmd == "q!":
		return Effect{ForceQuit: true}
	case cmd == "wq" || cmd == "x":
		return Effect{Submit: true, Quit: true}
	case cmd == "help" || cmd == "h":
		return Effect{Help: true}
	case strings.HasPrefix(cmd, "lua "), cmd == "lua":
		expr := strings.TrimSpace(strings.TrimPrefix(cmd, "lua"))
		if expr == "" {
			return Effect{Status: "usage: :lua <expr>"}
		}
		return Effect{EvalExpr: expr, EvalRange: rng}
	case len(cmd) >= 2 && cmd[0] == 's' && !isWordByte(cmd[1]):
		return b.runSubstitute(cmd, rng)
	case cmd == "":
		return Effect{}
	}
	// :<number> jumps to that 1-based line.
	if n, err := strconv.Atoi(cmd); err == nil {
		b.gotoLineNumber(n)
		return Effect{}
	}
	return Effect{Status: "not a command: " + cmd}
}

// parseNumericRange parses a leading "N,M" line range (1-based) from cmd,
// returning the 0-based inclusive span (ordered, clamped to the buffer) and the
// remaining command text. ok is false when cmd does not start with "N,M".
func (b *Buffer) parseNumericRange(cmd string) (start, end int, rest string, ok bool) {
	i := 0
	for i < len(cmd) && cmd[i] >= '0' && cmd[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(cmd) || cmd[i] != ',' {
		return 0, 0, "", false
	}
	j := i + 1
	for j < len(cmd) && cmd[j] >= '0' && cmd[j] <= '9' {
		j++
	}
	if j == i+1 {
		return 0, 0, "", false
	}
	n1, _ := strconv.Atoi(cmd[:i])
	n2, _ := strconv.Atoi(cmd[i+1 : j])
	s, e := n1-1, n2-1
	if s > e {
		s, e = e, s
	}
	s = max(s, 0)
	e = min(e, len(b.lines)-1)
	return s, e, strings.TrimSpace(cmd[j:]), true
}

// parseNumericAddress parses a leading "N" address (1-based) and returns the
// addressed row plus any command suffix. A bare "N" is handled by runCommand as
// a line jump; "Ncmd" applies cmd to row N.
func (b *Buffer) parseNumericAddress(cmd string) (row int, rest string, ok bool) {
	i := 0
	for i < len(cmd) && cmd[i] >= '0' && cmd[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, "", false
	}
	n, _ := strconv.Atoi(cmd[:i])
	row = min(max(n-1, 0), len(b.lines)-1)
	return row, strings.TrimSpace(cmd[i:]), true
}

// GoToLine moves the cursor to the given 1-based line (clamped), used when
// opening the editor at a specific line.
func (b *Buffer) GoToLine(n int) { b.gotoLineNumber(n) }

// gotoLineNumber moves the cursor to the given 1-based line (clamped).
func (b *Buffer) gotoLineNumber(n int) {
	row := min(max(n-1, 0), len(b.lines)-1)
	b.cursor = Pos{row, firstNonBlank(b.lineAt(row))}
	b.desiredCol = b.cursor.Col
	b.clampCursor()
	b.scrollToCursor()
}

// --- autocomplete -----------------------------------------------------------

func (b *Buffer) resetCompletion() {
	b.compActive = false
	b.compMatches = nil
	b.compIndex = 0
	b.compPrefix = ""
}

// completeCmdline cycles tab-completion: ex-command names for the first token,
// or registered function names for the leading identifier after "lua ".
func (b *Buffer) completeCmdline() {
	if b.compActive && len(b.compMatches) > 0 {
		b.compIndex = (b.compIndex + 1) % len(b.compMatches)
		b.applyCompletion()
		return
	}
	prefix, isLua := b.completionToken()
	var matches []Completion
	if isLua {
		for _, c := range b.completions {
			if strings.HasPrefix(c.Name, prefix) {
				matches = append(matches, c)
			}
		}
	} else {
		for _, name := range exCommands {
			if strings.HasPrefix(name, prefix) {
				matches = append(matches, Completion{Name: name})
			}
		}
	}
	if len(matches) == 0 {
		return
	}
	b.compActive = true
	b.compMatches = matches
	b.compIndex = 0
	b.compPrefix = prefix
	b.applyCompletion()
}

// completionToken returns the token being completed and whether it is a :lua
// function argument (vs the leading ex-command).
func (b *Buffer) completionToken() (string, bool) {
	s := strings.TrimPrefix(string(b.cmdline), "'<,'>")
	s = strings.TrimLeft(s, " ")
	if rest, ok := strings.CutPrefix(s, "lua "); ok {
		// complete the leading identifier of the expression
		id := rest
		if i := strings.IndexAny(id, "( \t"); i >= 0 {
			id = id[:i]
		}
		return id, true
	}
	return s, false
}

// applyCompletion rewrites the command-line so the active token becomes the
// current match.
func (b *Buffer) applyCompletion() {
	if b.compIndex >= len(b.compMatches) {
		return
	}
	name := b.compMatches[b.compIndex].Name
	s := string(b.cmdline)
	rangePrefix := ""
	if strings.HasPrefix(s, "'<,'>") {
		rangePrefix = "'<,'>"
		s = s[len("'<,'>"):]
	}
	lead := s[:len(s)-len(strings.TrimLeft(s, " "))]
	body := strings.TrimLeft(s, " ")
	if rest, ok := strings.CutPrefix(body, "lua "); ok {
		tail := ""
		if i := strings.IndexAny(rest, "( \t"); i >= 0 {
			tail = rest[i:]
		}
		body = "lua " + name + tail
	} else {
		body = name
	}
	b.cmdline = []rune(rangePrefix + lead + body)
}

// CommandLine reports the current command-line text (for rendering).
func (b *Buffer) CommandLine() string { return string(b.cmdline) }

// CompletionHint returns the active completion's name and usage for rendering,
// or empty strings when no completion is active.
func (b *Buffer) CompletionHint() (name, usage string) {
	if !b.compActive || b.compIndex >= len(b.compMatches) {
		return "", ""
	}
	c := b.compMatches[b.compIndex]
	return c.Name, c.Usage
}
