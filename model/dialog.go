package model

import (
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type ButtonKind int

const (
	ButtonDefault ButtonKind = iota
	ButtonPrimary
	ButtonDanger
)

type DialogButton struct {
	Label  string
	Kind   ButtonKind
	Msg    tea.Msg
	Hotkey rune
}

type DialogOptions struct {
	Title        string
	Body         string
	Buttons      []DialogButton
	DefaultIndex int
	CancelMsg    tea.Msg
}

type Dialog struct {
	active    bool
	title     string
	body      string
	buttons   []DialogButton
	mnemonics []int // rune index into Label for each button, -1 if none
	selected  int
	cancelMsg tea.Msg
	palette   Palette
}

func (d *Dialog) Open(opts DialogOptions) {
	d.active = true
	d.title = opts.Title
	d.body = opts.Body
	d.buttons = opts.Buttons
	d.mnemonics = computeDialogMnemonics(opts.Buttons)
	idx := max(opts.DefaultIndex, 0)
	if idx >= len(opts.Buttons) {
		idx = len(opts.Buttons) - 1
	}
	d.selected = idx
	d.cancelMsg = opts.CancelMsg
}

// computeDialogMnemonics assigns one mnemonic letter per button, picking the
// first unused letter in each label. Returns the rune index of the chosen
// character, or -1 if no letter could be assigned.
func computeDialogMnemonics(buttons []DialogButton) []int {
	used := map[rune]bool{}
	res := make([]int, len(buttons))
	for i, b := range buttons {
		res[i] = -1
		if b.Hotkey != 0 {
			hk := unicode.ToLower(b.Hotkey)
			for j, r := range b.Label {
				if unicode.ToLower(r) == hk {
					res[i] = j
					break
				}
			}
			used[hk] = true
		}
	}
	for i, b := range buttons {
		if b.Hotkey != 0 {
			continue
		}
		for j, r := range b.Label {
			lr := unicode.ToLower(r)
			if !unicode.IsLetter(lr) || used[lr] {
				continue
			}
			used[lr] = true
			res[i] = j
			break
		}
	}
	return res
}

// OpenConfirm shows a Yes/No dialog. Yes is primary and focused by default.
func (d *Dialog) OpenConfirm(title, body string, onConfirm tea.Msg) {
	d.Open(DialogOptions{
		Title: title,
		Body:  body,
		Buttons: []DialogButton{
			{Label: "Yes", Kind: ButtonPrimary, Msg: onConfirm},
			{Label: "No"},
		},
		DefaultIndex: 0,
	})
}

// OpenConfirmDestructive shows a <actionLabel>/Cancel dialog. The action
// button is danger-styled; focus defaults to the safe Cancel button.
func (d *Dialog) OpenConfirmDestructive(title, body, actionLabel string, onConfirm tea.Msg) {
	d.Open(DialogOptions{
		Title: title,
		Body:  body,
		Buttons: []DialogButton{
			{Label: actionLabel, Kind: ButtonDanger, Msg: onConfirm},
			{Label: "Cancel", Kind: ButtonPrimary},
		},
		DefaultIndex: 1,
	})
}

func (d *Dialog) Close() {
	d.active = false
	d.cancelMsg = nil
}

func (d *Dialog) Update(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, Keys.DialogPrev):
		if d.selected > 0 {
			d.selected--
		}
	case key.Matches(msg, Keys.DialogNext):
		if d.selected < len(d.buttons)-1 {
			d.selected++
		}
	case key.Matches(msg, Keys.DialogConfirm):
		chosen := d.buttons[d.selected]
		d.Close()
		if chosen.Msg != nil {
			return func() tea.Msg { return chosen.Msg }
		}
	case key.Matches(msg, Keys.DialogCancel):
		cancelMsg := d.cancelMsg
		d.Close()
		if cancelMsg != nil {
			return func() tea.Msg { return cancelMsg }
		}
	default:
		if len(msg.Text) == 1 {
			raw := []rune(msg.Text)[0]
			for i, b := range d.buttons {
				if b.Hotkey != 0 && raw == b.Hotkey {
					chosen := d.buttons[i]
					d.Close()
					if chosen.Msg != nil {
						return func() tea.Msg { return chosen.Msg }
					}
					return nil
				}
			}
			r := unicode.ToLower(raw)
			for i, idx := range d.mnemonics {
				if idx < 0 || d.buttons[i].Hotkey != 0 {
					continue
				}
				mr := unicode.ToLower([]rune(d.buttons[i].Label)[idx])
				if mr == r {
					chosen := d.buttons[i]
					d.Close()
					if chosen.Msg != nil {
						return func() tea.Msg { return chosen.Msg }
					}
					return nil
				}
			}
		}
	}
	return nil
}

func renderDialogLabel(label string, mIdx int, style lipgloss.Style) string {
	if mIdx < 0 {
		return style.Render(label)
	}
	runes := []rune(label)
	before := string(runes[:mIdx])
	mn := string(runes[mIdx])
	after := string(runes[mIdx+1:])
	// Inject raw underline-on / underline-off SGR codes so the outer style's
	// background and padding survive intact — using a nested lipgloss style
	// would emit a full reset and clobber the button's bg.
	const underlineOn = "\x1b[4m"
	const underlineOff = "\x1b[24m"
	return style.Render(before + underlineOn + mn + underlineOff + after)
}

func (d *Dialog) View() string {
	if !d.active {
		return ""
	}

	p := d.palette
	bodyStyle := lipgloss.NewStyle().Foreground(p.FgMuted)

	btnBase := lipgloss.NewStyle().Padding(0, 3)
	activeDanger := btnBase.Bold(true).Background(p.Danger).Foreground(p.FgOnAccent)
	activePrimary := btnBase.Bold(true).Background(p.PrimaryStrong).Foreground(p.FgOnAccent)
	inactive := btnBase.Foreground(p.FgSubtle)

	btnViews := make([]string, len(d.buttons))
	for i, btn := range d.buttons {
		var style lipgloss.Style
		if i == d.selected {
			if btn.Kind == ButtonDanger {
				style = activeDanger
			} else {
				style = activePrimary
			}
		} else {
			style = inactive
		}
		btnViews[i] = renderDialogLabel(btn.Label, d.mnemonics[i], style)
	}

	btnRow := btnViews[0]
	for _, b := range btnViews[1:] {
		btnRow = lipgloss.JoinHorizontal(lipgloss.Center, btnRow, "   ", b)
	}

	contentRows := make([]string, 0, 5)
	if d.body != "" {
		contentRows = append(contentRows, bodyStyle.Render(d.body), "")
	}
	contentRows = append(contentRows,
		btnRow,
		"",
		RenderInlineHints([]Shortcut{
			{"←/→", "select"},
			bindingShortcut(Keys.DialogConfirm),
			bindingShortcut(Keys.DialogCancel),
		}),
	)
	content := lipgloss.JoinVertical(lipgloss.Center, contentRows...)

	return OverlayFrame{Title: d.title, Body: content, Palette: p}.Render()
}
