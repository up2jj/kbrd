package model

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/config"
	"kbrd/shellcmd"
)

// runLineCommandMsg dispatches a line command picked from the in-editor menu.
// Line is the current editor line (handed to the command as ctx.line and as the
// {{.line}} template var / stdin); the command's return value replaces it.
type runLineCommandMsg struct {
	Cmd  config.Command
	Line string
	Vars map[string]string
}

// lineShellDoneMsg carries the captured stdout of a shell line filter back so
// the result can be spliced into the editor on the Bubble Tea goroutine.
type lineShellDoneMsg struct {
	Name string
	Out  string
	Exit int
	Err  string
}

// lineCommandDefaultTimeout bounds a shell line filter when no scripting timeout
// is configured (shell line commands work even with scripting disabled).
const lineCommandDefaultTimeout = 2 * time.Second

// openLineCommands opens the custom-command menu filtered to line commands,
// layered over the still-open editor. The board keeps the routing/wiring thin;
// dispatch and the editor splice live here.
func (b *Board) openLineCommands(msg openLineCommandsMsg) tea.Cmd {
	b.loadCommands()
	cmds := make([]config.Command, 0, len(b.commands))
	for _, c := range b.commands {
		if c.IsLine() {
			cmds = append(cmds, c)
		}
	}
	b.customCmds.OpenLine(cmds, b.commandWarnings, msg.Line, b.lineCommandVars(msg))
	return nil
}

// lineCommandVars builds the template/ctx vars for a line command: the standard
// board/column/file context (when the edited item is resolvable) plus `line`.
func (b *Board) lineCommandVars(msg openLineCommandsMsg) map[string]string {
	vars := map[string]string{}
	if msg.ColIndex >= 0 && msg.ColIndex < len(b.columns) {
		col := b.columns[msg.ColIndex]
		var item *Item
		if col.HasSelectedItem() {
			item = col.SelectedItem()
		}
		vars = b.buildCommandVars(msg.ColIndex, item)
	}
	vars["line"] = msg.Line
	return vars
}

// handleRunLineCommand dispatches a chosen line command. Lua commands run on the
// script host (their return value is spliced in once the coroutine completes,
// via the handleScriptResult chokepoint); shell commands run non-interactively
// with the line on stdin and their stdout replacing the line.
func (b *Board) handleRunLineCommand(msg runLineCommandMsg) (tea.Model, tea.Cmd) {
	if msg.Cmd.Source == config.SourceLua {
		// Mark the apply intent so handleScriptResult splices TakeReturn() into
		// the editor when the coroutine finishes (even after kbrd.ui.* yields).
		b.lineApplyPending = true
		req, err := b.scripts.RunCommand(msg.Cmd.LuaRef, msg.Vars)
		return b, b.handleScriptResult(msg.Cmd.Name, req, err)
	}

	rendered, err := msg.Cmd.Render(msg.Vars)
	if err != nil {
		return b, b.notifier.Send("template error: "+err.Error(), notifyError)
	}
	dir := b.cfg.Path
	line := msg.Line
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
		return lineShellDoneMsg{Name: name, Out: res.Output, Exit: res.ExitCode, Err: errStr}
	}
}

// applyLineReturn splices a completed Lua line command's return value into the
// editor's current line. A return of nil/none leaves the line untouched. No
// finished toast or column reload — the buffer change is its own feedback, and
// nothing on disk changed (unlike board commands).
func (b *Board) applyLineReturn() tea.Cmd {
	if out, ok := b.scripts.TakeReturn(); ok {
		b.editor.ReplaceCurrentLine(out)
	}
	return nil
}

// handleLineShellDone applies a shell line filter's stdout to the editor line.
// A failed run (start/timeout error or non-zero exit) leaves the line as-is and
// surfaces the captured stderr instead.
func (b *Board) handleLineShellDone(msg lineShellDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err != "" {
		return b, b.notifier.Send(msg.Name+": "+msg.Err, notifyError)
	}
	if msg.Exit != 0 {
		detail := strings.TrimSpace(msg.Out)
		if detail == "" {
			detail = "exited with a non-zero status"
		}
		return b, b.notifier.Send(msg.Name+": "+detail, notifyError)
	}
	b.editor.ReplaceCurrentLine(trimOneTrailingNewline(msg.Out))
	return b, nil
}

// trimOneTrailingNewline drops a single trailing line ending ("\n" or "\r\n"),
// which shells almost always append — without flattening intentional blank
// lines a multi-line filter may have produced.
func trimOneTrailingNewline(s string) string {
	s = strings.TrimSuffix(s, "\n")
	return strings.TrimSuffix(s, "\r")
}
