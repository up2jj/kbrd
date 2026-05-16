package model

import (
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
}

type Dialog struct {
	active    bool
	title     string
	body      string
	buttons   []DialogButton
	mnemonics []int // rune index into Label for each button, -1 if none
	selected  int
}

func (d *Dialog) Open(opts DialogOptions) {
	d.active = true
	d.title = opts.Title
	d.body = opts.Body
	d.buttons = opts.Buttons
	d.mnemonics = computeDialogMnemonics(opts.Buttons)
	idx := opts.DefaultIndex
	if idx < 0 {
		idx = 0
	}
	if idx >= len(opts.Buttons) {
		idx = len(opts.Buttons) - 1
	}
	d.selected = idx
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
}

func (d *Dialog) Update(msg tea.KeyMsg) tea.Cmd {
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
		d.Close()
	default:
		if len(msg.Runes) == 1 {
			raw := msg.Runes[0]
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

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#f1f5f9"))
	bodyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))

	btnBase := lipgloss.NewStyle().Padding(0, 3)
	activeDanger := btnBase.Bold(true).Background(lipgloss.Color("#ef4444")).Foreground(lipgloss.Color("#ffffff"))
	activePrimary := btnBase.Bold(true).Background(lipgloss.Color("#3b82f6")).Foreground(lipgloss.Color("#ffffff"))
	inactive := btnBase.Foreground(lipgloss.Color("#64748b"))

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

	content := lipgloss.JoinVertical(lipgloss.Center,
		titleStyle.Render(d.title),
		"",
		bodyStyle.Render(d.body),
		"",
		btnRow,
		"",
		RenderInlineHints([]Shortcut{
			{"←/→", "select"},
			bindingShortcut(Keys.DialogConfirm),
			bindingShortcut(Keys.DialogCancel),
		}),
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#475569")).
		Padding(1, 4).
		Render(content)
}
