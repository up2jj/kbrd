package model

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"kbrd/frontmatter"
)

// frontmatterSubmitMsg carries a finished key edit back to the Board. When
// Delete is false the key is set to Value (a verbatim YAML scalar handed to
// frontmatter.Set); when Delete is true the key is removed via
// frontmatter.Delete and Value is ignored.
type frontmatterSubmitMsg struct {
	Target   itemRefStable
	ColIndex int
	FileName string
	Key      string
	Value    string
	Delete   bool
}

// frontmatterKnownKeys are the display keys the loader understands; they seed
// the key field's completion alongside whatever keys the card already carries.
var frontmatterKnownKeys = []string{"accent", "icon", "meta", "tags", "render", "pinned"}

// frontmatterValueSuggestions are common scalar values offered as completions in
// the value field (ctrl+e) — handy for boolean-ish keys like `pinned`. Any other
// text is still accepted freely.
var frontmatterValueSuggestions = []string{"true", "false", "yes", "no"}

const (
	fmKeyField   = "key"
	fmValueField = "value"
)

// frontmatterStage tracks which screen of the two-stage editor is showing.
type frontmatterStage int

const (
	feNone  frontmatterStage = iota
	feKey                    // entering the key (Input with suggestions)
	feValue                  // editing the value (Input seeded from the key)
)

// FrontmatterEditor is the "edit one frontmatter key" overlay: a key Input with
// completion, followed by a value Input pre-filled with that key's current
// value. It is built as two sequential huh forms rather than one — huh fixes a
// field's value at build time, so the value field can only be seeded from the
// key after the key form completes.
type FrontmatterEditor struct {
	stage      frontmatterStage
	target     itemRefStable
	colIndex   int
	fileName   string
	data       map[string]any    // the card's parsed frontmatter (lookups + suggestions)
	extraKeys  []string          // board-wide + script-suggested keys for completion
	defaults   map[string]string // script-suggested default value per key
	keyVal     string            // bound to the key Input
	valueVal   string            // bound to the value Input
	form       *huh.Form
	escArmed   bool   // first esc pressed; the next one cancels
	nestedNote string // set when a chosen key resolves to a nested map (uneditable here)
	palette    Palette
	width      int
	height     int
}

func (e *FrontmatterEditor) Active() bool { return e.stage != feNone }

func (e *FrontmatterEditor) Close() {
	e.stage = feNone
	e.target = itemRefStable{}
	e.data = nil
	e.extraKeys = nil
	e.defaults = nil
	e.keyVal = ""
	e.valueVal = ""
	e.form = nil
	e.escArmed = false
	e.nestedNote = ""
}

func (e *FrontmatterEditor) SetPalette(p Palette) { e.palette = p }

// SetSize records the window size and re-fits an active form into it.
func (e *FrontmatterEditor) SetSize(w, h int) {
	e.width = w
	e.height = h
	if e.form != nil {
		e.fitForm()
	}
}

func (e *FrontmatterEditor) fitForm() {
	e.form = e.form.
		WithWidth(min(e.width-10, 72)).
		WithHeight(min(e.height-6, 8))
}

// Open starts the editor for the given card. data is the card's parsed
// frontmatter map (nil when the card has none); extraKeys are additional
// completion candidates (board-wide keys + script suggestions) and defaults maps
// a key to a value to seed when the card does not already carry it. Returns the
// key form's Init cmd.
func (e *FrontmatterEditor) Open(target itemRefStable, colIndex int, fileName string, data map[string]any, extraKeys []string, defaults map[string]string) tea.Cmd {
	e.target = target
	e.colIndex = colIndex
	e.fileName = fileName
	e.data = data
	e.extraKeys = extraKeys
	e.defaults = defaults
	e.keyVal = ""
	e.valueVal = ""
	e.nestedNote = ""
	return e.startKeyForm()
}

// startKeyForm builds stage one: a single Input completing against the union of
// the card's existing keys and the known display keys.
func (e *FrontmatterEditor) startKeyForm() tea.Cmd {
	e.form = huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Key(fmKeyField).
			Title("Frontmatter key").
			Description("type a key — ctrl+e completes an existing one").
			Suggestions(e.suggestKeys()).
			Value(&e.keyVal).
			Validate(huh.ValidateNotEmpty()),
	)).
		WithTheme(huhThemeFor(e.palette)).
		WithShowHelp(false)
	e.fitForm()
	e.stage = feKey
	return e.form.Init()
}

// startValueForm builds stage two: a value Input pre-filled with the key's
// current value; when the card lacks the key, a script-suggested default seeds
// it instead (empty if there is none), editable before submit.
func (e *FrontmatterEditor) startValueForm() tea.Cmd {
	if v, ok := e.data[e.keyVal]; ok {
		e.valueVal = seedValue(v)
	} else {
		e.valueVal = e.defaults[e.keyVal]
	}
	e.form = huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Key(fmValueField).
			Title(e.keyVal).
			Description("value for this key").
			Suggestions(frontmatterValueSuggestions).
			Value(&e.valueVal).
			Validate(func(s string) error { return frontmatter.Validate(e.keyVal, s) }),
	)).
		WithTheme(huhThemeFor(e.palette)).
		WithShowHelp(false)
	e.fitForm()
	e.stage = feValue
	return e.form.Init()
}

