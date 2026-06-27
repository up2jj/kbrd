package model

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

const mnemonicDebounceDelay = 75 * time.Millisecond

type mnemonicDebounceMsg struct{ Seq int }

type mnemonicSelectorState struct {
	active bool
	input  textinput.Model
	seq    int
}

func newMnemonicSelectorState(p Palette) mnemonicSelectorState {
	ti := textinput.New()
	ti.Prompt = ": "
	ti.Placeholder = "card mnemonic"
	ti.CharLimit = 64
	ti.SetWidth(60)
	applyInputPalette(&ti, p)
	return mnemonicSelectorState{input: ti}
}

type boardMnemonicSelector struct {
	b *Board
}

func (s boardMnemonicSelector) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	b := s.b
	switch {
	case key.Matches(msg, Keys.MnemonicJumpCancel):
		s.close()
		b.mnemonic.input.SetValue("")
		return b, nil
	case key.Matches(msg, Keys.MnemonicJumpConfirm):
		tag := strings.TrimSpace(b.mnemonic.input.Value())
		return b, s.jumpTag(tag)
	}

	before := b.mnemonic.input.Value()
	ti, cmd := b.mnemonic.input.Update(msg)
	b.mnemonic.input = ti
	if b.mnemonic.input.Value() == before {
		return b, cmd
	}
	return b, tea.Batch(cmd, s.scheduleDebounce())
}

func (s boardMnemonicSelector) handleDebounce(msg mnemonicDebounceMsg) (tea.Model, tea.Cmd) {
	b := s.b
	if !b.mnemonic.active || msg.Seq != b.mnemonic.seq {
		return b, nil
	}
	tag := strings.TrimSpace(b.mnemonic.input.Value())
	if tag == "" {
		return b, nil
	}
	if _, ok := b.refByMnemonic[tag]; ok {
		return b, s.jumpTag(tag)
	}
	if s.hasPrefix(tag) {
		return b, nil
	}
	return b, s.jumpTag(tag)
}

func (s boardMnemonicSelector) open() tea.Cmd {
	b := s.b
	b.mnemonic.seq++
	b.mnemonic.active = true
	b.mnemonic.input.SetValue("")
	return b.mnemonic.input.Focus()
}

func (s boardMnemonicSelector) close() {
	b := s.b
	b.mnemonic.seq++
	b.mnemonic.active = false
	b.mnemonic.input.Blur()
}

func (s boardMnemonicSelector) scheduleDebounce() tea.Cmd {
	b := s.b
	b.mnemonic.seq++
	seq := b.mnemonic.seq
	return tea.Tick(mnemonicDebounceDelay, func(time.Time) tea.Msg {
		return mnemonicDebounceMsg{Seq: seq}
	})
}

func (s boardMnemonicSelector) jumpTag(tag string) tea.Cmd {
	b := s.b
	s.close()
	b.mnemonic.input.SetValue("")
	if tag == "" {
		return nil
	}
	ref, ok := b.refByMnemonic[tag]
	if !ok {
		return b.notifier.Error("no item: " + tag)
	}
	col, item, err := b.resolveDelayedItemRef(ref)
	if err != nil {
		return b.notifier.ErrorCause("", err)
	}
	colIdx := b.indexOfColumn(col)
	if colIdx < 0 {
		return b.notifier.Error("column no longer exists")
	}
	b.selectedCol = colIdx
	col.SelectByName(item.Name)
	return nil
}

func (s boardMnemonicSelector) hasPrefix(prefix string) bool {
	for tag := range s.b.refByMnemonic {
		if strings.HasPrefix(tag, prefix) {
			return true
		}
	}
	return false
}

func (b *Board) mnemonicSelector() boardMnemonicSelector {
	return boardMnemonicSelector{b: b}
}
