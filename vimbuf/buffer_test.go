package vimbuf

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"kbrd/theme"
)

// keys feeds a sequence of key strings through HandleKey. Multi-char tokens are
// written wrapped in <...> (e.g. "<esc>"); everything else is sent rune by rune.
func keys(b *Buffer, s string) {
	for _, k := range tokenize(s) {
		b.HandleKey(k)
	}
}

func tokenize(s string) []string {
	var out []string
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '<' {
			j := i + 1
			for j < len(runes) && runes[j] != '>' {
				j++
			}
			out = append(out, string(runes[i+1:j]))
			i = j
			continue
		}
		out = append(out, string(runes[i]))
	}
	return out
}

func mustText(t *testing.T, b *Buffer, want string) {
	t.Helper()
	if got := b.Text(); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestTextRoundTrip(t *testing.T) {
	b := New("alpha\nbravo\ncharlie")
	mustText(t, b, "alpha\nbravo\ncharlie")
	if got := b.Lines(); len(got) != 3 || got[1] != "bravo" {
		t.Fatalf("Lines = %v", got)
	}
}

func TestCurrentLineAndReplace(t *testing.T) {
	b := New("alpha\nbravo\ncharlie")
	keys(b, "jj") // to charlie
	if got := b.CurrentLine(); got != "charlie" {
		t.Fatalf("CurrentLine = %q", got)
	}
	b.ReplaceCurrentLine("CHARLIE")
	mustText(t, b, "alpha\nbravo\nCHARLIE")
}

func TestReplaceLineRange(t *testing.T) {
	b := New("a\nb\nc\nd")
	b.ReplaceLineRange(1, 2, "X\nY\nZ")
	mustText(t, b, "a\nX\nY\nZ\nd")
}

func TestInsertAndEscape(t *testing.T) {
	b := New("hello")
	keys(b, "A world<esc>")
	mustText(t, b, "hello world")
	if b.Mode() != ModeNormal {
		t.Fatalf("mode = %v, want Normal", b.Mode())
	}
}

func TestDeleteWord(t *testing.T) {
	b := New("foo bar baz")
	keys(b, "dw")
	mustText(t, b, "bar baz")
}

func TestChangeWord(t *testing.T) {
	b := New("foo bar")
	keys(b, "cwzap<esc>")
	mustText(t, b, "zap bar")
}

func TestDeleteLineAndPaste(t *testing.T) {
	b := New("one\ntwo\nthree")
	keys(b, "dd") // delete "one"
	mustText(t, b, "two\nthree")
	keys(b, "p") // paste below current line (two)
	mustText(t, b, "two\none\nthree")
}

func TestCountedMotionDelete(t *testing.T) {
	b := New("a\nb\nc\nd\ne")
	keys(b, "2dd")
	mustText(t, b, "c\nd\ne")
}

// A count typed before an insert command must not survive the insert session and
// leak into the next normal-mode command. We don't implement counted insert, so
// 2iX inserts a single X, and the following x deletes one char (not two).
func TestCountBeforeInsertDoesNotLeak(t *testing.T) {
	b := New("abcdef")
	keys(b, "2iX<esc>") // X inserted once at the start, count consumed by enterInsert
	mustText(t, b, "Xabcdef")
	keys(b, "x") // deletes a single char under the cursor, not two
	mustText(t, b, "abcdef")

	// Same guarantee for o (and the other insert-entering commands): 2o opens a
	// single line, and the leftover count must not turn the following dd into 2dd.
	b = New("l1\nl2\nl3\nl4")
	keys(b, "2oX<esc>") // opens one line below l1, count consumed
	mustText(t, b, "l1\nX\nl2\nl3\nl4")
	keys(b, "dd") // deletes only the new X line, not two lines
	mustText(t, b, "l1\nl2\nl3\nl4")
}

func TestTextObjectQuote(t *testing.T) {
	b := New(`say "hello" now`)
	keys(b, "ci\"bye<esc>")
	mustText(t, b, `say "bye" now`)
}

func TestUndoRedo(t *testing.T) {
	b := New("hello")
	keys(b, "x")
	mustText(t, b, "ello")
	b.Undo()
	mustText(t, b, "hello")
	b.Redo()
	mustText(t, b, "ello")
}

func TestRepeatLastChange(t *testing.T) {
	b := New("aaaa")
	keys(b, "x") // delete one a -> aaa
	keys(b, ".") // repeat -> aa
	keys(b, ".") // -> a
	mustText(t, b, "a")
}

func TestIndentOperator(t *testing.T) {
	b := New("one\ntwo")
	keys(b, ">>")
	mustText(t, b, "  one\ntwo")
}

func TestJoinLines(t *testing.T) {
	b := New("foo\nbar")
	keys(b, "J")
	mustText(t, b, "foo bar")
}

func TestCommandEffects(t *testing.T) {
	b := New("x")
	if eff := b.HandleKey(":"); eff != (Effect{}) {
		t.Fatalf("entering command returned %+v", eff)
	}
	keys(b, "wq")
	eff := b.HandleKey("enter")
	if !eff.Submit || !eff.Quit {
		t.Fatalf(":wq effect = %+v, want Submit+Quit", eff)
	}
}

func TestLuaEvalEffect(t *testing.T) {
	b := New("hello")
	b.HandleKey(":")
	keys(b, "lua up(line)")
	eff := b.HandleKey("enter")
	if eff.EvalExpr != "up(line)" {
		t.Fatalf("EvalExpr = %q", eff.EvalExpr)
	}
	if eff.EvalRange != nil {
		t.Fatalf("EvalRange should be nil without a visual range")
	}
}

func TestLuaRangeEffect(t *testing.T) {
	b := New("a\nb\nc")
	keys(b, "V") // linewise visual on row 0
	keys(b, "j") // extend to row 1
	b.HandleKey(":")
	// cmdline pre-seeded with '<,'>
	keys(b, "lua bullets()")
	eff := b.HandleKey("enter")
	if eff.EvalRange == nil || eff.EvalRange.Start != 0 || eff.EvalRange.End != 1 {
		t.Fatalf("EvalRange = %+v, want {0,1}", eff.EvalRange)
	}
	if eff.EvalExpr != "bullets()" {
		t.Fatalf("EvalExpr = %q", eff.EvalExpr)
	}
}

func TestCompletion(t *testing.T) {
	b := New("x")
	b.SetEvalCompletions([]Completion{{Name: "indent", Usage: "indent(n)"}, {Name: "bullets"}})
	b.HandleKey(":")
	keys(b, "lua ind")
	b.HandleKey("tab")
	if got := b.CommandLine(); got != "lua indent" {
		t.Fatalf("after tab, cmdline = %q, want %q", got, "lua indent")
	}
	if name, usage := b.CompletionHint(); name != "indent" || usage != "indent(n)" {
		t.Fatalf("hint = %q/%q", name, usage)
	}
}

// The linewise selection stays highlighted while a range ":" command is typed.
func TestSelectionVisibleInCommand(t *testing.T) {
	b := New("a\nb\nc")
	keys(b, "Vj") // linewise visual over rows 0-1
	b.HandleKey(":")
	if b.Mode() != ModeCommand {
		t.Fatalf("mode = %v, want Command", b.Mode())
	}
	if _, _, ok := b.selectionForRow(0); !ok {
		t.Fatalf("row 0 should stay selected during command mode")
	}
	if _, _, ok := b.selectionForRow(1); !ok {
		t.Fatalf("row 1 should stay selected during command mode")
	}
	if _, _, ok := b.selectionForRow(2); ok {
		t.Fatalf("row 2 should not be selected")
	}
}

func TestFindCharMotion(t *testing.T) {
	b := New("alpha bravo charlie")
	keys(b, "fb") // jump to first 'b' (bravo)
	if got := b.Cursor().Col; got != 6 {
		t.Fatalf("fb cursor col = %d, want 6", got)
	}
	keys(b, ";") // repeat -> next 'b'? there is none after; stays
	// 'b' of bravo at 6; next 'b' none -> stays at 6
	if got := b.Cursor().Col; got != 6 {
		t.Fatalf("after ; col = %d, want 6", got)
	}
}

func TestTillCharAndComma(t *testing.T) {
	b := New("a.b.c.d")
	keys(b, "t.") // till first '.', land before it -> col 0
	if got := b.Cursor().Col; got != 0 {
		t.Fatalf("t. col = %d, want 0", got)
	}
	keys(b, ";") // repeat till next '.' -> before second '.', col 2
	if got := b.Cursor().Col; got != 2 {
		t.Fatalf("after ; col = %d, want 2", got)
	}
}

func TestDeleteToFindChar(t *testing.T) {
	b := New("hello world")
	keys(b, "df ") // delete from start through the space
	mustText(t, b, "world")
}

func TestGgMotion(t *testing.T) {
	b := New("one\ntwo\nthree")
	keys(b, "G") // last line
	if got := b.Cursor().Row; got != 2 {
		t.Fatalf("G row = %d, want 2", got)
	}
	keys(b, "gg") // first line
	if got := b.Cursor().Row; got != 0 {
		t.Fatalf("gg row = %d, want 0", got)
	}
}

func TestCountedGMotion(t *testing.T) {
	b := New("one\ntwo\nthree\nfour\nfive")
	keys(b, "3G")
	if got := b.Cursor().Row; got != 2 {
		t.Fatalf("3G row = %d, want 2", got)
	}
}

func TestDeleteToCountedG(t *testing.T) {
	b := New("one\ntwo\nthree\nfour\nfive")
	keys(b, "d3G")
	mustText(t, b, "four\nfive")
}

func TestWordMotions(t *testing.T) {
	b := New("foo bar baz")
	keys(b, "w")
	if got := b.Cursor().Col; got != 4 {
		t.Fatalf("w col = %d, want 4", got)
	}
	keys(b, "e")
	if got := b.Cursor().Col; got != 6 {
		t.Fatalf("e col = %d, want 6 (end of bar)", got)
	}
	keys(b, "b")
	if got := b.Cursor().Col; got != 4 {
		t.Fatalf("b col = %d, want 4", got)
	}
}

func TestGotoLineNumberCommand(t *testing.T) {
	b := New("one\ntwo\nthree\nfour\nfive")
	b.HandleKey(":")
	keys(b, "4")
	b.HandleKey("enter")
	if got := b.Cursor().Row; got != 3 {
		t.Fatalf(":4 row = %d, want 3", got)
	}
	if b.Mode() != ModeNormal {
		t.Fatalf("mode after :4 = %v, want Normal", b.Mode())
	}
}

func TestNumericRangeSelect(t *testing.T) {
	b := New("a\nb\nc\nd\ne")
	b.HandleKey(":")
	keys(b, "2,4")
	b.HandleKey("enter")
	if b.Mode() != ModeVisualLine {
		t.Fatalf(":2,4 mode = %v, want VisualLine", b.Mode())
	}
	// anchor row 1, cursor row 3 (0-based)
	if _, _, ok := b.selectionForRow(1); !ok {
		t.Fatalf("row 1 should be selected")
	}
	if _, _, ok := b.selectionForRow(3); !ok {
		t.Fatalf("row 3 should be selected")
	}
	if _, _, ok := b.selectionForRow(4); ok {
		t.Fatalf("row 4 should not be selected")
	}
}

func TestNumericRangeLua(t *testing.T) {
	b := New("a\nb\nc\nd")
	b.HandleKey(":")
	keys(b, "1,3lua up()")
	eff := b.HandleKey("enter")
	if eff.EvalRange == nil || eff.EvalRange.Start != 0 || eff.EvalRange.End != 2 {
		t.Fatalf("EvalRange = %+v, want {0,2}", eff.EvalRange)
	}
	if eff.EvalExpr != "up()" {
		t.Fatalf("EvalExpr = %q", eff.EvalExpr)
	}
}

func TestNumericAddressSubstitute(t *testing.T) {
	b := New("foo\nfoo\nfoo")
	b.HandleKey(":")
	keys(b, "2s/foo/bar/")
	b.HandleKey("enter")
	mustText(t, b, "foo\nbar\nfoo")
}

func TestNumericAddressLua(t *testing.T) {
	b := New("a\nb\nc")
	b.HandleKey(":")
	keys(b, "2lua up()")
	eff := b.HandleKey("enter")
	if eff.EvalRange == nil || eff.EvalRange.Start != 1 || eff.EvalRange.End != 1 {
		t.Fatalf("EvalRange = %+v, want {1,1}", eff.EvalRange)
	}
	if eff.EvalExpr != "up()" {
		t.Fatalf("EvalExpr = %q", eff.EvalExpr)
	}
}

func TestIncrementDecrement(t *testing.T) {
	b := New("count: 41")
	keys(b, "$") // on last char
	b.HandleKey("ctrl+a")
	mustText(t, b, "count: 42")
	b.HandleKey("ctrl+x")
	b.HandleKey("ctrl+x")
	mustText(t, b, "count: 40")
}

func TestIncrementFromBefore(t *testing.T) {
	b := New("x = 9 end") // cursor at col 0; ctrl+a finds the 9
	b.HandleKey("ctrl+a")
	mustText(t, b, "x = 10 end")
}

func TestToggleCheckbox(t *testing.T) {
	b := New("- [ ] task")
	b.HandleKey("tab")
	mustText(t, b, "- [x] task")
	b.HandleKey("tab")
	mustText(t, b, "- [ ] task")
}

func TestInsertTaskPrefixAtCursor(t *testing.T) {
	b := New("task")
	b.HandleKey("ctrl+t")
	mustText(t, b, "- [ ] task")

	b = New("task")
	b.HandleKey("i")
	b.HandleKey("ctrl+t")
	mustText(t, b, "- [ ] task")
	if b.Mode() != ModeInsert {
		t.Fatalf("mode = %v, want insert", b.Mode())
	}
}

func TestInsertTaskPrefixVisualRange(t *testing.T) {
	b := New("alpha\n  bravo\ncharlie")
	keys(b, "vj")
	b.HandleKey("ctrl+t")

	mustText(t, b, "- [ ] alpha\n  - [ ] bravo\ncharlie")
	if b.Mode() != ModeNormal {
		t.Fatalf("mode = %v, want normal", b.Mode())
	}
}

func TestInsertTaskPrefixVisualLineSkipsExistingTasks(t *testing.T) {
	b := New("plain\n  indented\n- [ ] done\n  - [x] checked\n- item")
	keys(b, "Vjjjj")
	b.HandleKey("ctrl+t")

	mustText(t, b, "- [ ] plain\n  - [ ] indented\n- [ ] done\n  - [x] checked\n- [ ] - item")
}

func TestInsertTaskPrefixVisualUndoOneStep(t *testing.T) {
	original := "alpha\nbravo\ncharlie"
	b := New(original)
	keys(b, "Vj")
	b.HandleKey("ctrl+t")
	mustText(t, b, "- [ ] alpha\n- [ ] bravo\ncharlie")

	b.HandleKey("u")
	mustText(t, b, original)
}

func TestSubstituteCurrentLine(t *testing.T) {
	b := New("foo foo foo")
	b.HandleKey(":")
	keys(b, "s/foo/bar/")
	b.HandleKey("enter")
	mustText(t, b, "bar foo foo") // no g flag: first only
}

func TestSubstituteGlobalRange(t *testing.T) {
	b := New("a x a\nb x b\nc")
	b.HandleKey(":")
	keys(b, "%s/x/Y/g")
	b.HandleKey("enter")
	mustText(t, b, "a Y a\nb Y b\nc")
}

func TestSubstituteGroups(t *testing.T) {
	b := New("key: value")
	b.HandleKey(":")
	keys(b, `s/(\w+): (\w+)/\2=\1/`)
	b.HandleKey("enter")
	mustText(t, b, "value=key")
}

func TestSubstituteAmpersandRepeat(t *testing.T) {
	b := New("foo\nfoo")
	b.HandleKey(":")
	keys(b, "s/foo/bar/")
	b.HandleKey("enter") // line 0 -> bar
	keys(b, "j")         // line 1
	b.HandleKey("&")     // repeat on line 1
	mustText(t, b, "bar\nbar")
}

// InsertText (clipboard/bracketed paste) must close its own undo group: a paste
// followed by a separate normal-mode edit has to be two undo steps, not one. If
// the group leaked open, the edit would fold into the paste's snapshot and a
// single u would revert both.
func TestInsertTextClosesUndoGroup(t *testing.T) {
	b := New("abc") // cursor at {0,0}
	b.InsertText("X")
	mustText(t, b, "Xabc")
	b.HandleKey("x") // delete the char under the cursor ('a') -> "Xbc"
	mustText(t, b, "Xbc")

	b.HandleKey("u") // reverts only the x
	mustText(t, b, "Xabc")
	b.HandleKey("u") // reverts the paste
	mustText(t, b, "abc")
}

// A :s that matches nothing must not consume an undo step: after a no-match
// substitute, a single u has to revert the prior real edit, not an identical
// snapshot the failed :s pushed.
func TestSubstituteNoMatchKeepsUndo(t *testing.T) {
	b := New("hello world")
	keys(b, "x") // real edit: delete 'h' -> "ello world"
	mustText(t, b, "ello world")

	b.HandleKey(":")
	keys(b, "s/zzz/q/") // no match -> "pattern not found", no buffer change
	b.HandleKey("enter")
	mustText(t, b, "ello world")

	b.HandleKey("u") // a single undo must restore the original
	mustText(t, b, "hello world")
}

func TestAutoContinueBullet(t *testing.T) {
	b := New("- one")
	keys(b, "A")         // append at end (insert)
	b.HandleKey("enter") // continue list
	keys(b, "two")
	b.HandleKey("esc")
	mustText(t, b, "- one\n- two")
}

func TestAutoContinueCheckbox(t *testing.T) {
	b := New("- [x] done")
	keys(b, "A")
	b.HandleKey("enter")
	keys(b, "next")
	b.HandleKey("esc")
	mustText(t, b, "- [x] done\n- [ ] next")
}

func TestAutoContinueOrdered(t *testing.T) {
	b := New("1. first")
	keys(b, "A")
	b.HandleKey("enter")
	keys(b, "second")
	b.HandleKey("esc")
	mustText(t, b, "1. first\n2. second")
}

func TestAutoContinueEmptyEndsList(t *testing.T) {
	b := New("- one")
	keys(b, "A")
	b.HandleKey("enter") // -> "- two" start? no, continues to "- "
	b.HandleKey("enter") // empty marker -> clear
	b.HandleKey("esc")
	mustText(t, b, "- one\n")
}

func TestOpenContinuesList(t *testing.T) {
	b := New("- one")
	keys(b, "o")
	keys(b, "two")
	b.HandleKey("esc")
	mustText(t, b, "- one\n- two")
}

func TestVisualSurround(t *testing.T) {
	b := New("hello world")
	keys(b, "viw") // visual inner word -> "hello"? viw selects word under cursor
	keys(b, "S*")  // wrap in *
	mustText(t, b, "*hello* world")
}

func TestDeleteSurround(t *testing.T) {
	b := New(`say "hi" now`)
	keys(b, "f\"") // move onto opening quote
	keys(b, `ds"`)
	mustText(t, b, "say hi now")
}

func TestChangeSurround(t *testing.T) {
	b := New("(item)")
	keys(b, `cs)]`) // change ( ) to [ ]
	mustText(t, b, "[item]")
}

func TestYankEmitsEffect(t *testing.T) {
	b := New("hello\nworld")
	b.HandleKey("y")        // operator pending
	eff := b.HandleKey("y") // yy -> yank line
	if eff.Yank != "hello\n" {
		t.Fatalf("yy Yank = %q, want %q", eff.Yank, "hello\n")
	}
	b2 := New("foo bar")
	b2.HandleKey("y")
	eff2 := b2.HandleKey("w") // yw
	if eff2.Yank != "foo " {
		t.Fatalf("yw Yank = %q, want %q", eff2.Yank, "foo ")
	}
}

func TestInsertText(t *testing.T) {
	b := New("ab")
	keys(b, "l")       // cursor on 'b'
	b.InsertText("XY") // insert before 'b'
	mustText(t, b, "aXYb")
}

func TestScrollThumbBounds(t *testing.T) {
	b := New("")
	// 100 lines, viewport 10
	b.lines = make([][]rune, 100)
	for i := range b.lines {
		b.lines[i] = []rune("x")
	}
	b.SetSize(40, 10)
	b.top = 0
	s, e := b.visualThumb(10, b.totalVisualRows())
	if s != 0 {
		t.Fatalf("top=0 thumb start = %d, want 0", s)
	}
	b.top = 90 // maxTop
	s2, e2 := b.visualThumb(10, b.totalVisualRows())
	if e2 != 10 {
		t.Fatalf("bottom thumb end = %d, want 10", e2)
	}
	if s2 < 0 || e2 > 10 || s2 >= e2 {
		t.Fatalf("thumb out of bounds: [%d,%d)", s2, e2)
	}
	_ = e
}

func TestViewNoOverflow(t *testing.T) {
	long := "this is a very long line that exceeds the viewport width by a lot indeed yes and more"
	b := New(long + "\n" + long + "\n" + long + "\nshort")
	b.SetSize(40, 30)
	for _, ln := range strings.Split(b.View(theme.Palette{}), "\n") {
		if w := lipgloss.Width(ln); w > 40 {
			t.Fatalf("rendered line width %d exceeds buffer width 40: %q", w, ln)
		}
	}
}

func TestScrollViewport(t *testing.T) {
	b := New("")
	b.lines = make([][]rune, 50)
	for i := range b.lines {
		b.lines[i] = []rune("x")
	}
	b.cursor = Pos{0, 0}
	b.SetSize(40, 10)
	b.Scroll(5)
	if b.top != 5 {
		t.Fatalf("top after Scroll(5) = %d, want 5", b.top)
	}
	if b.cursor.Row < b.top {
		t.Fatalf("cursor row %d should be kept >= top %d", b.cursor.Row, b.top)
	}
	b.Scroll(1000) // clamp to maxTop
	if b.top != 40 {
		t.Fatalf("top clamped = %d, want 40 (50-10)", b.top)
	}
	b.Scroll(-1000)
	if b.top != 0 {
		t.Fatalf("top after scroll up = %d, want 0", b.top)
	}
}

func TestSoftWrap(t *testing.T) {
	long := strings.Repeat("w", 100)
	b := New(long + "\nshort")
	b.SetSize(40, 12) // gutterW≈3, textW≈36 -> 100 chars = 3 visual rows
	view := b.View(theme.Palette{})
	lines := strings.Split(view, "\n")
	if len(lines) != 12 {
		t.Fatalf("rendered %d visual rows, want 12 (height)", len(lines))
	}
	for _, ln := range lines {
		if w := lipgloss.Width(ln); w > 40 {
			t.Fatalf("visual row width %d > 40 (should wrap, not overflow): %q", w, ln)
		}
	}
	// the long line occupies rows 0..2; "short" appears on row 3 with gutter "2"
	if !strings.Contains(lines[3], "2") || !strings.Contains(lines[3], "short") {
		t.Fatalf("row 3 should be line 2 'short', got %q", lines[3])
	}
}
