package model

import tea "charm.land/bubbletea/v2"

func keyPressText(text string) tea.KeyPressMsg {
	rs := []rune(text)
	if len(rs) == 0 {
		return tea.KeyPressMsg{}
	}
	code := rs[0]
	if len(rs) > 1 {
		code = tea.KeyExtended
	}
	return tea.KeyPressMsg{Code: code, Text: text}
}
