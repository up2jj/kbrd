// Package tui contains reusable terminal controls and conventions that are
// independent of Lua, boards, and script coroutine lifecycles.
package tui

import "charm.land/bubbles/v2/key"

// KeyMap is the common interaction vocabulary for scripted controls.
type KeyMap struct {
	Cancel key.Binding
	Submit key.Binding
	Prev   key.Binding
	Next   key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+p"), key.WithHelp("esc", "cancel")),
		Submit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		Prev:   key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "previous")),
		Next:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "next")),
	}
}
