package model

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"

	"kbrd/template"
)

// templateSubmitMsg carries the finished template form back to the Board: the
// chosen template, the column it targets, and the collected field values
// (string for input/text/select, []string for multiselect, bool for confirm,
// plus template.FilenameKey when the template declared no filename).
type templateSubmitMsg struct {
	Column   columnRef
	ColIndex int
	Template template.Template
	Values   map[string]any
}

// templateFlowStage tracks which screen of the flow is showing.
type templateFlowStage int

const (
	tfNone   templateFlowStage = iota
	tfPick                     // choosing what to create
	tfForm                     // filling the huh form
	tfAuthor                   // filling the column-template authoring form
)

type createChoiceKind int

const (
	createChoiceEmpty createChoiceKind = iota
	createChoiceTemplate
	createChoiceAuthorTemplate
)

type createChoice struct {
	Kind     createChoiceKind
	Label    string
	Desc     string
	Template template.Template
}

type createMenuRow struct {
	header   bool
	title    string
	choice   createChoice
	matchIdx []int
}

// createEmptyItemMsg asks the Board to open the existing new-card filename
// prompt for a stable column reference selected from the create menu.
type createEmptyItemMsg struct {
	Column   columnRef
	ColIndex int
}

type templateAuthorValues struct {
	Name     string
	Filename string
	Body     string
}

type templateAuthorSubmitMsg struct {
	Column     columnRef
	ColIndex   int
	Values     templateAuthorValues
	ReopenMenu bool
}

const (
	templateAuthorNameKey     = "name"
	templateAuthorFilenameKey = "filename"
	templateAuthorBodyKey     = "body"
)

// TemplateFlow is the unified create overlay: a grouped, fuzzy-searchable
// create menu followed by embedded huh forms for template use and authoring.
type TemplateFlow struct {
	stage      templateFlowStage
	column     columnRef
	colIndex   int
	templates  []template.Template
	rows       []createMenuRow
	nav        []int
	selected   int
	filtering  bool
	filter     string
	tmpl       template.Template
	author     templateAuthorValues
	form       *huh.Form
	escArmed   bool // first esc pressed; the next one cancels the form
	reopenMenu bool
	palette    Palette
	width      int
	height     int
}

func (t *TemplateFlow) Active() bool { return t.stage != tfNone }

func (t *TemplateFlow) Close() {
	t.stage = tfNone
	t.column = columnRef{}
	t.templates = nil
	t.rows = nil
	t.nav = nil
	t.selected = 0
	t.filtering = false
	t.filter = ""
	t.tmpl = template.Template{}
	t.author = templateAuthorValues{}
	t.form = nil
	t.escArmed = false
	t.reopenMenu = false
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
	if t.width <= 0 || t.height <= 0 {
		return
	}
	t.form = t.form.
		WithWidth(min(t.width-10, 72)).
		WithHeight(min(t.height-6, 24))
}

// Open starts the unified create menu for the given column. Template forms are
// reached by selecting a template; empty-card creation uses the existing editor
// filename prompt via createEmptyItemMsg.
func (t *TemplateFlow) Open(colIndex int, column columnRef, templates []template.Template) tea.Cmd {
	t.colIndex = colIndex
	t.column = column
	t.templates = templates
	t.selected = 0
	t.filtering = false
	t.filter = ""
	t.stage = tfPick
	t.recomputeMenu()
	return nil
}

func (t *TemplateFlow) OpenTemplate(colIndex int, column columnRef, tmpl template.Template) tea.Cmd {
	t.colIndex = colIndex
	t.column = column
	t.templates = nil
	t.selected = 0
	t.filtering = false
	t.filter = ""
	t.stage = tfForm
	t.reopenMenu = false
	return t.startForm(tmpl)
}

func (t *TemplateFlow) OpenAuthor(colIndex int, column columnRef, reopenMenu bool) tea.Cmd {
	t.colIndex = colIndex
	t.column = column
	t.templates = nil
	t.selected = 0
	t.filtering = false
	t.filter = ""
	t.reopenMenu = reopenMenu
	return t.startAuthorForm()
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
		msg := newStableTemplateSubmitMsg(t.column, t.colIndex, tmpl, map[string]any{})
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

func (t *TemplateFlow) startAuthorForm() tea.Cmd {
	t.author = templateAuthorValues{
		Filename: "{{slug .title}}",
		Body:     "# {{.title}}",
	}

	t.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key(templateAuthorNameKey).
				Title("Template name").
				Description("display name shown in this column's create menu").
				Placeholder("Bug report").
				Value(&t.author.Name).
				Validate(huh.ValidateNotEmpty()),
			huh.NewInput().
				Key(templateAuthorFilenameKey).
				Title("Card filename pattern").
				Description("Go template for new card filenames").
				Value(&t.author.Filename).
				Validate(huh.ValidateNotEmpty()),
		).Title("Template"),
		huh.NewGroup(
			huh.NewText().
				Key(templateAuthorBodyKey).
				Title("Card body starter").
				Description("Markdown body for cards created from this template").
				Lines(6).
				Value(&t.author.Body).
				Validate(huh.ValidateNotEmpty()),
		).Title("Starter body"),
	).
		WithTheme(huhThemeFor(t.palette)).
		WithShowHelp(false)
	t.fitForm()
	t.stage = tfAuthor
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
		if k, ok := msg.(tea.KeyPressMsg); ok {
			return t.updatePicker(k)
		}
		return nil
	case tfForm:
		return t.updateForm(msg)
	case tfAuthor:
		return t.updateAuthorForm(msg)
	}
	return nil
}