// suggestKeys returns the sorted union of the card's existing frontmatter keys,
// the known display keys, and the extra keys gathered at Open (board-wide keys +
// script suggestions), for the key field's tab-completion.
func (e *FrontmatterEditor) suggestKeys() []string {
	set := make(map[string]struct{}, len(e.data)+len(frontmatterKnownKeys)+len(e.extraKeys))
	for k := range e.data {
		set[k] = struct{}{}
	}
	for _, k := range frontmatterKnownKeys {
		set[k] = struct{}{}
	}
	for _, k := range e.extraKeys {
		set[k] = struct{}{}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Update routes messages to the active form. It accepts arbitrary tea.Msg (not
// just keys) because the embedded huh form drives itself with internal messages
// (cursor blink, suggestion updates).
func (e *FrontmatterEditor) Update(msg tea.Msg) tea.Cmd {
	if !e.Active() {
		return nil
	}

	// Cancelling needs a double esc: the first arms (and still reaches huh, so
	// field-level esc bindings keep working), the second closes. Any other key
	// disarms. Mirrors TemplateFlow.
	if k, ok := msg.(tea.KeyMsg); ok {
		if k.String() == "esc" {
			if e.escArmed {
				e.Close()
				return nil
			}
			e.escArmed = true
		} else {
			e.escArmed = false
		}
	}

	// On the value stage the key is already chosen, so ctrl+d removes it
	// outright. Intercepted before the form so the value Input never sees it.
	if e.stage == feValue {
		if k, ok := msg.(tea.KeyMsg); ok && k.String() == "ctrl+d" {
			out := newStableFrontmatterSubmitMsg(e.target, e.colIndex, e.fileName, e.keyVal, "", true)
			e.Close()
			return func() tea.Msg { return out }
		}
	}

	model, cmd := e.form.Update(msg)
	if f, ok := model.(*huh.Form); ok {
		e.form = f
	}

	// Check completion immediately and DROP huh's returned cmd on a terminal
	// state: it queues tea.Quit, which would kill the whole app.
	switch e.form.State {
	case huh.StateCompleted:
		switch e.stage {
		case feKey:
			if strings.TrimSpace(e.keyVal) == "" {
				e.Close()
				return nil
			}
			e.keyVal = strings.TrimSpace(e.keyVal)
			if isNestedValue(e.data[e.keyVal]) {
				// frontmatter.Set only rewrites top-level lines, so a nested
				// mapping can't be edited here. Re-prompt with a note instead
				// of advancing to a value stage that would corrupt it.
				e.nestedNote = e.keyVal + " is a nested mapping — edit it in the file directly"
				e.keyVal = ""
				return e.startKeyForm()
			}
			e.nestedNote = ""
			return e.startValueForm()
		case feValue:
			out := newStableFrontmatterSubmitMsg(e.target, e.colIndex, e.fileName, e.keyVal, e.valueVal, false)
			e.Close()
			return func() tea.Msg { return out }
		}
	case huh.StateAborted: // ctrl+c inside the form
		e.Close()
		return nil
	}
	return cmd
}

func (e *FrontmatterEditor) View() string {
	if !e.Active() {
		return ""
	}
	hints := []Shortcut{
		{Keys: "ctrl+e", Label: "complete"},
		{Keys: "enter", Label: "next"},
		{Keys: "esc esc", Label: "cancel"},
	}
	if e.stage == feValue {
		hints = []Shortcut{
			{Keys: "enter", Label: "save"},
			{Keys: "ctrl+e", Label: "complete"},
			{Keys: "ctrl+d", Label: "remove key"},
			{Keys: "esc esc", Label: "cancel"},
		}
	}
	footer := RenderInlineHints(hints)
	if e.nestedNote != "" {
		footer = lipgloss.NewStyle().Foreground(e.palette.Warning).Italic(true).
			Render(e.nestedNote)
	}
	if e.escArmed {
		footer = lipgloss.NewStyle().Foreground(e.palette.Warning).Italic(true).
			Render("press esc again to cancel")
	}
	fields := e.renderCurrentFields(min(e.width-10, 72))
	body := lipgloss.JoinVertical(lipgloss.Left, fields, "", e.form.View())
	return OverlayFrame{Title: "Edit frontmatter", Body: body, Footer: footer, Palette: e.palette}.Render()
}

// renderCurrentFields lists the card's existing frontmatter as aligned
// `key  value` rows for context. Returns a dim placeholder when the card has
// none. innerW bounds value truncation so rows never wrap the overlay.
func (e *FrontmatterEditor) renderCurrentFields(innerW int) string {
	label := helpDimStyle.Render("current frontmatter")
	if len(e.data) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, label,
			helpDimStyle.Render("  (none yet)"))
	}
	keys := make([]string, 0, len(e.data))
	for k := range e.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	keyW := 0
	for _, k := range keys {
		if w := lipgloss.Width(k); w > keyW {
			keyW = w
		}
	}
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(e.palette.FgBase)
	activeKeyStyle := lipgloss.NewStyle().Bold(true).Foreground(e.palette.Primary)
	valStyle := lipgloss.NewStyle().Foreground(e.palette.FgMuted)
	rows := make([]string, 0, len(keys)+1)
	rows = append(rows, label)
	for _, k := range keys {
		ks := keyStyle
		if e.stage == feValue && k == e.keyVal {
			ks = activeKeyStyle // the key being edited
		}
		padded := fmt.Sprintf("%-*s", keyW, k)
		var val string
		if isNestedValue(e.data[k]) {
			// Nested mappings are read-only here (see Update); show them dimmed
			// so the panel doesn't imply they can be edited.
			val = helpDimStyle.Render(nestedSummary(e.data[k]))
		} else {
			val = valStyle.Render(truncLine(seedValue(e.data[k]), max(innerW-keyW-2, 4)))
		}
		rows = append(rows, ks.Render(padded)+"  "+val)
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// isNestedValue reports whether v is a nested mapping — a structure the flat,
// line-based frontmatter.Set cannot edit, so the editor surfaces it read-only.
func isNestedValue(v any) bool {
	switch v.(type) {
	case map[string]any, map[any]any:
		return true
	}
	return false
}

// nestedSummary describes a nested mapping value for the read-only panel row.
func nestedSummary(v any) string {
	n := 0
	switch m := v.(type) {
	case map[string]any:
		n = len(m)
	case map[any]any:
		n = len(m)
	}
	if n == 1 {
		return "{1 field — edit in file}"
	}
	return fmt.Sprintf("{%d fields — edit in file}", n)
}

// seedValue renders a frontmatter value back into the editable scalar the value
// field starts with. frontmatter.Set writes it verbatim, so the result must be
// a valid YAML scalar. A nil value (absent key) yields "". Sequences become a
// flow list so they round-trip; nested mappings / multi-line values fall back to
// fmt.Sprint and are best-effort (see the editor's scope note).
func seedValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = fmt.Sprint(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprint(t)
	}
}

type boardFrontmatterActions struct {
	board *Board
}

func (b *Board) frontmatterActions() boardFrontmatterActions {
	return boardFrontmatterActions{board: b}
}

// openEditor gathers completion sources and opens the editor on the selected
// card.
func (a boardFrontmatterActions) openEditor(colIndex int, col *Column, item *Item) tea.Cmd {
	b := a.board
	keys, defaults := a.keySources(col.Name, item.Name)
	return b.frontmatterEdit.Open(refForItem(col, item), colIndex, item.Name, item.Data, keys, defaults)
}

// keySources collects key-completion candidates for the editor: the
// union of every key already present on any card across the board, plus any keys
// a frontmatter_suggestions Lua hook offers, along with that hook's per-key
// default values. Keys come from already-parsed Item.Data held in memory, so no
// filesystem scan (ripgrep) is needed — it is a cheap map-key sweep.
func (a boardFrontmatterActions) keySources(colName, itemName string) (keys []string, defaults map[string]string) {
	b := a.board
	set := map[string]struct{}{}
	for _, col := range b.columns {
		for _, k := range col.FrontmatterKeys() {
			set[k] = struct{}{}
		}
	}
	defaults = map[string]string{}
	if b.scripts != nil {
		res := b.scripts.FireFrontmatterSuggestions(colName, itemName)
		for _, s := range res.Suggestions {
			set[s.Key] = struct{}{}
			if s.Default != "" {
				defaults[s.Key] = s.Default
			}
		}
	}
	keys = make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, defaults
}

// handleSubmit writes the edited key/value back to the card and refreshes the
// column.
func (a boardFrontmatterActions) handleSubmit(msg frontmatterSubmitMsg) (tea.Model, tea.Cmd) {
	b := a.board
	target := msg.Target
	if target.FileName == "" {
		target.FileName = msg.FileName
	}
	col, item, err := b.resolveDelayedItemRef(target)
	if err != nil {
		return b, b.notifier.Send(err.Error(), notifyError)
	}
	if msg.Delete {
		if err := b.deleteFrontmatter(col, item.Name, msg.Key); err != nil {
			return b, b.notifier.Send("failed to remove "+msg.Key+": "+err.Error(), notifyError)
		}
		col.SelectByName(item.Name)
		return b, b.notifier.Send("removed "+msg.Key, notifySuccess)
	}
	if err := b.setFrontmatter(col, item.Name, msg.Key, msg.Value); err != nil {
		return b, b.notifier.Send("failed to set "+msg.Key+": "+err.Error(), notifyError)
	}
	col.SelectByName(item.Name)
	return b, b.notifier.Send("set "+msg.Key, notifySuccess)
}
