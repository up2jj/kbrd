// Package vimbuf is a dependency-free, modal (vim-like) text-buffer engine.
//
// It owns the text, cursor, modes, operator grammar, undo/redo and rendering of
// a single editable buffer, with no knowledge of Bubble Tea, the board, or the
// scripting host. Input arrives as key strings (the value of
// tea.KeyMsg.String()) via HandleKey, which returns an Effect describing any
// side effects the host must carry out (save/quit/eval). This keeps the engine
// unit-testable in isolation and keeps modal grammar out of the model package.
package vimbuf

import (
	"strconv"
	"strings"
)

// Mode is the editor's current modal state.
type Mode int

const (
	ModeNormal Mode = iota
	ModeInsert
	ModeVisual     // charwise visual
	ModeVisualLine // linewise visual
	ModeCommand    // ":" ex command-line
)

// Pos is a cursor position. Col is a rune index into the line (never a byte
// offset), so multibyte/CJK text does not corrupt motions.
type Pos struct {
	Row, Col int
}

const undoLimit = 200

// snapshot is one undo/redo entry: a deep copy of the buffer text plus cursor.
type snapshot struct {
	lines  [][]rune
	cursor Pos
}

// change records the raw key sequence of the last buffer-mutating command so
// "." can replay it by feeding the same keys back through HandleKey (e.g.
// `cwfoo<esc>` is stored as ["c","w","f","o","o","esc"]).
type change struct {
	seq []string
}

// Effect is what HandleKey hands back to the host after processing a key.
// The zero value means "nothing to do beyond the in-buffer edit".
type Effect struct {
	Submit    bool   // :w / :wq / :x — host should persist the buffer
	Quit      bool   // :q — host should close if clean, else refuse
	ForceQuit bool   // :q! — host should close, discarding
	Help      bool   // :help / :h — host should show the cheatsheet
	Yank      string // text just yanked — host mirrors it to the system clipboard
	EvalExpr  string
	EvalRange *Range // non-nil: operand is this line range; nil: current line
	Status    string // transient status-line message (errors, hints)
}

// Range is an inclusive 0-based row span used for range :lua operands.
type Range struct {
	Start, End int
}

// Completion is one candidate for command-line autocomplete: a registered
// function (or ex-command) name and an optional usage hint shown to the user.
type Completion struct {
	Name  string
	Usage string
}

// Buffer is the modal text engine. Construct with New.
type Buffer struct {
	lines      [][]rune
	cursor     Pos
	desiredCol int // sticky column target for vertical (j/k) motion
	mode       Mode
	anchor     Pos // visual-mode selection start

	// operator-pending state machine
	pendingOp      rune // 'd' 'c' 'y' '>' '<' 'u' 'U' '~' (case via g) or 0
	pendingCount   int  // count accumulated before/within an operator (0 = none)
	opCount        int  // count seen before the operator (for {count}op{count}motion)
	gPending       bool // saw a leading 'g' (gg, gu, gU, g~)
	findPending    rune // awaiting target char for f/F/t/T (0 = none)
	textObjPending rune // awaiting object char after op+i/op+a ('i' or 'a', 0 = none)
	replacePending bool // awaiting replacement char for r
	pendingMotion  bool // op was set with a count; next key is the motion

	// surround (vim-surround style): ds<char>, cs<old><new>, visual S<char>
	surroundOp      rune // 'd' or 'c' while a ds/cs is in progress (0 = none)
	surroundOldChar rune // the <old> char of an in-progress cs
	awaitSurround   bool // visual S pressed, awaiting the wrap char

	// viewport
	top    int
	height int
	width  int

	reg         string // unnamed register contents
	regLinewise bool   // register holds whole lines (affects p/P)
	justYanked  string // text yanked by the current command (mirrored to clipboard)

	lastSearch    string
	searchForward bool
	lastSub       subSpec // last :s, replayed by &

	lastFind struct {
		cmd    rune // 'f' 'F' 't' 'T' or 0
		target rune
	}

	lastChange *change
	rec        *change // command currently being recorded for "."
	recMutated bool    // the in-progress recorded command has changed the buffer
	replaying  bool    // guard so "." replay doesn't re-record

	cmdline     []rune
	visualRange *Range       // line range captured when ':' is pressed in linewise visual
	completions []Completion // command-line autocomplete candidates (injected by host)
	compActive  bool         // a tab-completion cycle is in progress
	compMatches []Completion // current cycle's matches
	compIndex   int          // index into compMatches
	compPrefix  string       // the token being completed (before tab cycling)

	indentWidth int

	undo, redo []snapshot
	grouping   bool // true while a multi-key command/insert session shares one undo step
	pendingReg byte // reserved for future named registers
}

