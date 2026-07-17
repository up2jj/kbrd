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
	scriptUIConfirm
	scriptUIActions

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

	input     tui.Input
	selectOne tui.Select
	confirm   tui.Confirm
	actions   tui.Actions
	size      tui.Size
	palette   Palette
}

func (s *ScriptUI) SetPalette(p Palette) {
	s.palette = p
	s.input.SetPalette(p)
	s.selectOne.SetPalette(p)
	s.confirm.SetPalette(p)
	s.actions.SetPalette(p)
}

func (s *ScriptUI) Active() bool { return s.kind != scriptUINone }

func (s *ScriptUI) Close() {
	s.input.Close()
	s.selectOne.Close()
	s.confirm.Close()
	s.actions.Close()
	s.kind = scriptUINone
	s.name = ""
	s.token = ""
}

func (s *ScriptUI) Open(name string, req *script.UIRequest) {
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
	}
}

func (s *ScriptUI) MatchesToken(token string) bool {
	return s.Active() && s.token == token
}

func (s *ScriptUI) SetSize(width, height int) {
	s.size.Set(width, height)
	s.input.SetSize(width, height)
	s.selectOne.SetSize(width, height)
	s.confirm.SetSize(width, height)
	s.actions.SetSize(width, height)
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
	}
	return cmd
}

func (s *ScriptUI) View() string {
	switch s.kind {
	case scriptUIInput:
		return s.input.View()
	case scriptUISelect:
		return s.selectOne.View()
	case scriptUIConfirm:
		return s.confirm.View()
	case scriptUIActions:
		return s.actions.View()
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
		}
	}
	return out
}
