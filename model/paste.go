package model

import (
	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

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

// pasteRequestMsg dispatches a chosen paste mode (from the paste menu) to the
// async paste command.
type pasteRequestMsg struct {
	ColIndex int
	FileName string
	Mode     pasteMode
}

// pasteDoneMsg reports a successful clipboard paste back to the Update loop so
// the reload, the ItemSaved publish, and the toast all run on the UI goroutine.
// The paste command does its disk write in a goroutine, where it must not touch
// the (concurrency-unsafe) event bus or in-memory column state. Kind is the
// ItemSaved kind for the paste mode (prepend/append/journal map directly; a
// whole-file replace is reported as "save").
type pasteDoneMsg struct {
	ColIndex int
	FileName string
	Kind     string
	Verb     string
}

// openPasteMenu reads the clipboard and opens the paste-mode picker over the
// selected card. An empty/unavailable clipboard is reported without opening.
func (b *Board) openPasteMenu(colIdx int, fileName string) tea.Cmd {
	text, err := clipboard.ReadAll()
	if err != nil || text == "" {
		return b.notifier.Send("clipboard empty or unavailable", notifyError)
	}
	b.dialog.Open(DialogOptions{
		Title: "Paste from clipboard",
		Body:  "Into " + fileName + ".md",
		Buttons: []DialogButton{
			{Label: "At beginning", Hotkey: 'a',
				Msg: pasteRequestMsg{ColIndex: colIdx, FileName: fileName, Mode: pasteAtStart}},
			{Label: "Append at end", Hotkey: 'p',
				Msg: pasteRequestMsg{ColIndex: colIdx, FileName: fileName, Mode: pasteAtEnd}},
			{Label: "Journal entry", Hotkey: 'j',
				Msg: pasteRequestMsg{ColIndex: colIdx, FileName: fileName, Mode: pasteJournal}},
			{Label: "Replace whole file", Kind: ButtonDanger, Hotkey: 'R',
				Msg: pasteRequestMsg{ColIndex: colIdx, FileName: fileName, Mode: pasteReplace}},
		},
		DefaultIndex: 1,
	})
	return nil
}

// pasteToItem performs the clipboard write for a chosen mode on a goroutine and
// reports completion via pasteDoneMsg (handlePasteDone finishes on the UI thread).
func (b *Board) pasteToItem(colIdx int, fileName string, mode pasteMode) tea.Cmd {
	return func() tea.Msg {
		text, err := clipboard.ReadAll()
		if err != nil || text == "" {
			return notifyMsg{Message: "clipboard empty or unavailable", Type: notifyError}
		}
		col := b.columns[colIdx]
		var verb, kind string
		switch mode {
		case pasteAtStart:
			err = col.PrependText(fileName, text)
			verb, kind = "prepended to ", "prepend"
		case pasteJournal:
			at, body := b.journalStamp(text)
			err = col.JournalText(fileName, at, body)
			verb, kind = "journaled to ", "journal"
		case pasteReplace:
			err = col.ReplaceFile(fileName, text)
			verb, kind = "replaced ", "save"
		default:
			err = col.AppendText(fileName, text)
			verb, kind = "appended to ", "append"
		}
		if err != nil {
			return notifyMsg{Message: "failed to paste: " + err.Error(), Type: notifyError}
		}
		// Reload, publish ItemSaved, and toast on the UI goroutine — see pasteDoneMsg.
		return pasteDoneMsg{ColIndex: colIdx, FileName: fileName, Kind: kind, Verb: verb}
	}
}

// handlePasteDone finalizes a successful clipboard paste on the UI goroutine:
// reload the column, keep the pasted item selected, publish ItemSaved (so
// item_saved hooks fire for pastes exactly as they do for editor
// append/prepend/journal/save), and toast.
func (b *Board) handlePasteDone(msg pasteDoneMsg) (tea.Model, tea.Cmd) {
	if msg.ColIndex < 0 || msg.ColIndex >= len(b.columns) {
		return b, nil
	}
	col := b.columns[msg.ColIndex]
	b.reloadColumnAfterMutation(col)
	col.SelectByName(msg.FileName)
	b.bus.Publish(events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: msg.FileName}, Kind: msg.Kind})
	return b, b.notifier.Send(msg.Verb+msg.FileName, notifySuccess)
}
