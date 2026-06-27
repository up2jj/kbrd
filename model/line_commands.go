package model

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"kbrd/config"
	"kbrd/shellcmd"
)

// runLineCommandMsg dispatches a line command picked from the in-editor menu.
// Line is the current editor line (handed to the command as ctx.line and as the
// {{.line}} template var / stdin); the command's return value replaces it. Row
// is the line's 0-based row at dispatch, so the result lands on that row even if
// the cursor has since moved (the menu closes before an async command finishes).
type runLineCommandMsg struct {
	Cmd  config.Command
	Line string
	Row  int
	Vars map[string]string
}

// lineShellDoneMsg carries the captured stdout of a shell line filter back so
// the result can be spliced into the editor on the Bubble Tea goroutine. Row is
// the dispatch-time target row (see runLineCommandMsg).
type lineShellDoneMsg struct {
	Name string
	Out  string
	Row  int
	Exit int
	Err  string
}

// lineCommandDefaultTimeout bounds a shell line filter when no scripting timeout
// is configured (shell line commands work even with scripting disabled).
const lineCommandDefaultTimeout = 2 * time.Second

type boardLineCommands struct {
	board *Board
}

func (b *Board) lineCommands() boardLineCommands {
	return boardLineCommands{board: b}
}

// open opens the custom-command menu filtered to line commands, layered over
// the still-open editor. The board keeps the routing/wiring thin; dispatch and
// the editor splice live here.
func (l boardLineCommands) open(msg openLineCommandsMsg) tea.Cmd {
	b := l.board
	b.loadCommands()
	cmds := make([]config.Command, 0, len(b.commands))
	for _, c := range b.commands {
		if c.IsLine() {
			cmds = append(cmds, c)
		}
	}
	b.customCmds.OpenLine(cmds, b.commandWarnings, msg.Line, msg.Row, l.vars(msg))
	return nil
}

// vars builds the template/ctx vars for a line command: the standard
// board/column/file context (when the edited item is resolvable) plus `line`.
// The item is resolved by the editor's FileName, not the column's current
// selection — a script/timer/hook may have moved selection while the editor
// stayed open, and the command must bind to the card actually being edited.
func (l boardLineCommands) vars(msg openLineCommandsMsg) map[string]string {
	b := l.board
	vars := map[string]string{}
	target := msg.Target
	if target.FileName == "" {
		target.FileName = msg.FileName
	}
	if col, item, err := b.resolveDelayedItemRef(target); err == nil {
		if colIdx := b.indexOfColumn(col); colIdx >= 0 {
			vars = b.commandContext().vars(colIdx, item)
		}
	}
	vars["line"] = msg.Line
	return vars
}

// handleRun dispatches a chosen line command. Lua commands run on the
// script host (their return value is spliced in once the coroutine completes,
// via the handleScriptResult chokepoint); shell commands run non-interactively
// with the line on stdin and their stdout replacing the line.
func (l boardLineCommands) handleRun(msg runLineCommandMsg) (tea.Model, tea.Cmd) {
	b := l.board
	if msg.Cmd.Source == config.SourceLua {
		// Mark the apply intent (and the target row) so handleScriptResult splices
		// TakeReturn() into that row when the coroutine finishes — even after
		// kbrd.ui.* yields, by which point the cursor may have moved.
		b.lineApplyPending = true
		b.lineApplyRow = msg.Row
		req, err := b.scripts.RunCommand(msg.Cmd.LuaRef, msg.Vars)
		return b, b.handleScriptResult(msg.Cmd.Name, req, err)
	}

	rendered, err := msg.Cmd.Render(msg.Vars)
	if err != nil {
		return b, b.notifier.ErrorCause("template error", err)
	}
	dir := b.cfg.Path
	line := msg.Line
	row := msg.Row
	name := msg.Cmd.Name
	timeout := time.Duration(b.cfg.Scripting.CommandTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = lineCommandDefaultTimeout
	}
	return b, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		res, err := shellcmd.RunStdinStdout(ctx, dir, rendered, line)
		errStr := ""
		if err != nil {
			errStr = err.Error()
		}
		return lineShellDoneMsg{Name: name, Out: res.Output, Row: row, Exit: res.ExitCode, Err: errStr}
	}
}

// applyReturn splices a completed Lua line command's return value into the
// editor's current line. A return of nil/none leaves the line untouched. No
// finished toast or column reload — the buffer change is its own feedback, and
// nothing on disk changed (unlike board commands).
func (l boardLineCommands) applyReturn() tea.Cmd {
	b := l.board
	if out, ok := b.scripts.TakeReturn(); ok {
		b.editor.ReplaceLine(b.lineApplyRow, out)
	}
	return nil
}

// handleShellDone applies a shell line filter's stdout to the editor line.
// A failed run (start/timeout error or non-zero exit) leaves the line as-is and
// surfaces the captured stderr instead.
func (l boardLineCommands) handleShellDone(msg lineShellDoneMsg) (tea.Model, tea.Cmd) {
	b := l.board
	if msg.Err != "" {
		return b, b.notifier.Error(msg.Name + ": " + msg.Err)
	}
	if msg.Exit != 0 {
		detail := strings.TrimSpace(msg.Out)
		if detail == "" {
			detail = "exited with a non-zero status"
		}
		return b, b.notifier.Error(msg.Name + ": " + detail)
	}
	b.editor.ReplaceLine(msg.Row, trimOneTrailingNewline(msg.Out))
	return b, nil
}

// trimOneTrailingNewline drops a single trailing line ending ("\n" or "\r\n"),
// which shells almost always append — without flattening intentional blank
// lines a multi-line filter may have produced.
func trimOneTrailingNewline(s string) string {
	s = strings.TrimSuffix(s, "\n")
	return strings.TrimSuffix(s, "\r")
}