func (t *TemplateFlow) updatePicker(msg tea.KeyPressMsg) tea.Cmd {
	if t.filtering {
		switch msg.Code {
		case tea.KeyEsc:
			t.stopFilter()
		case tea.KeyEnter:
			return t.runSelectedChoice()
		case tea.KeyBackspace:
			t.backspaceFilter()
		default:
			if msg.Text != "" {
				t.appendFilter(msg.Text)
			} else {
				t.updateMenuSelection(msg)
			}
		}
		return nil
	}

	switch {
	case key.Matches(msg, Keys.SwitcherClose) || msg.String() == "q":
		t.Close()
	case msg.String() == "/":
		t.startFilter()
	case key.Matches(msg, Keys.SwitcherConfirm):
		return t.runSelectedChoice()
	default:
		t.updateMenuSelection(msg)
	}
	return nil
}

func (t *TemplateFlow) recomputeMenu() {
	t.rows = t.rows[:0]
	t.nav = t.nav[:0]

	if t.filtering {
		choices := t.menuChoices()
		matches := filterFuzzy(len(choices), t.filter, func(i int) string {
			c := choices[i]
			if c.Desc != "" {
				return c.Label + "  " + c.Desc
			}
			return c.Label
		})
		for _, mt := range matches {
			t.rows = append(t.rows, createMenuRow{choice: choices[mt.Index], matchIdx: mt.MatchedIndexes})
			t.nav = append(t.nav, len(t.rows)-1)
		}
	} else {
		t.appendMenuGroup("Create", []createChoice{emptyCreateChoice()})
		t.appendMenuGroup("Template authoring", []createChoice{authorTemplateChoice()})
		t.appendMenuGroup("Column templates", t.templateChoices(template.ScopeColumn))
		t.appendMenuGroup("Board templates", t.templateChoices(template.ScopeBoard))
	}

	t.selected = min(max(t.selected, 0), max(len(t.nav)-1, 0))
}

func (t *TemplateFlow) appendMenuGroup(title string, choices []createChoice) {
	if len(choices) == 0 {
		return
	}
	t.rows = append(t.rows, createMenuRow{header: true, title: title})
	for _, choice := range choices {
		t.rows = append(t.rows, createMenuRow{choice: choice})
		t.nav = append(t.nav, len(t.rows)-1)
	}
}

func (t *TemplateFlow) menuChoices() []createChoice {
	choices := []createChoice{emptyCreateChoice(), authorTemplateChoice()}
	choices = append(choices, t.templateChoices(template.ScopeColumn)...)
	choices = append(choices, t.templateChoices(template.ScopeBoard)...)
	return choices
}

func (t *TemplateFlow) templateChoices(scope string) []createChoice {
	var choices []createChoice
	for _, tmpl := range t.templates {
		if tmpl.Scope != scope {
			continue
		}
		desc := "Template from this column"
		if scope == template.ScopeBoard {
			desc = "Board template"
		}
		choices = append(choices, createChoice{
			Kind:     createChoiceTemplate,
			Label:    tmpl.Name,
			Desc:     desc,
			Template: tmpl,
		})
	}
	return choices
}

func emptyCreateChoice() createChoice {
	return createChoice{
		Kind:  createChoiceEmpty,
		Label: "Empty Markdown file",
		Desc:  "Create an empty .md card in this column",
	}
}

func authorTemplateChoice() createChoice {
	return createChoice{
		Kind:  createChoiceAuthorTemplate,
		Label: "New column template",
		Desc:  "Create a reusable template for this column",
	}
}

func (t *TemplateFlow) startFilter() {
	t.filtering = true
	t.filter = ""
	t.selected = 0
	t.recomputeMenu()
}

func (t *TemplateFlow) stopFilter() {
	t.filtering = false
	t.filter = ""
	t.selected = 0
	t.recomputeMenu()
}

