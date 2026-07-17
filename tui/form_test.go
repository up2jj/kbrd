package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestFormEscapeCancels(t *testing.T) {
	var control Form
	control.SetSize(80, 24)
	control.Open(FormOptions{Title: "Create", Fields: []FormField{{ID: "title", Type: "input", Label: "Title"}}})
	if view := control.View(); !strings.Contains(view, "Create") || !strings.Contains(view, "Title") {
		t.Fatalf("unexpected form view: %q", view)
	}
	control.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	result, ok := control.TakeResult()
	if !ok || !result.Cancelled || result.Submitted {
		t.Fatalf("result = (%+v, %v)", result, ok)
	}
}

func TestFormCheckboxSubmitsTypedValue(t *testing.T) {
	var control Form
	control.SetSize(80, 24)
	control.Open(FormOptions{Fields: []FormField{{ID: "remove", Type: "checkbox", Label: "Remove", Initial: true}}})
	pumpFormCmd(&control, control.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	result, ok := control.TakeResult()
	if !ok || !result.Submitted || result.Cancelled || result.Values["remove"] != true {
		t.Fatalf("result = (%+v, %v)", result, ok)
	}
}

func pumpFormCmd(control *Form, cmd tea.Cmd) {
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		cmd = queue[0]
		queue = queue[1:]
		if cmd == nil {
			continue
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		if next := control.Update(msg); next != nil {
			queue = append(queue, next)
		}
	}
}

func TestFormFieldValidators(t *testing.T) {
	validate := textValidator(FormField{Required: true, MinLength: 2, MaxLength: 4, Pattern: "^[a-z]+$", PatternHint: "lowercase only"})
	tests := []struct {
		value string
		want  string
	}{
		{"", "required"},
		{"a", "at least 2"},
		{"abcde", "at most 4"},
		{"AB", "lowercase only"},
		{"ab", ""},
	}
	for _, tt := range tests {
		err := validate(tt.value)
		if tt.want == "" && err != nil {
			t.Fatalf("validate(%q) = %v", tt.value, err)
		}
		if tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
			t.Fatalf("validate(%q) = %v, want %q", tt.value, err, tt.want)
		}
	}
}

func TestFormOptionsExcludeDisabledItems(t *testing.T) {
	options := formOptions([]SelectItem{{ID: "a", Label: "A"}, {ID: "b", Label: "B", Disabled: true}})
	if len(options) != 1 || options[0].Value != "a" {
		t.Fatalf("options = %+v", options)
	}
}
