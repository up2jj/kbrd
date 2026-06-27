package model

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type boardMnemonicSelector struct {
	b *Board
}

func (s boardMnemonicSelector) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	b := s.b
	switch {
	case key.Matches(msg, Keys.MnemonicJumpCancel):
		s.close()
		b.mnemonicInput.SetValue("")
		return b, nil
	case key.Matches(msg, Keys.MnemonicJumpConfirm):
		s.close()
		tag := strings.TrimSpace(b.mnemonicInput.Value())
		b.mnemonicInput.SetValue("")
		if tag == "" {
			return b, nil
		}
		ref, ok := b.refByMnemonic[tag]
		if !ok {
			return b, b.notifier.Error("no item: " + tag)
		}
		col, item, err := b.resolveDelayedItemRef(ref)
		if err != nil {
			return b, b.notifier.ErrorCause("", err)
		}
		colIdx := b.indexOfColumn(col)
		if colIdx < 0 {
			return b, b.notifier.Error("column no longer exists")
		}
		b.selectedCol = colIdx
		col.SelectByName(item.Name)
		return b, nil
	}

	ti, cmd := b.mnemonicInput.Update(msg)
	b.mnemonicInput = ti
	return b, cmd
}

func (s boardMnemonicSelector) open() tea.Cmd {
	b := s.b
	b.mnemonicMode = true
	b.mnemonicInput.SetValue("")
	return b.mnemonicInput.Focus()
}

func (s boardMnemonicSelector) close() {
	b := s.b
	b.mnemonicMode = false
	b.mnemonicInput.Blur()
}

func (b *Board) mnemonicSelector() boardMnemonicSelector {
	return boardMnemonicSelector{b: b}
}