func (t *TemplateFlow) appendFilter(s string) {
	if s == "" {
		return
	}
	t.filter += s
	t.selected = 0
	t.recomputeMenu()
}

func (t *TemplateFlow) backspaceFilter() {
	if r := []rune(t.filter); len(r) > 0 {
		t.filter = string(r[:len(r)-1])
		t.selected = 0
		t.recomputeMenu()
		return
	}
	t.stopFilter()
}

func (t *TemplateFlow) updateMenuSelection(msg tea.KeyPressMsg) {
	if len(t.nav) == 0 {
		return
	}
	switch msg.String() {
	case "down", "j", "ctrl+n", "tab":
		t.selected = min(t.selected+1, len(t.nav)-1)
	case "up", "k", "ctrl+p", "shift+tab":
		t.selected = max(t.selected-1, 0)
	case "g", "home":
		t.selected = 0
	case "G", "end":
		t.selected = len(t.nav) - 1
	case "pgdown", "ctrl+d":
		t.selected = min(t.selected+10, len(t.nav)-1)
	case "pgup", "ctrl+u":
		t.selected = max(t.selected-10, 0)
	}
}

func (t *TemplateFlow) selectedChoice() (createChoice, bool) {
	if t.selected < 0 || t.selected >= len(t.nav) {
		return createChoice{}, false
	}
	row := t.rows[t.nav[t.selected]]
	if row.header {
		return createChoice{}, false
	}
	return row.choice, true
}

func (t *TemplateFlow) runSelectedChoice() tea.Cmd {
	choice, ok := t.selectedChoice()
	if !ok {
		return nil
	}
	switch choice.Kind {
	case createChoiceEmpty:
		msg := createEmptyItemMsg{Column: t.column, ColIndex: t.colIndex}
		t.Close()
		return func() tea.Msg { return msg }
	case createChoiceTemplate:
		return t.startForm(choice.Template)
	case createChoiceAuthorTemplate:
		t.reopenMenu = false
		return t.startAuthorForm()
	}
	return nil
}

