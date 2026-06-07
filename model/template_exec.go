package model

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/board"
	"kbrd/config"
	"kbrd/shellcmd"
	"kbrd/template"
)

// templateExec runs the {{shell}} directives a card template emits. The card is
// created immediately with pending markers; a background worker per marker runs
// the command and, on completion, replaces the marker in the file with the
// output (the fsnotify watcher then refreshes the board). Keeping this off the
// critical path is the whole point — an LLM command can take many seconds.
//
// All orchestration lives here so model/board.go stays thin: it holds a
// templateExec field and delegates (dispatch / done / recover / Inflight).
// State is touched only on the Bubble Tea goroutine (dispatch from a key
// handler, done from a message handler), so it needs no locking — the same
// contract as the rest of the model. Command execution happens in tea.Cmd
// goroutines; the resulting file write happens back on the UI goroutine in
// done, so concurrent jobs on the same file never race.
type templateExec struct {
	inflight int
	nextID   int // session-monotonic id assigned at dispatch (see plan: collision analysis)
	notifier *Notifier
}

// templateShellDoneMsg carries one finished command back to the Board.
type templateShellDoneMsg struct {
	CardPath string
	ID       int
	Output   string
	Err      string // non-empty includes shellcmd.ErrTimeout
	ExitCode int
}

const (
	templateExecDisabledNote = "> _template shell exec disabled — set `[template] exec = true` (or drop `--safe`)_"
	templateInterruptedNote  = "> ⚠ _generation interrupted_"
)

// dispatch rewrites the freshly rendered body's shell markers and, when exec is
// enabled, returns a tea.Cmd that runs each command. cardPath is the absolute
// path the card will be written to; dir is the command working directory.
//
// Exec disabled: each marker becomes an inert note, no commands run.
// Exec enabled: each marker is reassigned a session-unique id (so it can never
// collide with another card's markers if content is ever merged), a worker is
// spawned per marker, and the rewritten body still carries the markers for the
// worker to replace on completion.
func (te *templateExec) dispatch(cardPath, body, dir string, cfg config.TemplateConfig) (string, tea.Cmd) {
	markers := template.ParseShellMarkers(body)
	if len(markers) == 0 {
		return body, nil
	}

	var b strings.Builder
	last := 0
	var cmds []tea.Cmd
	timeout := time.Duration(cfg.CommandTimeoutMs) * time.Millisecond
	for _, m := range markers {
		b.WriteString(body[last:m.Span[0]])
		last = m.Span[1]

		if !cfg.Exec {
			b.WriteString(templateExecDisabledNote)
			continue
		}
		te.nextID++
		id := te.nextID
		b.WriteString(template.RenderShellMarker(id, m.Cmd, m.Stdin))
		cmd, stdin := m.Cmd, m.Stdin
		cmds = append(cmds, func() tea.Msg {
			ctx := context.Background()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}
			res, err := shellcmd.RunStdin(ctx, dir, cmd, stdin)
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}
			return templateShellDoneMsg{
				CardPath: cardPath, ID: id,
				Output: res.Output, Err: errStr, ExitCode: res.ExitCode,
			}
		})
	}
	b.WriteString(body[last:])

	if len(cmds) == 0 {
		return b.String(), nil
	}
	te.inflight += len(cmds)
	return b.String(), tea.Batch(cmds...)
}

// done replaces the finished command's marker in its card file with the output
// (or an error note). Runs on the UI goroutine, so writes are serialized even
// for concurrent jobs on the same file.
func (te *templateExec) done(msg templateShellDoneMsg) tea.Cmd {
	if te.inflight > 0 {
		te.inflight--
	}
	data, err := os.ReadFile(msg.CardPath)
	if err != nil {
		// Card deleted/renamed while the command ran — drop the output.
		return te.notifier.Send("template: card gone, discarded output of "+filepath.Base(msg.CardPath), notifyError)
	}
	body := string(data)
	newBody := template.RewriteShellMarker(body, msg.ID, shellResultNote(msg))
	if newBody == body {
		// Marker edited away by the user — nothing to fill.
		return te.notifier.Send("template: section removed, discarded command output", notifyError)
	}
	// Existing-only write: the card may vanish between the read above and
	// this write; never resurrect it.
	if err := board.ReplaceFileContent(msg.CardPath, newBody); err != nil {
		return te.notifier.Send("template: failed to write result: "+err.Error(), notifyError)
	}
	return nil // the fsnotify watcher picks up the write and refreshes
}

// shellResultNote formats one command's outcome for insertion into the card.
func shellResultNote(msg templateShellDoneMsg) string {
	out := strings.TrimRight(msg.Output, "\n")
	if msg.Err != "" {
		note := "> ⚠ _command failed: " + msg.Err + "_"
		if out != "" {
			note += "\n\n" + out
		}
		return note
	}
	if msg.ExitCode != 0 {
		note := "> ⚠ _command exited " + strconv.Itoa(msg.ExitCode) + "_"
		if out != "" {
			note += "\n\n" + out
		}
		return note
	}
	if out == "" {
		return "> _(no output)_"
	}
	return out
}

// recover repairs markers left pending by a prior interrupted/killed session.
// Called once at startup (before columns are read) so the cleaned files load
// with the interrupted note instead of a frozen ⏳. Pure filesystem walk over
// the board's columns/cards — no *Board, no UI state.
func (te *templateExec) recover(boardPath string) {
	cols, err := board.Columns(boardPath)
	if err != nil {
		return
	}
	for _, col := range cols {
		colPath := filepath.Join(boardPath, col)
		items, err := board.Items(colPath)
		if err != nil {
			continue
		}
		for _, name := range items {
			path := filepath.Join(colPath, name+".md")
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			body := string(data)
			if !strings.Contains(body, "<!-- kbrd:shell ") {
				continue // cheap guard before the regex
			}
			newBody := body
			for _, m := range template.ParseShellMarkers(body) {
				newBody = template.RewriteShellMarker(newBody, m.ID, templateInterruptedNote)
			}
			if newBody != body {
				_ = board.ReplaceFileContent(path, newBody)
			}
		}
	}
}

// Inflight reports how many commands are currently running, for the header chip.
func (te *templateExec) Inflight() int { return te.inflight }