// New builds a buffer seeded with text (split on "\n"), cursor at the origin,
// in Normal mode.
func New(text string) *Buffer {
	b := &Buffer{
		mode:        ModeNormal,
		indentWidth: 2,
	}
	b.SetText(text)
	b.cursor = Pos{0, 0}
	return b
}

// SetSize records the viewport dimensions (content area, gutter excluded).
func (b *Buffer) SetSize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	b.width, b.height = w, h
	b.scrollToCursor()
}

// SetEvalCompletions injects the command-line autocomplete candidates (registered
// function names + usage hints). The host owns the truth; the engine just holds
// the snapshot to complete and hint against.
func (b *Buffer) SetEvalCompletions(c []Completion) {
	b.completions = c
}

// SetText replaces the whole buffer (no undo entry). Used on open.
func (b *Buffer) SetText(s string) {
	parts := strings.Split(s, "\n")
	b.lines = make([][]rune, len(parts))
	for i, p := range parts {
		b.lines[i] = []rune(p)
	}
	if len(b.lines) == 0 {
		b.lines = [][]rune{{}}
	}
	b.clampCursor()
}

// Text joins the buffer back into a newline-separated string.
func (b *Buffer) Text() string {
	parts := make([]string, len(b.lines))
	for i, l := range b.lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

// Lines returns the buffer as plain strings (for tests and CurrentLine callers).
func (b *Buffer) Lines() []string {
	out := make([]string, len(b.lines))
	for i, l := range b.lines {
		out[i] = string(l)
	}
	return out
}

// Mode reports the current modal state.
func (b *Buffer) Mode() Mode { return b.mode }

// Width reports the configured content width (for a stable overlay frame width).
func (b *Buffer) Width() int { return b.width }

// Cursor reports the current cursor position.
func (b *Buffer) Cursor() Pos { return b.cursor }

// LineCount reports the number of lines in the buffer.
func (b *Buffer) LineCount() int { return len(b.lines) }

// Scroll moves the viewport by delta lines (mouse wheel), keeping the cursor
// within the visible range.
func (b *Buffer) Scroll(delta int) {
	if b.height <= 0 {
		return
	}
	b.top = min(max(b.top+delta, 0), b.maxTop())
	// Keep the cursor within the visible logical range.
	if b.cursor.Row < b.top {
		b.cursor.Row = b.top
	}
	if last := b.lastVisibleRow(); b.cursor.Row > last {
		b.cursor.Row = last
	}
	b.clampCursor()
}

// PendingCommand returns the in-progress (not-yet-complete) normal-mode command
// for a vim-style "showcmd" indicator — e.g. "2", "d", "f" (awaiting a target),
// "ci". Empty when nothing is pending.
func (b *Buffer) PendingCommand() string {
	var s []rune
	if b.pendingCount > 0 {
		s = append(s, []rune(strconv.Itoa(b.pendingCount))...)
	}
	if b.surroundOp != 0 {
		s = append(s, b.surroundOp, 's')
		if b.surroundOldChar != 0 {
			s = append(s, b.surroundOldChar)
		}
		return string(s)
	}
	if b.pendingOp != 0 {
		s = append(s, b.pendingOp)
	}
	if b.gPending {
		s = append(s, 'g')
	}
	if b.findPending != 0 {
		s = append(s, b.findPending)
	}
	if b.replacePending {
		s = append(s, 'r')
	}
	if b.textObjPending != 0 {
		s = append(s, b.textObjPending)
	}
	return string(s)
}

// CurrentLine returns the text of the line the cursor is on.
func (b *Buffer) CurrentLine() string {
	if b.cursor.Row < 0 || b.cursor.Row >= len(b.lines) {
		return ""
	}
	return string(b.lines[b.cursor.Row])
}

// CursorRow returns the 0-based logical row the cursor is on. Used to capture a
// line command's target row at dispatch so a slow/async result replaces that
// row, not whichever line the cursor later wandered to.
func (b *Buffer) CursorRow() int {
	return b.cursor.Row
}

// ReplaceCurrentLine swaps the cursor's line for s (which may contain newlines,
// splitting into several lines) as a single undo step, leaving the cursor at the
// end of the replacement. Mirrors the old textarea editor's contract so line
// commands keep working unchanged.
func (b *Buffer) ReplaceCurrentLine(s string) {
	row := b.cursor.Row
	if row < 0 || row >= len(b.lines) {
		return
	}
	b.ReplaceLineRange(row, row, s)
}

// ReplaceLineRange replaces lines [start,end] (inclusive, 0-based) with s
// (split on newlines) as a single undo step, leaving the cursor at the end of
// the replacement.
func (b *Buffer) ReplaceLineRange(start, end int, s string) {
	if start < 0 {
		start = 0
	}
	if end >= len(b.lines) {
		end = len(b.lines) - 1
	}
	if start > end {
		return
	}
	b.pushUndo()
	repl := strings.Split(s, "\n")
	rl := make([][]rune, len(repl))
	for i, r := range repl {
		rl[i] = []rune(r)
	}
	tail := append([][]rune{}, b.lines[end+1:]...)
	b.lines = append(b.lines[:start], append(rl, tail...)...)
	if len(b.lines) == 0 {
		b.lines = [][]rune{{}}
	}
	b.cursor.Row = start + len(rl) - 1
	b.clampCursor()
	b.cursor.Col = len(b.curLine())
	b.clampCursorInsert() // park at end of replacement
	b.desiredCol = b.cursor.Col
	b.scrollToCursor()
}

// --- undo/redo --------------------------------------------------------------

func (b *Buffer) snap() snapshot {
	cp := make([][]rune, len(b.lines))
	for i, l := range b.lines {
		cp[i] = append([]rune(nil), l...)
	}
	return snapshot{lines: cp, cursor: b.cursor}
}

func (b *Buffer) restore(s snapshot) {
	cp := make([][]rune, len(s.lines))
	for i, l := range s.lines {
		cp[i] = append([]rune(nil), l...)
	}
	b.lines = cp
	if len(b.lines) == 0 {
		b.lines = [][]rune{{}}
	}
	b.cursor = s.cursor
	b.clampCursor()
	b.scrollToCursor()
}

func (b *Buffer) pushUndo() {
	b.undo = append(b.undo, b.snap())
	if len(b.undo) > undoLimit {
		b.undo = b.undo[len(b.undo)-undoLimit:]
	}
	b.redo = b.redo[:0]
}

// begin opens an undo group: the first call in a group records a snapshot;
// subsequent calls within the same group are no-ops, so a multi-key command
// (and the insert session a change-operator opens) collapses to one undo step.
func (b *Buffer) begin() {
	if !b.grouping {
		b.pushUndo()
		b.grouping = true
	}
}

func (b *Buffer) endGroup() { b.grouping = false }

// Undo reverts the last grouped change.
func (b *Buffer) Undo() {
	if len(b.undo) == 0 {
		return
	}
	b.redo = append(b.redo, b.snap())
	s := b.undo[len(b.undo)-1]
	b.undo = b.undo[:len(b.undo)-1]
	b.restore(s)
	b.endGroup()
}

// Redo reapplies the last undone change.
func (b *Buffer) Redo() {
	if len(b.redo) == 0 {
		return
	}
	b.undo = append(b.undo, b.snap())
	s := b.redo[len(b.redo)-1]
	b.redo = b.redo[:len(b.redo)-1]
	b.restore(s)
}

// --- helpers ----------------------------------------------------------------

func (b *Buffer) curLine() []rune {
	if b.cursor.Row < 0 || b.cursor.Row >= len(b.lines) {
		return nil
	}
	return b.lines[b.cursor.Row]
}

func (b *Buffer) lineAt(row int) []rune {
	if row < 0 || row >= len(b.lines) {
		return nil
	}
	return b.lines[row]
}

// clampCursor keeps the cursor inside the buffer for Normal mode: the column may
// rest on the last rune but not past it (an empty line allows col 0).
func (b *Buffer) clampCursor() {
	if len(b.lines) == 0 {
		b.lines = [][]rune{{}}
	}
	if b.cursor.Row < 0 {
		b.cursor.Row = 0
	}
	if b.cursor.Row >= len(b.lines) {
		b.cursor.Row = len(b.lines) - 1
	}
	max := len(b.curLine())
	if b.mode != ModeInsert && max > 0 {
		max--
	}
	if b.cursor.Col < 0 {
		b.cursor.Col = 0
	}
	if b.cursor.Col > max {
		b.cursor.Col = max
	}
}

// clampCursorInsert keeps the cursor inside the buffer for Insert mode, where
// the column may rest one past the last rune (append position).
func (b *Buffer) clampCursorInsert() {
	if b.cursor.Row < 0 {
		b.cursor.Row = 0
	}
	if b.cursor.Row >= len(b.lines) {
		b.cursor.Row = len(b.lines) - 1
	}
	if b.cursor.Col < 0 {
		b.cursor.Col = 0
	}
	if max := len(b.curLine()); b.cursor.Col > max {
		b.cursor.Col = max
	}
}

// --- soft-wrap geometry -----------------------------------------------------
//
// Lines soft-wrap, so the viewport is measured in *visual* rows while `top` and
// the cursor stay *logical*. These helpers convert between the two.

// gutterWidth is the line-number gutter width (digits + a trailing space).
func (b *Buffer) gutterWidth() int {
	return max(len(strconv.Itoa(len(b.lines)))+1, 3)
}

// textWidth is the usable width for line text after the gutter and the
// always-reserved 1-column scrollbar lane.
func (b *Buffer) textWidth() int {
	w := b.width
	if w <= 0 {
		w = 80
	}
	return max(w-b.gutterWidth()-1, 1)
}

// lineRows is how many visual rows a logical line occupies when soft-wrapped.
func (b *Buffer) lineRows(row int) int {
	n := len(b.lineAt(row))
	if n == 0 {
		return 1
	}
	tw := b.textWidth()
	return (n + tw - 1) / tw
}

func (b *Buffer) scrollToCursor() {
	if b.height <= 0 {
		return
	}
	if b.cursor.Row < b.top {
		b.top = max(b.cursor.Row, 0)
		return
	}
	// Raise top until the cursor's wrapped segment fits within height visual rows.
	tw := b.textWidth()
	for b.top < b.cursor.Row {
		used := 0
		for r := b.top; r < b.cursor.Row; r++ {
			used += b.lineRows(r)
		}
		used += b.cursor.Col/tw + 1 // rows up to and including the cursor's segment
		if used <= b.height {
			break
		}
		b.top++
	}
	if b.top > b.cursor.Row {
		b.top = b.cursor.Row
	}
}

// maxTop is the topmost logical line whose visual rows — together with every
// line below it — all fit within the viewport height, i.e. the most-scrolled
// position that still shows the last line at the bottom. Computed by walking up
// from the last line and stopping before a line that would overflow the window.
//
// It must use "all remaining rows fit" (acc <= height) rather than "remaining
// rows reach the height" (acc >= height): a soft-wrapped line straddling the
// fold makes the accumulated count jump past height, and the >= rule would then
// return a top whose window ends before the last line — stranding it off-screen
// and clamping the scroll short of the bottom.
func (b *Buffer) maxTop() int {
	if b.height <= 0 {
		return 0
	}
	acc := 0
	top := len(b.lines) - 1 // at minimum the last line is shown alone
	for r := len(b.lines) - 1; r >= 0; r-- {
		rows := b.lineRows(r)
		if acc+rows > b.height {
			break
		}
		acc += rows
		top = r
	}
	return top
}

// lastVisibleRow is the last logical line at least partly visible from top.
func (b *Buffer) lastVisibleRow() int {
	used := 0
	for r := b.top; r < len(b.lines); r++ {
		used += b.lineRows(r)
		if used >= b.height {
			return r
		}
	}
	return len(b.lines) - 1
}

// HandleKey processes one key (a tea.KeyMsg.String() value) and returns any
// host-level Effect. It dispatches by mode.
func (b *Buffer) HandleKey(key string) Effect {
	if !b.replaying {
		// Begin recording a potential "." change at the start of a normal-mode
		// command, then capture every key (including the ensuing insert session)
		// until the command finishes.
		if b.rec == nil && b.mode == ModeNormal {
			b.rec = &change{}
			b.recMutated = false
		}
		if b.rec != nil {
			b.rec.seq = append(b.rec.seq, key)
		}
	}
	var eff Effect
	switch b.mode {
	case ModeInsert:
		eff = b.handleInsert(key)
	case ModeCommand:
		eff = b.handleCommand(key)
	case ModeVisual, ModeVisualLine:
		eff = b.handleVisual(key)
	default:
		eff = b.handleNormal(key)
	}
	if b.justYanked != "" {
		eff.Yank = b.justYanked
		b.justYanked = ""
	}
	return eff
}

// repeatLastChange replays the recorded key sequence of the last mutating
// command (the "." command). The replaying guard stops the replay from being
// recorded as a new change.
func (b *Buffer) repeatLastChange(count int) {
	if b.lastChange == nil || len(b.lastChange.seq) == 0 {
		return
	}
	seq := append([]string(nil), b.lastChange.seq...)
	if count < 1 {
		count = 1
	}
	b.replaying = true
	for range count {
		for _, k := range seq {
			b.HandleKey(k)
		}
	}
	b.replaying = false
}