func (t *TemplateFlow) updateHuhForm(msg tea.Msg) (tea.Cmd, bool) {
	if t.form == nil {
		return nil, false
	}
	// Cancelling a half-filled form needs a double esc: the first arms (and
	// still reaches huh, so field-level esc bindings like clearing a select
	// filter keep working), the second — pressed immediately after — closes.
	// Any other key disarms.
	if k, ok := msg.(tea.KeyPressMsg); ok {
		if k.String() == "esc" {
			if t.escArmed {
				t.Close()
				return nil, false
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
	return cmd, t.form != nil
}

func (t *TemplateFlow) updateForm(msg tea.Msg) tea.Cmd {
	cmd, active := t.updateHuhForm(msg)
	if !active {
		return cmd
	}

	// Check completion immediately and DROP huh's returned cmd on a terminal
	// state: it queues tea.Quit, which would kill the whole app.
	switch t.form.State {
	case huh.StateCompleted:
		values := t.collectValues()
		out := newStableTemplateSubmitMsg(t.column, t.colIndex, t.tmpl, values)
		t.Close()
		return func() tea.Msg { return out }
	case huh.StateAborted: // ctrl+c inside the form
		t.Close()
		return nil
	}
	return cmd
}

func (t *TemplateFlow) updateAuthorForm(msg tea.Msg) tea.Cmd {
	cmd, active := t.updateHuhForm(msg)
	if !active {
		return cmd
	}
	switch t.form.State {
	case huh.StateCompleted:
		return t.finishAuthorForm()
	case huh.StateAborted:
		t.Close()
		return nil
	}
	return cmd
}

func (t *TemplateFlow) finishAuthorForm() tea.Cmd {
	out := t.authorSubmitMsg()
	t.Close()
	return func() tea.Msg { return out }
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

func (t *TemplateFlow) authorSubmitMsg() templateAuthorSubmitMsg {
	return newStableTemplateAuthorSubmitMsg(t.column, t.colIndex, templateAuthorValues{
		Name:     strings.TrimSpace(t.author.Name),
		Filename: strings.TrimSpace(t.author.Filename),
		Body:     strings.TrimSpace(t.author.Body),
	}, t.reopenMenu)
}

func (t *TemplateFlow) View() string {
	switch t.stage {
	case tfPick:
		return t.viewPicker()
	case tfForm, tfAuthor:
		footer := RenderInlineHints([]Shortcut{
			{Keys: "tab/enter", Label: "next"},
			{Keys: "shift+tab", Label: "back"},
			{Keys: "esc esc", Label: "cancel"},
		})
		if t.escArmed {
			footer = lipgloss.NewStyle().Foreground(t.palette.Warning).Italic(true).
				Render("press esc again to cancel")
		}
		title := t.tmpl.Name
		if t.stage == tfAuthor {
			title = "New column template"
		}
		return OverlayFrame{Title: title, Body: t.form.View(), Footer: footer, Palette: t.palette}.Render()
	}
	return ""
}

func (t *TemplateFlow) viewPicker() string {
	footer := RenderInlineHints([]Shortcut{
		{Keys: "↑/↓", Label: "select"},
		{Keys: "/", Label: "search"},
		{Keys: "enter", Label: "create"},
		{Keys: "esc/q", Label: "cancel"},
	})
	if t.filtering {
		footer = RenderInlineHints([]Shortcut{
			{Keys: "type", Label: "filter"},
			{Keys: "↑/↓", Label: "select"},
			{Keys: "enter", Label: "create"},
			{Keys: "esc", Label: "clear"},
		})
	}
	contentW := t.createMenuContentWidth(footer)
	body := t.viewCreateMenuBody(contentW)
	return OverlayFrame{
		Title:   "Create item",
		Body:    body,
		Footer:  footer,
		Width:   overlayWidthForBody(contentW),
		Palette: t.palette,
	}.Render()
}

func (t *TemplateFlow) viewCreateMenuBody(contentW int) string {
	p := t.palette
	nameStyle := lipgloss.NewStyle().Foreground(p.FgBase)
	descStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	hiStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight)

	var lines []string
	if t.filtering {
		cursor := hiStyle.Render("> ")
		query := t.filter
		if query == "" {
			query = descStyle.Render("type to filter…")
		} else {
			query = nameStyle.Render(query)
		}
		lines = append(lines, cursor+query, "")
	}

	selRow := -1
	if t.selected < len(t.nav) {
		selRow = t.nav[t.selected]
	}
	for i, row := range t.rows {
		lines = append(lines, t.renderCreateMenuRow(row, i == selRow))
	}
	if len(t.nav) == 0 {
		lines = append(lines, helpDimStyle.Render("no matches"))
	}

	for i, line := range lines {
		if lipgloss.Width(line) > contentW {
			lines[i] = ansi.Truncate(line, contentW, "…")
		}
	}
	return lipgloss.NewStyle().Width(contentW).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (t *TemplateFlow) createMenuContentWidth(footer string) int {
	contentW := lipgloss.Width(footer)
	for _, line := range t.createMenuSizingLines() {
		contentW = max(contentW, lipgloss.Width(line))
	}
	minInner := 50
	if t.width > 0 && t.width-12 < minInner {
		minInner = t.width - 12
	}
	contentW = max(contentW, minInner)
	if t.width > 0 {
		contentW = min(contentW, max(t.width-8, 1))
	}
	return max(contentW, 1)
}

func (t *TemplateFlow) createMenuSizingLines() []string {
	var lines []string
	if t.filtering {
		query := t.filter
		if query == "" {
			query = "type to filter…"
		}
		lines = append(lines, "> "+query)
	}
	for _, row := range t.rows {
		lines = append(lines, t.renderCreateMenuRow(row, false), t.renderCreateMenuRow(row, true))
	}
	if len(t.nav) == 0 {
		lines = append(lines, "no matches")
	}
	return lines
}

func (t *TemplateFlow) renderCreateMenuRow(row createMenuRow, selected bool) string {
	p := t.palette
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Primary)
	nameStyle := lipgloss.NewStyle().Foreground(p.FgBase)
	descStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	hiStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight)
	hiSelStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Background(p.Primary)
	gutterSel := lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("▌")

	if row.header {
		return headerStyle.Render("── " + row.title + " ──")
	}
	labelIdx, descIdx := splitLabelDescMatchIndexes(row.choice.Label, row.matchIdx)
	labelBase, descBase := nameStyle, descStyle
	hiLabel, hiDesc := hiStyle, hiStyle
	if selected {
		labelBase, descBase = selStyle, selStyle
		hiLabel, hiDesc = hiSelStyle, hiSelStyle
	}
	styled := renderHighlighted(row.choice.Label, labelIdx, labelBase, hiLabel)
	if row.choice.Desc != "" {
		sep := "  —  "
		if selected {
			styled += selStyle.Render(sep)
		} else {
			styled += descStyle.Render(sep)
		}
		styled += renderHighlighted(row.choice.Desc, descIdx, descBase, hiDesc)
	}
	gutter := " "
	if selected {
		gutter = gutterSel
		styled = selStyle.Render(" ") + styled + selStyle.Render(" ")
	}
	return gutter + " " + styled
}

// huhThemeFor maps the app palette onto a huh theme so embedded forms match
// the rest of the UI in both light and dark modes.
func huhThemeFor(p Palette) huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		t := huh.ThemeBase(isDark)

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
	})
}
