package model

import (
	"fmt"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"kbrd/template"
)

// templateSubmitMsg carries the finished template form back to the Board: the
// chosen template, the column it targets, and the collected field values
// (string for input/text/select, []string for multiselect, bool for confirm,
// plus template.FilenameKey when the template declared no filename).
type templateSubmitMsg struct {
	ColIndex int
	Template template.Template
	Values   map[string]any
}

// templateFlowStage tracks which screen of the flow is showing.
type templateFlowStage int

const (
	tfNone templateFlowStage = iota
	tfPick                   // choosing a template
	tfForm                   // filling the huh form
)

// TemplateFlow is the "new item from template" overlay: a template picker
// followed by an embedded huh form, one form page per template step.
type TemplateFlow struct {
	stage     templateFlowStage
	colIndex  int
	templates []template.Template
	selected  int
	tmpl      template.Template
	form      *huh.Form
	escArmed  bool // first esc pressed; the next one cancels the form
	palette   Palette
	width     int
	height    int
}

func (t *TemplateFlow) Active() bool { return t.stage != tfNone }

func (t *TemplateFlow) Close() {
	t.stage = tfNone
	t.templates = nil
	t.selected = 0
	t.tmpl = template.Template{}
	t.form = nil
	t.escArmed = false
}

func (t *TemplateFlow) SetPalette(p Palette) { t.palette = p }

// SetSize records the window size and re-fits an active form into it.
func (t *TemplateFlow) SetSize(w, h int) {
	t.width = w
	t.height = h
	if t.form != nil {
		t.fitForm()
	}
}

func (t *TemplateFlow) fitForm() {
	t.form = t.form.
		WithWidth(min(t.width-10, 72)).
		WithHeight(min(t.height-6, 24))
}

// Open starts the flow for the given column. With a single template the
// picker is skipped. Returns the form's Init cmd when one starts immediately.
func (t *TemplateFlow) Open(colIndex int, templates []template.Template) tea.Cmd {
	t.colIndex = colIndex
	t.templates = templates
	t.selected = 0
	if len(templates) == 1 {
		return t.startForm(templates[0])
	}
	t.stage = tfPick
	return nil
}

// startForm builds the huh form for tmpl: one group per step, plus a
// synthetic filename group when the template declares no filename template.
// A template with no fields at all skips the form and submits directly.
func (t *TemplateFlow) startForm(tmpl template.Template) tea.Cmd {
	t.tmpl = tmpl

	var groups []*huh.Group
	for _, step := range tmpl.Steps {
		fields := make([]huh.Field, 0, len(step.Fields))
		for _, f := range step.Fields {
			fields = append(fields, buildField(f))
		}
		if len(fields) == 0 {
			continue
		}
		g := huh.NewGroup(fields...)
		if step.Title != "" {
			g = g.Title(step.Title)
		}
		groups = append(groups, g)
	}
	if tmpl.Filename == "" {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().
				Key(template.FilenameKey).
				Title("Filename").
				Description("name for the new card (without .md)").
				Validate(huh.ValidateNotEmpty()),
		))
	}
	if len(groups) == 0 {
		// Nothing to ask: render and create straight away.
		msg := templateSubmitMsg{ColIndex: t.colIndex, Template: tmpl, Values: map[string]any{}}
		t.Close()
		return func() tea.Msg { return msg }
	}

	// huh's built-in help line is replaced by our own footer in View so the
	// form matches the picker/dialog hint style exactly.
	t.form = huh.NewForm(groups...).
		WithTheme(huhThemeFor(t.palette)).
		WithShowHelp(false)
	t.fitForm()
	t.stage = tfForm
	return t.form.Init()
}

