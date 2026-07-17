package tui

import (
	"regexp"
	"strconv"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"kbrd/theme"
)

type InputOptions struct {
	Title       string
	Label       string
	Initial     string
	Placeholder string
	Required    bool
	MinLength   int
	MaxLength   int
	Pattern     string
	PatternHint string
}

type InputResult struct {
	Value     string
	Submitted bool
	Cancelled bool
}

type Input struct {
	opts    InputOptions
	input   textinput.Model
	pattern *regexp.Regexp
	error   string
	result  *InputResult
	active  bool
	size    Size
	palette theme.Palette
	keys    KeyMap
}

func (i *Input) Open(opts InputOptions) {
	input := textinput.New()
	input.Prompt = "› "
	input.Placeholder = opts.Placeholder
	input.CharLimit = opts.MaxLength
	input.SetValue(opts.Initial)
	input.Focus()
	theme.ApplyTextInputPalette(&input, i.palette)

	i.opts = opts
	i.input = input
	i.pattern = nil
	if opts.Pattern != "" {
		i.pattern = regexp.MustCompile(opts.Pattern)
	}
	i.error = ""
	i.result = nil
	i.active = true
	i.keys = DefaultKeyMap()
	i.resizeInput()
}

func (i *Input) Active() bool { return i.active }

func (i *Input) SetSize(width, height int) {
	i.size.Set(width, height)
	i.resizeInput()
}

func (i *Input) SetPalette(p theme.Palette) {
	i.palette = p
	if i.active {
		theme.ApplyTextInputPalette(&i.input, p)
	}
}

func (i *Input) Update(msg tea.Msg) tea.Cmd {
	if !i.active {
		return nil
	}
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(keyMsg, i.keys.Cancel):
			i.finish(InputResult{Cancelled: true})
			return nil
		case key.Matches(keyMsg, i.keys.Submit):
			if err := i.validate(); err != "" {
				i.error = err
				return nil
			}
			i.finish(InputResult{Value: i.input.Value(), Submitted: true})
			return nil
		}
	}
	model, cmd := i.input.Update(msg)
	i.input = model
	i.error = ""
	return cmd
}

func (i *Input) View() string {
	if !i.active {
		return ""
	}
	rows := make([]string, 0, 3)
	if i.opts.Label != "" {
		rows = append(rows, lipgloss.NewStyle().Foreground(i.palette.FgBase).Render(i.opts.Label))
	}
	rows = append(rows, i.input.View())
	if i.error != "" {
		rows = append(rows, lipgloss.NewStyle().Foreground(i.palette.Danger).Render(i.error))
	}
	title := i.opts.Title
	if title == "" {
		title = "Input"
	}
	return theme.OverlayFrame{
		Title: title, Body: lipgloss.JoinVertical(lipgloss.Left, rows...),
		Footer:  theme.RenderHints(i.palette, []theme.Hint{{Keys: "enter", Label: "confirm"}, {Keys: "esc", Label: "cancel"}}),
		Palette: i.palette, Width: i.frameWidth(),
	}.Render()
}

func (i *Input) TakeResult() (InputResult, bool) {
	if i.result == nil {
		return InputResult{}, false
	}
	result := *i.result
	i.result = nil
	return result, true
}

func (i *Input) Close() {
	i.active = false
	i.result = nil
	i.error = ""
	i.input = textinput.Model{}
}

func (i *Input) Value() string { return i.input.Value() }

func (i *Input) finish(result InputResult) {
	i.result = &result
	i.active = false
}

func (i *Input) validate() string {
	value := i.input.Value()
	length := utf8.RuneCountInString(value)
	if i.opts.Required && length == 0 {
		return "A value is required"
	}
	if i.opts.MinLength > 0 && length < i.opts.MinLength {
		return "Must be at least " + runeCountLabel(i.opts.MinLength)
	}
	if i.opts.MaxLength > 0 && length > i.opts.MaxLength {
		return "Must be at most " + runeCountLabel(i.opts.MaxLength)
	}
	if i.pattern != nil && !i.pattern.MatchString(value) {
		if i.opts.PatternHint != "" {
			return i.opts.PatternHint
		}
		return "Value has an invalid format"
	}
	return ""
}

func (i *Input) resizeInput() {
	width := i.size.Fit(72, 0).Width - 8
	i.input.SetWidth(max(width, 12))
}

func (i *Input) frameWidth() int {
	return max(i.size.Fit(72, 0).Width-2, 0)
}

func runeCountLabel(n int) string {
	if n == 1 {
		return "1 character"
	}
	return strconv.Itoa(n) + " characters"
}
