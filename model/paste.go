package model

import (
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"kbrd/board"
	"kbrd/events"
)

// pasteMode selects how a clipboard paste merges into the target card.
type pasteMode int

const (
	pasteAtEnd pasteMode = iota
	pasteAtStart
	pasteReplace
	pasteJournal
)

// pasteRequestMsg dispatches a chosen paste mode to the async paste command. It
// carries a target resolved on the UI goroutine — the column name/path and the
// item's full path — rather than a column index, so the write (and the later
// finalize) bind to a stable identity even if columns are reloaded, reordered,
// or the board is switched before the command runs.
type pasteRequestMsg struct {
	ColName  string
	ColPath  string
	ItemPath string
	FileName string
	Mode     pasteMode
}

// pasteDoneMsg reports a successful clipboard paste back to the Update loop so
// the reload, the ItemSaved publish, and the toast all run on the UI goroutine
// (the event bus and in-memory column state are not goroutine-safe). It carries
// the same stable identity as the request; handlePasteDone resolves it against
// the current board and no-ops if the target is gone. Kind is the ItemSaved kind
// for the paste mode (prepend/append/journal map directly; a whole-file replace
// is reported as "save").
type pasteDoneMsg struct {
	ColName  string
	ColPath  string
	FileName string
	Kind     string
	Verb     string
}

type boardPasteActions struct {
	board *Board
}

func (b *Board) pasteActions() boardPasteActions {
	return boardPasteActions{board: b}
}

// openPasteMenu reads the clipboard and opens the paste-mode picker over the
// selected card. The target is resolved here, on the UI goroutine, into a stable
// (column name/path + item path) identity carried by each button's request — so
// nothing downstream depends on the column's current index. An empty/unavailable
// clipboard, or an item that can't be resolved, is reported without opening.
func (a boardPasteActions) openMenu(colIdx int, fileName string) tea.Cmd {
	b := a.board
	text, err := clipboard.ReadAll()
	if err != nil || text == "" {
		return b.notifier.Error("clipboard empty or unavailable")
	}
	if colIdx < 0 || colIdx >= len(b.columns) {
		return b.notifier.Error("item not found: " + fileName)
	}
	col := b.columns[colIdx]
	itemPath := col.fullPathFor(fileName)
	if itemPath == "" {
		return b.notifier.Error("item not found: " + fileName)
	}
	req := func(mode pasteMode) pasteRequestMsg {
		return pasteRequestMsg{ColName: col.Name, ColPath: col.Path, ItemPath: itemPath, FileName: fileName, Mode: mode}
	}
	b.dialog.Open(DialogOptions{
		Title: "Paste from clipboard",
		Body:  "Into " + fileName + ".md",
		Buttons: []DialogButton{
			{Label: "At beginning", Hotkey: 'a', Msg: req(pasteAtStart)},
			{Label: "Append at end", Hotkey: 'p', Msg: req(pasteAtEnd)},
			{Label: "Journal entry", Hotkey: 'j', Msg: req(pasteJournal)},
			{Label: "Replace whole file", Kind: ButtonDanger, Hotkey: 'R', Msg: req(pasteReplace)},
		},
		DefaultIndex: 1,
	})
	return nil
}

// pasteToItem performs the clipboard write for a chosen mode on a goroutine,
// writing directly to the request's captured item path (never touching
// b.columns, which may have changed). Completion is reported via pasteDoneMsg so
// handlePasteDone can finish on the UI thread.
func (a boardPasteActions) pasteToItem(msg pasteRequestMsg) tea.Cmd {
	b := a.board
	// Capture journal config on the UI goroutine; the worker must not read live
	// Board state (a board switch / config reload could race it).
	detectDate := b.cfg.Journal.DetectDate
	return func() tea.Msg {
		text, err := clipboard.ReadAll()
		if err != nil || text == "" {
			return notifyMsg{Message: "clipboard empty or unavailable", Type: notifyError}
		}
		var verb, kind string
		switch msg.Mode {
		case pasteAtStart:
			err = board.PrependLine(msg.ItemPath, text)
			verb, kind = "prepended to ", "prepend"
		case pasteJournal:
			at, body := journalStampWith(detectDate, text)
			err = board.JournalLine(msg.ItemPath, at, body)
			verb, kind = "journaled to ", "journal"
		case pasteReplace:
			err = board.ReplaceFileContent(msg.ItemPath, text)
			verb, kind = "replaced ", "save"
		default:
			err = board.AppendLine(msg.ItemPath, text)
			verb, kind = "appended to ", "append"
		}
		if err != nil {
			return notifyMsg{Message: "failed to paste: " + err.Error(), Type: notifyError}
		}
		// Reload, publish ItemSaved, and toast on the UI goroutine — see pasteDoneMsg.
		return pasteDoneMsg{ColName: msg.ColName, ColPath: msg.ColPath, FileName: msg.FileName, Kind: kind, Verb: verb}
	}
}

// handlePasteDone finalizes a successful clipboard paste on the UI goroutine:
// resolve the target column by stable identity, reload it, keep the pasted item
// selected, publish ItemSaved (so item_saved hooks fire for pastes exactly as
// they do for editor append/prepend/journal/save), and toast. If the board no
// longer holds the target (reloaded away, reordered to nothing, or the board was
// switched), it no-ops — the disk write already landed and the fs watcher will
// reconcile the view.
func (a boardPasteActions) handleDone(msg pasteDoneMsg) (tea.Model, tea.Cmd) {
	b := a.board
	col := a.resolveColumn(msg)
	if col == nil {
		return b, nil
	}
	b.reloadColumnAfterMutation(col)
	col.SelectByName(msg.FileName)
	b.bus.Publish(events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: msg.FileName}, Kind: msg.Kind})
	return b, b.notifier.Success(msg.Verb + msg.FileName)
}

// resolvePasteColumn finds the column a completed paste belongs to by stable
// identity: a real column matches by directory path (unique within and across
// boards, and stable across reload/reorder); a virtual column (no path) matches
// by name only if it still holds the written file. Returns nil when the target
// is no longer present.
func (a boardPasteActions) resolveColumn(msg pasteDoneMsg) *Column {
	b := a.board
	for _, c := range b.columns {
		if msg.ColPath != "" {
			if c.Path == msg.ColPath {
				return c
			}
			continue
		}
		if c.Name == msg.ColName && c.fullPathFor(msg.FileName) != "" {
			return c
		}
	}
	return nil
}
