package tui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"kbrd/theme"
)

type TextareaOptions struct {
	Title       string
	Initial     string
	LineNumbers bool
	Actions     []Action
}

type TextareaResult struct {
	Action    string
	Value     string
	Submitted bool
	Cancelled bool
}

// Textarea is a simple multiline input for scripted modals.
type Textarea struct {
	opts    TextareaOptions
	input   textarea.Model
	result  *TextareaResult
	active  bool
	size    Size
	palette theme.Palette
}

func (t *Textarea) Open(opts TextareaOptions) {
	input := textarea.New()
	input.ShowLineNumbers = opts.LineNumbers
	input.SetValue(opts.Initial)
	input.CursorEnd()
	input.Focus()

	t.opts = opts
	t.input = input
	t.result = nil
	t.active = true
	t.applyPalette()
	t.fit()
}

func (t *Textarea) Active() bool { return t.active }

func (t *Textarea) SetSize(width, height int) {
	t.size.Set(width, height)
	t.fit()
}

func (t *Textarea) SetPalette(p theme.Palette) {
	t.palette = p
	if t.active {
		t.applyPalette()
	}
}

func (t *Textarea) Update(msg tea.Msg) tea.Cmd {
	if !t.active {
		return nil
	}
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		pressed := strings.ToLower(keyMsg.String())
		if pressed == "esc" {
			t.finish(TextareaResult{Cancelled: true})
			return nil
		}
		for _, action := range t.opts.Actions {
			if action.Key == "" || !strings.EqualFold(action.Key, pressed) {
				continue
			}
			if action.Disabled {
				return nil
			}
			t.finish(TextareaResult{
				Action: action.ID, Value: t.input.Value(), Submitted: true,
			})
			return nil
		}
	}
	input, cmd := t.input.Update(msg)
	t.input = input
	return cmd
}

func (t *Textarea) View() string {
	if !t.active {
		return ""
	}
	title := t.opts.Title
	if title == "" {
		title = "Textarea"
	}
	hints := make([]theme.Hint, 0, len(t.opts.Actions)+1)
	for _, action := range t.opts.Actions {
		hints = append(hints, theme.Hint{Keys: action.Key, Label: action.Label})
	}
	hints = append(hints, theme.Hint{Keys: "esc", Label: "cancel"})
	return theme.OverlayFrame{
		Title: title, Body: t.input.View(), Footer: theme.RenderHints(t.palette, hints),
		Palette: t.palette, Width: t.frameWidth(),
	}.Render()
}

func (t *Textarea) TakeResult() (TextareaResult, bool) {
	if t.result == nil {
		return TextareaResult{}, false
	}
	result := *t.result
	t.result = nil
	return result, true
}

func (t *Textarea) Close() {
	t.active = false
	t.result = nil
	t.opts = TextareaOptions{}
	t.input = textarea.Model{}
}

func (t *Textarea) finish(result TextareaResult) {
	t.result = &result
	t.active = false
}

func (t *Textarea) fit() {
	if !t.active {
		return
	}
	t.input.SetWidth(max(t.frameWidth()-8, 1))
	t.input.SetHeight(max(t.size.Height-9, 3))
}

func (t *Textarea) frameWidth() int { return max(t.size.Fit(110, 0).Width-2, 20) }

func (t *Textarea) applyPalette() {
	styles := t.input.Styles()
	styles.Focused.Text = lipgloss.NewStyle().Foreground(t.palette.FgBase)
	styles.Focused.LineNumber = lipgloss.NewStyle().Foreground(t.palette.FgDim)
	styles.Focused.CursorLineNumber = lipgloss.NewStyle().Foreground(t.palette.Primary)
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(t.palette.Primary)
	styles.Blurred = styles.Focused
	styles.Cursor.Color = t.palette.Highlight
	t.input.SetStyles(styles)
}
