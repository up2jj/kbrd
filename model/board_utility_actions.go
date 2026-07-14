package model

import (
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"kbrd/clipboardring"
	"kbrd/events"
)

type boardUtilityActions struct {
	b *Board
}

func (u boardUtilityActions) refresh() tea.Cmd {
	b := u.b
	return func() tea.Msg {
		if err := b.loadColumns(); err != nil {
			return notifyMsg{Message: "failed to refresh: " + err.Error(), Type: notifyError}
		}
		b.bus.Publish(events.BoardRefresh{Reason: "refresh"})
		// The column_items transform needs the UI goroutine (Lua VM); this
		// closure runs on a worker, so hand off via the message handler.
		return refreshedMsg{}
	}
}

func (u boardUtilityActions) copyToClipboard(content []byte) tea.Cmd {
	return u.copyToClipboardWithEntry(content, nil, clipboardring.Entry{})
}

func (u boardUtilityActions) copyToClipboardWithEntry(content []byte, store *clipboardring.Store, entry clipboardring.Entry) tea.Cmd {
	return func() tea.Msg {
		if err := clipboard.WriteAll(string(content)); err != nil {
			return notifyMsg{Message: "clipboard not available", Type: notifyError}
		}
		if store != nil {
			if err := store.Add(entry); err != nil {
				return notifyMsg{Message: "copied, but clipboard history failed: " + err.Error(), Type: notifyError}
			}
		}
		return notifyMsg{Message: "copied to clipboard", Type: notifySuccess}
	}
}

func (b *Board) utilityActions() boardUtilityActions {
	return boardUtilityActions{b: b}
}