// buildField maps one declared template field onto a huh field.
func buildField(f template.Field) huh.Field {
	switch f.Type {
	case "text":
		v := new(string)
		*v = fieldSeed(f)
		// Validator covers required + pattern/length, shared with the Lua path.
		return huh.NewText().Key(f.Key).Title(f.Title).Description(f.Description).
			Placeholder(f.Placeholder).Lines(4).Value(v).Validate(f.Validator())
	case "select":
		v := new(string)
		*v = f.Default
		return huh.NewSelect[string]().Key(f.Key).Title(f.Title).Description(f.Description).
			Options(huh.NewOptions(f.Options...)...).Value(v)
	case "multiselect":
		v := new([]string)
		if f.Default != "" {
			*v = []string{f.Default}
		}
		ms := huh.NewMultiSelect[string]().Key(f.Key).Title(f.Title).Description(f.Description).
			Options(huh.NewOptions(f.Options...)...).Value(v)
		if f.Required {
			ms = ms.Validate(func(sel []string) error {
				if len(sel) == 0 {
					return fmt.Errorf("select at least one option")
				}
				return nil
			})
		}
		return ms
	case "confirm":
		v := new(bool)
		*v = f.Default == "true"
		return huh.NewConfirm().Key(f.Key).Title(f.Title).Description(f.Description).Value(v)
	case "note":
		return huh.NewNote().Title(f.Title).Description(f.Description)
	default: // "input" (types are validated at parse time)
		v := new(string)
		*v = fieldSeed(f)
		// Validator covers required + pattern/length, shared with the Lua path.
		return huh.NewInput().Key(f.Key).Title(f.Title).Description(f.Description).
			Placeholder(f.Placeholder).Value(v).Validate(f.Validator())
	}
}

// fieldSeed returns the initial value an input/text field starts with: its
// default, or — for prefill: clipboard — the system clipboard's content. The
// prefilled value lands in the visible form field where the user can edit or
// clear it before submitting; templates can never read the clipboard at
// render time. A failed clipboard read (headless session, empty clipboard)
// degrades to an empty field.
func fieldSeed(f template.Field) string {
	if f.Prefill == template.PrefillClipboard {
		if s, err := clipboard.ReadAll(); err == nil {
			return s
		}
		return ""
	}
	return f.Default
}

// Update routes messages for whichever stage is active. It accepts arbitrary
// tea.Msg (not just keys) because the embedded huh form drives itself with
// internal messages (cursor blink, group transitions).
func (t *TemplateFlow) Update(msg tea.Msg) tea.Cmd {
	switch t.stage {
	case tfPick:
		if k, ok := msg.(tea.KeyMsg); ok {
			return t.updatePicker(k)
		}
		return nil
	case tfForm:
		return t.updateForm(msg)
	}
	return nil
}

func (t *TemplateFlow) updatePicker(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, Keys.SwitcherClose):
		t.Close()
	case key.Matches(msg, Keys.SwitcherPrev):
		if t.selected > 0 {
			t.selected--
		}
	case key.Matches(msg, Keys.SwitcherNext):
		if t.selected < len(t.templates)-1 {
			t.selected++
		}
	case key.Matches(msg, Keys.SwitcherConfirm):
		if len(t.templates) == 0 {
			t.Close()
			return nil
		}
		return t.startForm(t.templates[t.selected])
	}
	return nil
}

func (t *TemplateFlow) updateForm(msg tea.Msg) tea.Cmd {
	// Cancelling a half-filled form needs a double esc: the first arms (and
	// still reaches huh, so field-level esc bindings like clearing a select
	// filter keep working), the second — pressed immediately after — closes.
	// Any other key disarms.
	if k, ok := msg.(tea.KeyMsg); ok {
		if k.String() == "esc" {
			if t.escArmed {
				t.Close()
				return nil
			}
			t.escArmed = true
		} else {
			t.escArmed = false
		}
	}

	model, cmd := t.form.Update(msg)
	if f, ok := model.(*huh.Form); ok {
		t.form = f
	}

	// Check completion immediately and DROP huh's returned cmd on a terminal
	// state: it queues tea.Quit, which would kill the whole app.
	switch t.form.State {
	case huh.StateCompleted:
		values := t.collectValues()
		out := templateSubmitMsg{ColIndex: t.colIndex, Template: t.tmpl, Values: values}
		t.Close()
		return func() tea.Msg { return out }
	case huh.StateAborted: // ctrl+c inside the form
		t.Close()
		return nil
	}
	return cmd
}

