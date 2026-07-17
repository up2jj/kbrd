package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"kbrd/theme"
)

type Action struct {
	ID                string
	Label             string
	Key               string
	Primary           bool
	Destructive       bool
	Disabled          bool
	DisabledReason    string
	RequiresSelection bool
}

type ActionsOptions struct {
	Title   string
	Actions []Action
}

type ActionResult struct {
	ID        string
	Submitted bool
	Cancelled bool
}

// Actions reuses Select's navigation and rendering while adding direct
// shortcuts for individual actions.
type Actions struct {
	selectOne Select
	actions   []Action
}

func (a *Actions) Open(opts ActionsOptions) {
	a.actions = append(a.actions[:0], opts.Actions...)
	items := make([]SelectItem, len(opts.Actions))
	for index, action := range opts.Actions {
		description := action.Key
		items[index] = SelectItem{
			ID: action.ID, Label: action.Label, Description: description,
			Disabled: action.Disabled, DisabledReason: action.DisabledReason,
			Primary: action.Primary, Destructive: action.Destructive,
		}
	}
	a.selectOne.Open(SelectOptions{Title: opts.Title, Items: items})
}

func (a *Actions) Active() bool { return a.selectOne.Active() }

func (a *Actions) SetSize(width, height int) { a.selectOne.SetSize(width, height) }

func (a *Actions) SetPalette(p theme.Palette) { a.selectOne.SetPalette(p) }

func (a *Actions) Update(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		pressed := strings.ToLower(keyMsg.String())
		for _, action := range a.actions {
			if action.Key != "" && strings.EqualFold(action.Key, pressed) && !action.Disabled {
				a.selectOne.SubmitID(action.ID)
				return nil
			}
		}
	}
	return a.selectOne.Update(msg)
}

func (a *Actions) View() string { return a.selectOne.View() }

func (a *Actions) TakeResult() (ActionResult, bool) {
	result, ok := a.selectOne.TakeResult()
	if !ok {
		return ActionResult{}, false
	}
	return ActionResult{ID: result.ID, Submitted: result.Submitted, Cancelled: result.Cancelled}, true
}

func (a *Actions) Close() {
	a.selectOne.Close()
	a.actions = nil
}
