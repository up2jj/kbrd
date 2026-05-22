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
		{Name: "Edit", ID: "e", Description: "edit", Template: "nano"},
		{Name: "Reveal", ID: "f", Description: "reveal", Template: "open"},
		{Name: "Word count", ID: "w", Description: "wc", Template: "wc"},
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

func TestCustomCommandMenu_FilterNarrowsAndEnterRuns(t *testing.T) {
	var m CustomCommandMenu
	m.Open(testCommands(), nil, nil)

	// Typing 'w' should fuzzy-match "Word count" (and possibly "Reveal"
	// via the 'w' in 'wc' description) — but "Word count" should rank first.
	m.Update(key1('w'))
	if m.filter != "w" {
		t.Fatalf("filter: got %q want %q", m.filter, "w")
	}
	if len(m.matches) == 0 {
		t.Fatal("expected at least one match for 'w'")
	}
	// Highlighted (selected=0) match should be Word count.
	top := m.commands[m.matches[0].Index]
	if top.Name != "Word count" {
		t.Errorf("top match: got %q want Word count", top.Name)
	}

	cmd := m.Update(keySpecial(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter should emit a tea.Cmd")
	}
	if m.Active() {
		t.Fatal("enter did not close menu")
	}
	msg := cmd()
	run, ok := msg.(runCustomCommandMsg)
	if !ok {
		t.Fatalf("msg: got %T want runCustomCommandMsg", msg)
	}
	if run.Cmd.ID != "w" {
		t.Errorf("ran wrong command: %+v", run.Cmd)
	}
}

func TestCustomCommandMenu_BackspaceRestores(t *testing.T) {
	var m CustomCommandMenu
	m.Open(testCommands(), nil, nil)
	all := len(m.matches)
	m.Update(key1('w'))
	if len(m.matches) >= all {
		t.Fatalf("filter did not narrow: %d >= %d", len(m.matches), all)
	}
	m.Update(keySpecial(tea.KeyBackspace))
	if m.filter != "" {
		t.Errorf("backspace did not clear filter: %q", m.filter)
	}
	if len(m.matches) != all {
		t.Errorf("backspace did not restore: got %d want %d", len(m.matches), all)
	}
}

func TestCustomCommandMenu_NoMatchEnterCloses(t *testing.T) {
	var m CustomCommandMenu
	m.Open(testCommands(), nil, nil)
	m.Update(key1('z'))
	m.Update(key1('z'))
	m.Update(key1('z'))
	if len(m.matches) != 0 {
		t.Fatalf("expected zero matches for 'zzz', got %d", len(m.matches))
	}
	cmd := m.Update(keySpecial(tea.KeyEnter))
	if cmd != nil {
		t.Errorf("enter on empty matches should not emit a tea.Cmd, got %v", cmd)
	}
	if m.Active() {
		t.Error("enter on empty matches should close menu")
	}
}

func TestCustomCommandMenu_EmptyFilterShowsAllInOrder(t *testing.T) {
	var m CustomCommandMenu
	m.Open(testCommands(), nil, nil)
	if len(m.matches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(m.matches))
	}
	for i, want := range []string{"Edit", "Reveal", "Word count"} {
		if got := m.commands[m.matches[i].Index].Name; got != want {
			t.Errorf("match[%d]: got %q want %q", i, got, want)
		}
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
	view := m.View(120, 40)
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
	view := m.View(120, 40)
	for _, want := range []string{"load errors", ".kbrd_commands.yml", "parse error: bad yaml"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q, got:\n%s", want, view)
		}
	}
}

func TestCustomCommandMenu_View_ListsCommands(t *testing.T) {
	var m CustomCommandMenu
	m.Open(testCommands(), nil, nil)
	view := m.View(120, 40)
	for _, want := range []string{"Edit", "Reveal", "Word count"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q, got:\n%s", want, view)
		}
	}
}
