package model

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/config"
)

func key1(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func keySpecial(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

func testCommands() []config.Command {
	return []config.Command{
		{Name: "Edit", Shortcut: "e", Description: "edit", Template: "nano"},
		{Name: "Reveal", Shortcut: "f", Description: "reveal", Template: "open"},
		{Name: "Word count", Shortcut: "w", Description: "wc", Template: "wc"},
	}
}

func TestCustomCommandMenu_OpenClose(t *testing.T) {
	var m CustomCommandMenu
	if m.Active() {
		t.Fatal("new menu must not be active")
	}
	m.Open(testCommands(), nil, map[string]string{"filePath": "/x"})
	if !m.Active() {
		t.Fatal("Open did not activate")
	}
	if m.selected != 0 {
		t.Fatalf("selected: got %d want 0", m.selected)
	}
	m.Close()
	if m.Active() {
		t.Fatal("Close did not deactivate")
	}
	if m.commands != nil || m.vars != nil {
		t.Errorf("Close did not clear state: cmds=%v vars=%v", m.commands, m.vars)
	}
}

func TestCustomCommandMenu_Esc(t *testing.T) {
	var m CustomCommandMenu
	m.Open(testCommands(), nil, nil)
	cmd := m.Update(keySpecial(tea.KeyEsc))
	if cmd != nil {
		t.Fatalf("esc should not emit a tea.Cmd, got %v", cmd)
	}
	if m.Active() {
		t.Fatal("esc did not close menu")
	}
}

func TestCustomCommandMenu_ArrowNavigation(t *testing.T) {
	var m CustomCommandMenu
	m.Open(testCommands(), nil, nil)

	m.Update(keySpecial(tea.KeyDown))
	if m.selected != 1 {
		t.Errorf("after down: got %d want 1", m.selected)
	}
	m.Update(keySpecial(tea.KeyDown))
	if m.selected != 2 {
		t.Errorf("after second down: got %d want 2", m.selected)
	}
	// past end clamps
	m.Update(keySpecial(tea.KeyDown))
	if m.selected != 2 {
		t.Errorf("past end: got %d want clamped to 2", m.selected)
	}
	m.Update(keySpecial(tea.KeyUp))
	if m.selected != 1 {
		t.Errorf("after up: got %d want 1", m.selected)
	}
	m.Update(keySpecial(tea.KeyUp))
	m.Update(keySpecial(tea.KeyUp))
	if m.selected != 0 {
		t.Errorf("past start: got %d want clamped to 0", m.selected)
	}
}

func TestCustomCommandMenu_EnterRunsSelected(t *testing.T) {
	var m CustomCommandMenu
	vars := map[string]string{"filePath": "/x"}
	m.Open(testCommands(), nil, vars)
	m.Update(keySpecial(tea.KeyDown)) // select Reveal

	cmd := m.Update(keySpecial(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter should emit a tea.Cmd")
	}
	if m.Active() {
		t.Fatal("running should close menu")
	}
	msg := cmd()
	run, ok := msg.(runCustomCommandMsg)
	if !ok {
		t.Fatalf("msg: got %T want runCustomCommandMsg", msg)
	}
	if run.Cmd.Name != "Reveal" {
		t.Errorf("Cmd.Name: got %q want Reveal", run.Cmd.Name)
	}
	if run.Vars["filePath"] != "/x" {
		t.Errorf("vars not forwarded: %+v", run.Vars)
	}
}

func TestCustomCommandMenu_ShortcutKeyRunsCommand(t *testing.T) {
	var m CustomCommandMenu
	m.Open(testCommands(), nil, nil)

	cmd := m.Update(key1('w'))
	if cmd == nil {
		t.Fatal("shortcut key should emit a tea.Cmd")
	}
	if m.Active() {
		t.Fatal("shortcut key did not close menu")
	}
	msg := cmd()
	run, ok := msg.(runCustomCommandMsg)
	if !ok {
		t.Fatalf("msg: got %T want runCustomCommandMsg", msg)
	}
	if run.Cmd.Shortcut != "w" {
		t.Errorf("ran wrong command: %+v", run.Cmd)
	}
}

func TestCustomCommandMenu_UnknownKeyIsNoop(t *testing.T) {
	var m CustomCommandMenu
	m.Open(testCommands(), nil, nil)
	cmd := m.Update(key1('z'))
	if cmd != nil {
		t.Errorf("unknown key should not emit a tea.Cmd, got %v", cmd)
	}
	if !m.Active() {
		t.Error("unknown key should not close menu")
	}
}

func TestCustomCommandMenu_EnterOnEmptyMenuJustCloses(t *testing.T) {
	var m CustomCommandMenu
	m.Open(nil, nil, nil)
	cmd := m.Update(keySpecial(tea.KeyEnter))
	if cmd != nil {
		t.Errorf("empty enter should not emit a tea.Cmd, got %v", cmd)
	}
	if m.Active() {
		t.Error("empty enter should close menu")
	}
}

func TestCustomCommandMenu_View_EmptyState(t *testing.T) {
	var m CustomCommandMenu
	m.Open(nil, nil, nil)
	view := m.View()
	if !strings.Contains(view, "no commands defined") {
		t.Errorf("empty state missing hint, got:\n%s", view)
	}
}

func TestCustomCommandMenu_View_ShowsWarnings(t *testing.T) {
	var m CustomCommandMenu
	warnings := []config.CommandLoadWarning{
		{Source: ".kbrd_commands.yml", Message: "parse error: bad yaml"},
	}
	m.Open(nil, warnings, nil)
	view := m.View()
	for _, want := range []string{"load errors", ".kbrd_commands.yml", "parse error: bad yaml"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q, got:\n%s", want, view)
		}
	}
}

func TestCustomCommandMenu_View_ListsCommands(t *testing.T) {
	var m CustomCommandMenu
	m.Open(testCommands(), nil, nil)
	view := m.View()
	for _, want := range []string{"Edit", "Reveal", "Word count", "[e]", "[f]", "[w]"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q, got:\n%s", want, view)
		}
	}
}
