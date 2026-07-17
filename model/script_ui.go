package model

import (
	tea "charm.land/bubbletea/v2"

	"kbrd/script"
	"kbrd/tui"
)

// scriptResumeMsg carries the user's response back to the Lua coroutine that
// asked for it.
type scriptResumeMsg struct {
	Name   string
	Token  string
	Result any
}

type scriptUIKind int

const (
	scriptUINone scriptUIKind = iota
	scriptUIInput
	scriptUISelect
	scriptUIMultiSelect
	scriptUIConfirm
	scriptUIActions
	scriptUIForm
	scriptUITextarea
	scriptUIViewer

	// Retain the old internal names while legacy calls migrate to shared controls.
	scriptUIPrompt = scriptUIInput
	scriptUIPick   = scriptUISelect
)

// ScriptUI coordinates one in-flight scripted request and delegates terminal
// behavior to reusable controls in tui.
type ScriptUI struct {
	kind  scriptUIKind
	name  string
	token string

	input      tui.Input
	selectOne  tui.Select
	selectMany tui.MultiSelect
	confirm    tui.Confirm
	actions    tui.Actions
	form       tui.Form
	textarea   tui.Textarea
	viewer     tui.Viewer
	size       tui.Size
	palette    Palette
}

func (s *ScriptUI) SetPalette(p Palette) {
	s.palette = p
	s.input.SetPalette(p)
	s.selectOne.SetPalette(p)
	s.selectMany.SetPalette(p)
	s.confirm.SetPalette(p)
	s.actions.SetPalette(p)
	s.form.SetPalette(p)
	s.textarea.SetPalette(p)
	s.viewer.SetPalette(p)
}

func (s *ScriptUI) Active() bool { return s.kind != scriptUINone }

func (s *ScriptUI) Close() {
	s.input.Close()
	s.selectOne.Close()
	s.selectMany.Close()
	s.confirm.Close()
	s.actions.Close()
	s.form.Close()
	s.textarea.Close()
	s.viewer.Close()
	s.kind = scriptUINone
	s.name = ""
	s.token = ""
}

func (s *ScriptUI) Open(name string, req *script.UIRequest) tea.Cmd {
	s.Close()
	s.name = name
	s.token = req.Token

	switch req.Kind {
	case script.UIKindInput:
		s.kind = scriptUIInput
		s.input.SetPalette(s.palette)
		s.input.SetSize(s.size.Width, s.size.Height)
		s.input.Open(tui.InputOptions{
			Title: req.Spec.Title, Label: req.Spec.Label, Initial: stringValue(req.Spec.Initial),
			Placeholder: req.Spec.Placeholder, Required: req.Spec.Required,
			MinLength: req.Spec.MinLength, MaxLength: req.Spec.MaxLength,
			Pattern: req.Spec.Pattern, PatternHint: req.Spec.PatternHint,
		})
	case script.UIKindSelect:
		s.kind = scriptUISelect
		s.selectOne.SetPalette(s.palette)
		s.selectOne.SetSize(s.size.Width, s.size.Height)
		s.selectOne.Open(tui.SelectOptions{
			Title: req.Spec.Title, Items: selectItems(req.Spec.Items),
			Searchable: req.Spec.Searchable, InitialID: req.Spec.InitialID,
		})
	case script.UIKindMultiSelect:
		s.kind = scriptUIMultiSelect
		s.selectMany.SetPalette(s.palette)
		s.selectMany.SetSize(s.size.Width, s.size.Height)
		s.selectMany.Open(tui.MultiSelectOptions{
			Title: req.Spec.Title, Items: selectItems(req.Spec.Items),
			Searchable: req.Spec.Searchable, InitialIDs: req.Spec.InitialIDs,
		})
	case script.UIKindConfirm:
		s.kind = scriptUIConfirm
		s.confirm.SetPalette(s.palette)
		s.confirm.SetSize(s.size.Width, s.size.Height)
		s.confirm.Open(tui.ConfirmOptions{
			Title: req.Spec.Title, Message: req.Spec.Message, Detail: req.Spec.Detail,
			ConfirmLabel: req.Spec.ConfirmLabel, RejectLabel: req.Spec.RejectLabel,
			Default: req.Spec.Default, Destructive: req.Spec.Destructive,
		})
	case script.UIKindActions:
		s.kind = scriptUIActions
		s.actions.SetPalette(s.palette)
		s.actions.SetSize(s.size.Width, s.size.Height)
		s.actions.Open(tui.ActionsOptions{Title: req.Spec.Title, Actions: actionItems(req.Spec.Actions)})
	case script.UIKindForm:
		s.kind = scriptUIForm
		s.form.SetPalette(s.palette)
		s.form.SetSize(s.size.Width, s.size.Height)
		return s.form.Open(tui.FormOptions{Title: req.Spec.Title, Fields: formFields(req.Spec.Fields)})
	case script.UIKindTextarea:
		s.kind = scriptUITextarea
		s.textarea.SetPalette(s.palette)
		s.textarea.SetSize(s.size.Width, s.size.Height)
		s.textarea.Open(tui.TextareaOptions{
			Title: req.Spec.Title, Initial: stringValue(req.Spec.Initial),
			Wrap: req.Spec.Wrap, LineNumbers: req.Spec.LineNumbers, Actions: actionItems(req.Spec.Actions),
		})
	case script.UIKindViewer:
		s.kind = scriptUIViewer
		s.viewer.SetPalette(s.palette)
		s.viewer.SetSize(s.size.Width, s.size.Height)
		s.viewer.Open(tui.ViewerOptions{
			Title: req.Spec.Title, Content: req.Spec.Content, Format: req.Spec.Format,
			Wrap: req.Spec.Wrap, LineNumbers: req.Spec.LineNumbers, Actions: actionItems(req.Spec.Actions),
		})
	}
	return nil
}

