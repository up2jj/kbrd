package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"kbrd/theme"
	"kbrd/vimbuf"
)

type TextareaOptions struct {
	Title       string
	Initial     string
	Wrap        bool
	LineNumbers bool
	Actions     []Action
}

type TextareaCursor struct {
	Line   int
	Column int
	Offset int
}

type TextareaSelection struct {
	StartOffset int
	EndOffset   int
	Text        string
}

type TextareaResult struct {
	Action    string
	Value     string
	Cursor    TextareaCursor
	Selection *TextareaSelection
	Submitted bool
	Cancelled bool
}

// Textarea is a multiline modal editor backed by vimbuf. Escape is reserved
// for cancelling the scripted modal; ctrl+[ remains available for returning
// from insert or visual mode to normal mode.
type Textarea struct {
	opts    TextareaOptions
	buf     *vimbuf.Buffer
	result  *TextareaResult
	active  bool
	size    Size
	palette theme.Palette
	status  string
}

func (t *Textarea) Open(opts TextareaOptions) {
	t.opts = opts
	t.buf = vimbuf.New(opts.Initial)
	t.buf.SetWrap(opts.Wrap)
	t.buf.SetLineNumbers(opts.LineNumbers)
	t.buf.StartInsert()
	t.result = nil
	t.active = true
	t.status = ""
	t.fit()
}

func (t *Textarea) Active() bool { return t.active }

func (t *Textarea) SetSize(width, height int) {
	t.size.Set(width, height)
	t.fit()
}

func (t *Textarea) SetPalette(p theme.Palette) { t.palette = p }

func (t *Textarea) Update(msg tea.Msg) tea.Cmd {
	if !t.active || t.buf == nil {
		return nil
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
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
			t.status = action.DisabledReason
			if t.status == "" {
				t.status = "This action is disabled"
			}
			return nil
		}
		selection, selected := t.selection()
		if action.RequiresSelection && !selected {
			t.status = "Select text before using " + action.Label
			return nil
		}
		result := TextareaResult{
			Action: action.ID, Value: t.buf.Text(), Cursor: t.cursor(),
			Submitted: true,
		}
		if selected {
			result.Selection = selection
		}
		t.finish(result)
		return nil
	}
	effect := t.buf.HandleKey(keyMsg.String())
	if effect.Status != "" {
		t.status = effect.Status
	} else {
		t.status = ""
	}
	return nil
}

func (t *Textarea) View() string {
	if !t.active || t.buf == nil {
		return ""
	}
	title := t.opts.Title
	if title == "" {
		title = "Textarea"
	}
	mode := lipgloss.NewStyle().Bold(true).Foreground(t.palette.Primary).Render(t.buf.ModeName())
	cur := t.cursor()
	status := fmt.Sprintf("%s  Ln %d, Col %d", mode, cur.Line, cur.Column)
	if t.status != "" {
		status += "  " + lipgloss.NewStyle().Foreground(t.palette.Warning).Render(t.status)
	}
	body := t.buf.View(t.palette) + "\n" + status
	hints := make([]theme.Hint, 0, len(t.opts.Actions)+2)
	for _, action := range t.opts.Actions {
		hints = append(hints, theme.Hint{Keys: action.Key, Label: action.Label})
	}
	hints = append(hints, theme.Hint{Keys: "ctrl+[", Label: "normal"}, theme.Hint{Keys: "esc", Label: "cancel"})
	return theme.OverlayFrame{
		Title: title, Body: body, Footer: theme.RenderHints(t.palette, hints),
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
	t.buf = nil
	t.result = nil
	t.opts = TextareaOptions{}
	t.status = ""
}

func (t *Textarea) finish(result TextareaResult) {
	t.result = &result
	t.active = false
}

func (t *Textarea) fit() {
	if t.buf == nil {
		return
	}
	t.buf.SetSize(max(t.frameWidth()-8, 1), max(t.size.Height-10, 3))
}

func (t *Textarea) frameWidth() int { return max(t.size.Fit(110, 0).Width-2, 20) }

func (t *Textarea) cursor() TextareaCursor {
	pos := t.buf.Cursor()
	return TextareaCursor{Line: pos.Row + 1, Column: pos.Col + 1, Offset: byteOffset(t.buf.Text(), pos)}
}

func (t *Textarea) selection() (*TextareaSelection, bool) {
	selection, ok := t.buf.Selection()
	if !ok {
		return nil, false
	}
	text := t.buf.Text()
	return &TextareaSelection{
		StartOffset: byteOffset(text, selection.Start),
		EndOffset:   byteOffset(text, selection.End),
		Text:        selection.Text,
	}, true
}

func byteOffset(text string, pos vimbuf.Pos) int {
	lines := strings.Split(text, "\n")
	row := min(max(pos.Row, 0), len(lines)-1)
	offset := 0
	for i := 0; i < row; i++ {
		offset += len(lines[i]) + 1
	}
	runes := []rune(lines[row])
	col := min(max(pos.Col, 0), len(runes))
	return offset + len(string(runes[:col]))
}
