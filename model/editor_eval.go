package model

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

type boardEditorEval struct {
	board *Board
}

func (b *Board) editorEval() boardEditorEval {
	return boardEditorEval{board: b}
}

// editorEvalMsg asks the board to evaluate a Lua expression typed in the editor's
// ":lua" command-line. Range nil means the operand is the current line; non-nil
// means replace that 0-based inclusive row range with the result.
type editorEvalMsg struct {
	Expr  string
	Range *evalRange
}

type evalRange struct {
	Start, End int
}

// handleEditorEval evaluates a ":lua" expression with the editor's operand and
// board/file context exposed as the Lua global `ctx`, then splices a string
// return back into the buffer (a nil/no-value return leaves the buffer as-is).
func (e boardEditorEval) handle(msg editorEvalMsg) (tea.Model, tea.Cmd) {
	b := e.board
	if b.scripts == nil {
		return b, b.editor.setStatus("scripting disabled")
	}
	ctx := e.ctx(msg.Range)
	out, ok, err := b.scripts.EvalWithContext(msg.Expr, ctx)
	if err != nil {
		return b, b.editor.setStatus("lua: " + err.Error())
	}
	if !ok {
		return b, nil
	}
	if msg.Range != nil {
		b.editor.ReplaceLineRange(msg.Range.Start, msg.Range.End, out)
	} else {
		b.editor.ReplaceCurrentLine(out)
	}
	if b.editor.IsScratchpad() {
		return b, b.scratchpadActions().save(b.editor.ScratchpadContent())
	}
	return b, nil
}

// buildEditorEvalCtx assembles the `ctx` table for a :lua command: the standard
// board/column/file context (reusing commandContext.filesystemCtx) plus the operand —
// `line`/`text` for a single line, or `lines`/`text`/`range` for a row range.
func (e boardEditorEval) ctx(rng *evalRange) map[string]any {
	b := e.board
	ctx := map[string]any{}
	target := b.editor.itemTarget()
	if target.FileName == "" {
		target.FileName = b.editor.FileName
	}
	if col, item, err := b.resolveDelayedItemRef(target); err == nil {
		colIdx := b.indexOfColumn(col)
		ctx = b.commandContext().filesystemCtx(colIdx, item)
	}
	if b.editor.buf == nil {
		return ctx
	}
	lines := b.editor.buf.Lines()
	if rng != nil {
		s, e := rng.Start, rng.End
		if s < 0 {
			s = 0
		}
		if e >= len(lines) {
			e = len(lines) - 1
		}
		if s > e {
			return ctx
		}
		sel := lines[s : e+1]
		arr := make([]any, len(sel))
		for i, l := range sel {
			arr[i] = l
		}
		ctx["lines"] = arr
		ctx["text"] = strings.Join(sel, "\n")
		ctx["range"] = map[string]any{"from": s + 1, "to": e + 1}
	} else {
		ln := b.editor.CurrentLine()
		ctx["line"] = ln
		ctx["text"] = ln
	}
	return ctx
}