func (s *ScriptUI) MatchesToken(token string) bool {
	return s.Active() && s.token == token
}

func (s *ScriptUI) SetSize(width, height int) {
	s.size.Set(width, height)
	s.input.SetSize(width, height)
	s.selectOne.SetSize(width, height)
	s.selectMany.SetSize(width, height)
	s.confirm.SetSize(width, height)
	s.actions.SetSize(width, height)
	s.form.SetSize(width, height)
	s.textarea.SetSize(width, height)
	s.viewer.SetSize(width, height)
}

func (s *ScriptUI) Update(msg tea.Msg) tea.Cmd {
	if !s.Active() {
		return nil
	}
	var cmd tea.Cmd
	switch s.kind {
	case scriptUIInput:
		cmd = s.input.Update(msg)
		if result, ok := s.input.TakeResult(); ok {
			return s.resume(script.UIResult{Action: resultAction(result.Cancelled), Submitted: result.Submitted, Cancelled: result.Cancelled, Value: result.Value})
		}
	case scriptUISelect:
		cmd = s.selectOne.Update(msg)
		if result, ok := s.selectOne.TakeResult(); ok {
			return s.resume(script.UIResult{Action: resultAction(result.Cancelled), Submitted: result.Submitted, Cancelled: result.Cancelled, Value: result.ID})
		}
	case scriptUIMultiSelect:
		cmd = s.selectMany.Update(msg)
		if result, ok := s.selectMany.TakeResult(); ok {
			return s.resume(script.UIResult{Action: resultAction(result.Cancelled), Submitted: result.Submitted, Cancelled: result.Cancelled, IDs: result.IDs})
		}
	case scriptUIConfirm:
		cmd = s.confirm.Update(msg)
		if result, ok := s.confirm.TakeResult(); ok {
			return s.resume(script.UIResult{Action: resultAction(result.Cancelled), Submitted: result.Submitted, Cancelled: result.Cancelled, Value: result.Value})
		}
	case scriptUIActions:
		cmd = s.actions.Update(msg)
		if result, ok := s.actions.TakeResult(); ok {
			return s.resume(script.UIResult{Action: actionResultAction(result), Submitted: result.Submitted, Cancelled: result.Cancelled, Value: result.ID})
		}
	case scriptUIForm:
		cmd = s.form.Update(msg)
		if result, ok := s.form.TakeResult(); ok {
			return s.resume(script.UIResult{Action: resultAction(result.Cancelled), Submitted: result.Submitted, Cancelled: result.Cancelled, Values: result.Values})
		}
	case scriptUITextarea:
		cmd = s.textarea.Update(msg)
		if result, ok := s.textarea.TakeResult(); ok {
			uiResult := script.UIResult{Action: result.Action, Submitted: result.Submitted, Cancelled: result.Cancelled, Value: result.Value}
			if result.Cancelled {
				uiResult.Action = "cancel"
			} else {
				uiResult.Cursor = &script.CursorPosition{Line: result.Cursor.Line, Column: result.Cursor.Column, Offset: result.Cursor.Offset}
				if result.Selection != nil {
					uiResult.Selection = &script.TextSelection{StartOffset: result.Selection.StartOffset, EndOffset: result.Selection.EndOffset, Text: result.Selection.Text}
				}
			}
			return s.resume(uiResult)
		}
	case scriptUIViewer:
		cmd = s.viewer.Update(msg)
		if result, ok := s.viewer.TakeResult(); ok {
			action := result.Action
			if result.Cancelled {
				action = "cancel"
			}
			return s.resume(script.UIResult{Action: action, Submitted: result.Submitted, Cancelled: result.Cancelled})
		}
	}
	return cmd
}

