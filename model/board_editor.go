package model

import (
	"kbrd/script"
	"kbrd/vimbuf"
)

func newBoardEditor(vim bool, palette Palette, termWidth, termHeight int, evalCompletionHost **script.Host) *Editor {
	editor := NewEditor(vim)
	configureBoardEditor(editor, palette, termWidth, termHeight, evalCompletionHost)
	return editor
}

func configureBoardEditor(editor *Editor, palette Palette, termWidth, termHeight int, evalCompletionHost **script.Host) {
	editor.palette = palette
	editor.SetSize(termWidth, termHeight)
	editor.SetEvalCompletionsFunc(evalCompletionsFromHost(evalCompletionHost))
}

func evalCompletionsFromHost(host **script.Host) func() []vimbuf.Completion {
	return func() []vimbuf.Completion {
		if host == nil || *host == nil {
			return nil
		}
		src := (*host).EvalCompletions()
		out := make([]vimbuf.Completion, len(src))
		for i, c := range src {
			out[i] = vimbuf.Completion{Name: c.Name, Usage: c.Usage}
		}
		return out
	}
}

// resetEditor replaces the editor with a fresh instance, re-seeding it with the
// current palette and terminal size. The size matters because applySize() falls
// back to a fixed default when termWidth/termHeight are 0, which would otherwise
// make expand/collapse (ctrl+e) a no-op until the next terminal resize.
func (b *Board) resetEditor() {
	b.editor = newBoardEditor(b.cfg.Editor.Vim, b.palette, b.termWidth, b.termHeight, &b.scripts)
}