// collectValues reads every declared field back out of the completed form.
func (t *TemplateFlow) collectValues() map[string]any {
	values := make(map[string]any)
	for _, step := range t.tmpl.Steps {
		for _, f := range step.Fields {
			switch f.Type {
			case "note":
				// display-only
			case "confirm":
				values[f.Key] = t.form.GetBool(f.Key)
			case "multiselect":
				if sel, ok := t.form.Get(f.Key).([]string); ok {
					values[f.Key] = sel
				} else {
					values[f.Key] = []string{}
				}
			default:
				values[f.Key] = t.form.GetString(f.Key)
			}
		}
	}
	if t.tmpl.Filename == "" {
		values[template.FilenameKey] = t.form.GetString(template.FilenameKey)
	}
	return values
}

func (t *TemplateFlow) View() string {
	switch t.stage {
	case tfPick:
		return t.viewPicker()
	case tfForm:
		footer := RenderInlineHints([]Shortcut{
			{Keys: "tab/enter", Label: "next"},
			{Keys: "shift+tab", Label: "back"},
			{Keys: "esc esc", Label: "cancel"},
		})
		if t.escArmed {
			footer = lipgloss.NewStyle().Foreground(t.palette.Warning).Italic(true).
				Render("press esc again to cancel")
		}
		return OverlayFrame{Title: t.tmpl.Name, Body: t.form.View(), Footer: footer, Palette: t.palette}.Render()
	}
	return ""
}

func (t *TemplateFlow) viewPicker() string {
	labels := make([]string, len(t.templates))
	for i, tmpl := range t.templates {
		label := tmpl.Name
		if tmpl.Scope == template.ScopeBoard {
			label += " (board)"
		}
		labels[i] = label
	}
	body := renderPickerChoices(t.palette, labels, t.selected)
	footer := RenderInlineHints([]Shortcut{
		{Keys: "↑/↓", Label: "select"},
		{Keys: "enter", Label: "confirm"},
		{Keys: "esc", Label: "cancel"},
	})
	return OverlayFrame{Title: "New from template", Body: body, Footer: footer, Palette: t.palette}.Render()
}

// huhThemeFor maps the app palette onto a huh theme so embedded forms match
// the rest of the UI in both light and dark modes.
func huhThemeFor(p Palette) *huh.Theme {
	t := huh.ThemeBase()

	t.Focused.Base = t.Focused.Base.BorderForeground(p.BorderActive)
	t.Focused.Title = t.Focused.Title.Foreground(p.Primary).Bold(true)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(p.Primary).Bold(true)
	t.Focused.Description = t.Focused.Description.Foreground(p.FgMuted)
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(p.Danger)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(p.Danger)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(p.Primary)
	t.Focused.Option = t.Focused.Option.Foreground(p.FgBase)
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(p.Primary)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(p.Primary)
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(p.Success)
	t.Focused.UnselectedOption = t.Focused.UnselectedOption.Foreground(p.FgBase)
	t.Focused.UnselectedPrefix = t.Focused.UnselectedPrefix.Foreground(p.FgDim)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(p.FgOnAccent).Background(p.PrimaryStrong)
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(p.FgBase).Background(p.BgCodeInline)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(p.Primary)
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(p.FgDim)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(p.Primary)

	t.Blurred = t.Focused
	t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Title = t.Blurred.Title.Foreground(p.FgSubtle).Bold(false)
	t.Blurred.NoteTitle = t.Blurred.NoteTitle.Foreground(p.FgSubtle).Bold(false)

	// Match helpDimStyle (the picker/dialog footer): dim italic throughout.
	dim := lipgloss.NewStyle().Foreground(p.FgDim).Italic(true)
	t.Help.Ellipsis = dim
	t.Help.ShortKey = dim
	t.Help.ShortDesc = dim
	t.Help.ShortSeparator = dim
	t.Help.FullKey = dim
	t.Help.FullDesc = dim
	t.Help.FullSeparator = dim

	return t
}
