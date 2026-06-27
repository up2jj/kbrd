package model

import (
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

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

func (u boardUtilityActions) toggleTheme() {
	b := u.b
	if b.theme == "dark" {
		b.theme = "light"
	} else {
		b.theme = "dark"
	}
	b.applyPalette()
}

func (u boardUtilityActions) copyToClipboard(content []byte) tea.Cmd {
	return func() tea.Msg {
		if err := clipboard.WriteAll(string(content)); err != nil {
			return notifyMsg{Message: "clipboard not available", Type: notifyError}
		}
		return notifyMsg{Message: "copied to clipboard", Type: notifySuccess}
	}
}

func (b *Board) utilityActions() boardUtilityActions {
	return boardUtilityActions{b: b}
}
