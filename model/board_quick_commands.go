package model

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type boardQuickCommands struct {
	b *Board
}

func (q boardQuickCommands) handleCommand(msg quickCommandMsg) (tea.Model, tea.Cmd) {
	b := q.b
	cmd := strings.TrimPrefix(msg.Command, ":")
	if cmd == "" {
		return b, nil
	}

	action := cmd[0]
	args := cmd[1:]
	_ = args

	switch action {
	case 'r':
		return b, b.utilityActions().refresh()
	case 't':
		b.utilityActions().toggleTheme()
		return b, nil
	case 'q':
		return b, q.open()
	default:
		return b, b.notifier.Send("unknown command: "+string(action), notifyError)
	}
}

func (q boardQuickCommands) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	b := q.b
	switch {
	case key.Matches(msg, Keys.QuickCmdCancel):
		q.close()
		b.quickCmdInput.SetValue("")
		return b, nil
	case key.Matches(msg, Keys.QuickCmdConfirm):
		q.close()
		cmd := strings.TrimSpace(b.quickCmdInput.Value())
		b.quickCmdInput.SetValue("")
		if cmd == "" {
			return b, nil
		}
		if len(cmd) >= 2 && isItemCommandAction(cmd[0]) {
			action := cmd[0]
			suffix := cmd[1:]
			if ref, ok := b.refByMnemonic[suffix]; ok {
				return b, b.itemActions().dispatch(action, ref)
			}
			return b, b.notifier.Send("no item: "+suffix, notifyError)
		}
		return b, func() tea.Msg {
			return quickCommandMsg{Command: cmd}
		}
	}

	ti, cmd := b.quickCmdInput.Update(msg)
	b.quickCmdInput = ti

	buf := b.quickCmdInput.Value()
	// Item-command fast path: first char is an item action, the rest must match
	// a mnemonic. Dispatch on unique resolution.
	if len(buf) >= 1 && isItemCommandAction(buf[0]) {
		suffix := buf[1:]
		if suffix == "" {
			return b, cmd
		}
		if _, ok := b.refByMnemonic[suffix]; ok {
			return b, cmd
		}
		for tag := range b.refByMnemonic {
			if strings.HasPrefix(tag, suffix) {
				return b, cmd
			}
		}
		q.close()
		b.quickCmdInput.SetValue("")
		return b, b.notifier.Send("no item: "+suffix, notifyError)
	}

	return b, cmd
}

func (q boardQuickCommands) open() tea.Cmd {
	b := q.b
	b.quickCmdMode = true
	b.quickCmdInput.SetValue("")
	return b.quickCmdInput.Focus()
}

func (q boardQuickCommands) close() {
	b := q.b
	b.quickCmdMode = false
	b.quickCmdInput.Blur()
}

func isItemCommandAction(c byte) bool {
	switch c {
	case 'e', 'a', 'p', 'J', 'c', 'V', 'o', '!', 'd', 'm':
		return true
	}
	return false
}

func (b *Board) quickCommands() boardQuickCommands {
	return boardQuickCommands{b: b}
}
