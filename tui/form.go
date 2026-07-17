package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"kbrd/theme"
)

type FormField struct {
	ID          string
	Type        string
	Label       string
	Description string
	Placeholder string
	Required    bool
	Initial     any
	Items       []SelectItem
	MinLength   int
	MaxLength   int
	Pattern     string
	PatternHint string
}

type FormOptions struct {
	Title  string
	Fields []FormField
}

type FormResult struct {
	Values    map[string]any
	Submitted bool
	Cancelled bool
}

// Form adapts declarative fields onto huh while retaining kbrd's overlay and
// lifecycle conventions.
type Form struct {
	opts    FormOptions
	form    *huh.Form
	result  *FormResult
	active  bool
	size    Size
	palette theme.Palette
}

func (f *Form) Open(opts FormOptions) tea.Cmd {
	f.opts = opts
	f.result = nil
	f.active = true

	fields := make([]huh.Field, 0, len(opts.Fields))
	for _, spec := range opts.Fields {
		fields = append(fields, buildFormField(spec))
	}
	f.form = huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(formTheme(f.palette)).
		WithShowHelp(false)
	f.fit()
	return f.form.Init()
}

func (f *Form) Active() bool { return f.active }

func (f *Form) SetSize(width, height int) {
	f.size.Set(width, height)
	f.fit()
}

func (f *Form) SetPalette(p theme.Palette) {
	f.palette = p
	if f.form != nil {
		f.form = f.form.WithTheme(formTheme(p))
	}
}

func (f *Form) Update(msg tea.Msg) tea.Cmd {
	if !f.active || f.form == nil {
		return nil
	}
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok && (keyMsg.String() == "esc" || keyMsg.String() == "ctrl+p") {
		f.finish(FormResult{Cancelled: true})
		return nil
	}
	model, cmd := f.form.Update(msg)
	if form, ok := model.(*huh.Form); ok {
		f.form = form
	}
	switch f.form.State {
	case huh.StateCompleted:
		values := f.collectValues()
		f.finish(FormResult{Values: values, Submitted: true})
		return nil // huh queues tea.Quit on completion; never propagate it.
	case huh.StateAborted:
		f.finish(FormResult{Cancelled: true})
		return nil
	}
	return cmd
}

func (f *Form) View() string {
	if !f.active || f.form == nil {
		return ""
	}
	title := f.opts.Title
	if title == "" {
		title = "Form"
	}
	return theme.OverlayFrame{
		Title: title, Body: f.form.View(),
		Footer: theme.RenderHints(f.palette, []theme.Hint{
			{Keys: "tab/enter", Label: "next"}, {Keys: "shift+tab", Label: "back"}, {Keys: "esc", Label: "cancel"},
		}),
		Palette: f.palette, Width: f.frameWidth(),
	}.Render()
}

func (f *Form) TakeResult() (FormResult, bool) {
	if f.result == nil {
		return FormResult{}, false
	}
	result := *f.result
	f.result = nil
	return result, true
}

func (f *Form) Close() {
	f.active = false
	f.form = nil
	f.result = nil
	f.opts = FormOptions{}
}

func (f *Form) finish(result FormResult) {
	f.result = &result
	f.active = false
}

func (f *Form) fit() {
	if f.form == nil || f.size.Width <= 0 || f.size.Height <= 0 {
		return
	}
	f.form = f.form.
		WithWidth(max(20, min(f.size.Width-12, 72))).
		WithHeight(max(6, min(f.size.Height-8, 24)))
}

func (f *Form) frameWidth() int { return max(f.size.Fit(82, 0).Width-2, 0) }

func (f *Form) collectValues() map[string]any {
	values := make(map[string]any)
	for _, field := range f.opts.Fields {
		switch field.Type {
		case "label", "separator":
		case "checkbox":
			values[field.ID] = f.form.GetBool(field.ID)
		case "multiselect":
			selected, _ := f.form.Get(field.ID).([]string)
			if selected == nil {
				selected = []string{}
			}
			values[field.ID] = selected
		case "number":
			text := f.form.GetString(field.ID)
			if strings.TrimSpace(text) == "" {
				values[field.ID] = nil
			} else {
				value, _ := strconv.ParseFloat(text, 64)
				values[field.ID] = value
			}
		default:
			values[field.ID] = f.form.GetString(field.ID)
		}
	}
	return values
}

