package model

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/vimbuf"
)

// wireEditorCompletions injects a provider of :lua autocomplete candidates
// (registered function names + usage) into the editor, read lazily from the
// script host each time a vim buffer opens.
func (b *Board) wireEditorCompletions() {
	b.editor.SetEvalCompletionsFunc(func() []vimbuf.Completion {
		if b.scripts == nil {
			return nil
		}
		src := b.scripts.EvalCompletions()
		out := make([]vimbuf.Completion, len(src))
		for i, c := range src {
			out[i] = vimbuf.Completion{Name: c.Name, Usage: c.Usage}
		}
		return out
	})
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
func (b *Board) handleEditorEval(msg editorEvalMsg) (tea.Model, tea.Cmd) {
	if b.scripts == nil {
		return b, b.editor.setStatus("scripting disabled")
	}
	ctx := b.buildEditorEvalCtx(msg.Range)
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
	return b, nil
}

// buildEditorEvalCtx assembles the `ctx` table for a :lua command: the standard
// board/column/file context (reusing buildFilesystemCtx) plus the operand —
// `line`/`text` for a single line, or `lines`/`text`/`range` for a row range.
func (b *Board) buildEditorEvalCtx(rng *evalRange) map[string]any {
	colIdx := b.editor.ColIndex
	ctx := map[string]any{}
	if colIdx >= 0 && colIdx < len(b.columns) {
		// Resolve by the editor's FileName, not the column's current selection: a
		// script/timer/hook may have moved selection while the editor stayed open,
		// and the ctx must bind to the card whose buffer is being edited.
		item := b.columns[colIdx].ItemByName(b.editor.FileName)
		ctx = b.buildFilesystemCtx(colIdx, item)
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