func (s *ScriptUI) View() string {
	switch s.kind {
	case scriptUIInput:
		return s.input.View()
	case scriptUISelect:
		return s.selectOne.View()
	case scriptUIMultiSelect:
		return s.selectMany.View()
	case scriptUIConfirm:
		return s.confirm.View()
	case scriptUIActions:
		return s.actions.View()
	case scriptUIForm:
		return s.form.View()
	case scriptUITextarea:
		return s.textarea.View()
	case scriptUIViewer:
		return s.viewer.View()
	default:
		return ""
	}
}

func (s *ScriptUI) resume(result script.UIResult) tea.Cmd {
	name, token := s.name, s.token
	s.Close()
	return func() tea.Msg {
		return scriptResumeMsg{Name: name, Token: token, Result: result}
	}
}

func resultAction(cancelled bool) string {
	if cancelled {
		return "cancel"
	}
	return "submit"
}

func actionResultAction(result tui.ActionResult) string {
	if result.Cancelled {
		return "cancel"
	}
	return result.ID
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func selectItems(items []script.UIItem) []tui.SelectItem {
	out := make([]tui.SelectItem, len(items))
	for index, item := range items {
		out[index] = tui.SelectItem{
			ID: item.ID, Label: item.Label, Description: item.Description, Icon: item.Icon,
			Disabled: item.Disabled, DisabledReason: item.DisabledReason, Group: item.Group,
		}
	}
	return out
}

func actionItems(actions []script.UIAction) []tui.Action {
	out := make([]tui.Action, len(actions))
	for index, action := range actions {
		out[index] = tui.Action{
			ID: action.ID, Label: action.Label, Key: action.Key, Primary: action.Primary,
			Destructive: action.Destructive, Disabled: action.Disabled, DisabledReason: action.DisabledReason,
			RequiresSelection: action.RequiresSelection,
		}
	}
	return out
}

func formFields(fields []script.UIField) []tui.FormField {
	out := make([]tui.FormField, len(fields))
	for index, field := range fields {
		out[index] = tui.FormField{
			ID: field.ID, Type: field.Type, Label: field.Label, Description: field.Description,
			Placeholder: field.Placeholder, Required: field.Required, Initial: field.Initial,
			Items: selectItems(field.Items), MinLength: field.MinLength, MaxLength: field.MaxLength,
			Pattern: field.Pattern, PatternHint: field.PatternHint,
		}
	}
	return out
}