func buildFormField(field FormField) huh.Field {
	switch field.Type {
	case "textarea":
		value, _ := field.Initial.(string)
		return huh.NewText().Key(field.ID).Title(field.Label).Description(field.Description).
			Placeholder(field.Placeholder).Lines(5).ExternalEditor(false).Value(&value).Validate(textValidator(field))
	case "select":
		value, _ := field.Initial.(string)
		return huh.NewSelect[string]().Key(field.ID).Title(field.Label).Description(field.Description).
			Options(formOptions(field.Items)...).Value(&value)
	case "multiselect":
		value, _ := field.Initial.([]string)
		input := huh.NewMultiSelect[string]().Key(field.ID).Title(field.Label).Description(field.Description).
			Options(formOptions(field.Items)...).Value(&value)
		if field.Required {
			input = input.Validate(func(value []string) error {
				if len(value) == 0 {
					return fmt.Errorf("select at least one option")
				}
				return nil
			})
		}
		return input
	case "checkbox":
		value, _ := field.Initial.(bool)
		input := huh.NewConfirm().Key(field.ID).Title(field.Label).Description(field.Description).Value(&value)
		if field.Required {
			input = input.Validate(func(value bool) error {
				if !value {
					return fmt.Errorf("must be checked")
				}
				return nil
			})
		}
		return input
	case "number":
		value := ""
		if initial, ok := field.Initial.(float64); ok {
			value = strconv.FormatFloat(initial, 'f', -1, 64)
		}
		return huh.NewInput().Key(field.ID).Title(field.Label).Description(field.Description).
			Placeholder(field.Placeholder).Value(&value).Validate(func(value string) error {
			if strings.TrimSpace(value) == "" {
				if field.Required {
					return fmt.Errorf("value is required")
				}
				return nil
			}
			if _, err := strconv.ParseFloat(value, 64); err != nil {
				return fmt.Errorf("enter a valid number")
			}
			return nil
		})
	case "label":
		return huh.NewNote().Title(field.Label).Description(field.Description)
	case "separator":
		return huh.NewNote().Title(field.Label).Description("────────────────────────")
	default: // input
		value, _ := field.Initial.(string)
		return huh.NewInput().Key(field.ID).Title(field.Label).Description(field.Description).
			Placeholder(field.Placeholder).Value(&value).Validate(textValidator(field))
	}
}

func formOptions(items []SelectItem) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(items))
	for _, item := range items {
		if item.Disabled {
			continue
		}
		label := strings.TrimSpace(strings.TrimSpace(item.Icon + " " + item.Label))
		if item.Description != "" {
			label += " — " + item.Description
		}
		options = append(options, huh.NewOption(label, item.ID))
	}
	return options
}

func textValidator(field FormField) func(string) error {
	var pattern *regexp.Regexp
	if field.Pattern != "" {
		pattern = regexp.MustCompile(field.Pattern)
	}
	return func(value string) error {
		length := utf8.RuneCountInString(value)
		if field.Required && strings.TrimSpace(value) == "" {
			return fmt.Errorf("value is required")
		}
		if field.MinLength > 0 && length < field.MinLength {
			return fmt.Errorf("enter at least %d characters", field.MinLength)
		}
		if field.MaxLength > 0 && length > field.MaxLength {
			return fmt.Errorf("enter at most %d characters", field.MaxLength)
		}
		if pattern != nil && !pattern.MatchString(value) {
			if field.PatternHint != "" {
				return fmt.Errorf("%s", field.PatternHint)
			}
			return fmt.Errorf("value does not match %s", field.Pattern)
		}
		return nil
	}
}

func formTheme(p theme.Palette) huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		styles := huh.ThemeBase(isDark)
		styles.Focused.Base = styles.Focused.Base.BorderForeground(p.BorderActive)
		styles.Focused.Title = styles.Focused.Title.Foreground(p.Primary).Bold(true)
		styles.Focused.NoteTitle = styles.Focused.NoteTitle.Foreground(p.Primary).Bold(true)
		styles.Focused.Description = styles.Focused.Description.Foreground(p.FgMuted)
		styles.Focused.ErrorIndicator = styles.Focused.ErrorIndicator.Foreground(p.Danger)
		styles.Focused.ErrorMessage = styles.Focused.ErrorMessage.Foreground(p.Danger)
		styles.Focused.SelectSelector = styles.Focused.SelectSelector.Foreground(p.Primary)
		styles.Focused.Option = styles.Focused.Option.Foreground(p.FgBase)
		styles.Focused.MultiSelectSelector = styles.Focused.MultiSelectSelector.Foreground(p.Primary)
		styles.Focused.SelectedOption = styles.Focused.SelectedOption.Foreground(p.Primary)
		styles.Focused.SelectedPrefix = styles.Focused.SelectedPrefix.Foreground(p.Success)
		styles.Focused.UnselectedOption = styles.Focused.UnselectedOption.Foreground(p.FgBase)
		styles.Focused.UnselectedPrefix = styles.Focused.UnselectedPrefix.Foreground(p.FgDim)
		styles.Focused.FocusedButton = styles.Focused.FocusedButton.Foreground(p.FgOnAccent).Background(p.PrimaryStrong)
		styles.Focused.BlurredButton = styles.Focused.BlurredButton.Foreground(p.FgBase).Background(p.BgCodeInline)
		styles.Focused.TextInput.Cursor = styles.Focused.TextInput.Cursor.Foreground(p.Primary)
		styles.Focused.TextInput.Placeholder = styles.Focused.TextInput.Placeholder.Foreground(p.FgDim)
		styles.Focused.TextInput.Prompt = styles.Focused.TextInput.Prompt.Foreground(p.Primary)

		styles.Blurred = styles.Focused
		styles.Blurred.Base = styles.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
		styles.Blurred.Title = styles.Blurred.Title.Foreground(p.FgSubtle).Bold(false)
		styles.Blurred.NoteTitle = styles.Blurred.NoteTitle.Foreground(p.FgSubtle).Bold(false)
		return styles
	})
}
