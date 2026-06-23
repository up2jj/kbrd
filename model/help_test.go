package model

import (
	"strings"
	"testing"

	"kbrd/config"
)

func TestContextShortcuts(t *testing.T) {
	t.Parallel()

	t.Run("quick command mode shows only cancel", func(t *testing.T) {
		got := ContextShortcuts(ShortcutContext{QuickCmdMode: true})
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1: %+v", len(got), got)
		}
		if got[0].Keys != "esc" || got[0].Label != "cancel" {
			t.Errorf("got %+v, want {esc, cancel}", got[0])
		}
	})

	t.Run("quick command mode wins over selected item", func(t *testing.T) {
		got := ContextShortcuts(ShortcutContext{QuickCmdMode: true, HasSelectedItem: true})
		if len(got) != 1 || got[0].Keys != "esc" {
			t.Errorf("got %+v, want only {esc, cancel}", got)
		}
	})

	t.Run("selected item shows item shortcuts", func(t *testing.T) {
		got := ContextShortcuts(ShortcutContext{HasSelectedItem: true})
		if len(got) == 0 {
			t.Fatal("no shortcuts returned")
		}
		if got[0].Keys != "space" || got[0].Label != "peek" {
			t.Errorf("first shortcut = %+v, want {space, peek}", got[0])
		}
		// Sanity-check a few other expected keys are present.
		mustContain(t, got, "e", "edit")
		mustContain(t, got, "d", "delete")
		mustContain(t, got, "?", "more")
	})

	t.Run("default shows new/filter shortcuts", func(t *testing.T) {
		got := ContextShortcuts(ShortcutContext{})
		if len(got) == 0 {
			t.Fatal("no shortcuts returned")
		}
		mustContain(t, got, "n", "new")
		mustNotContain(t, got, "t", "template")
		mustContain(t, got, "/", "filter")
		mustContain(t, got, "R", "rename col")
	})
}

func mustContain(t *testing.T, items []Shortcut, keys, label string) {
	t.Helper()
	for _, s := range items {
		if s.Keys == keys && s.Label == label {
			return
		}
	}
	t.Errorf("shortcuts %+v missing {%s, %s}", items, keys, label)
}

func mustNotContain(t *testing.T, items []Shortcut, keys, label string) {
	t.Helper()
	for _, s := range items {
		if s.Keys == keys && s.Label == label {
			t.Errorf("shortcuts %+v unexpectedly contain {%s, %s}", items, keys, label)
		}
	}
}

func TestHelpMenuGroups_Structure(t *testing.T) {
	t.Parallel()
	groups := HelpMenuGroups()
	if len(groups) == 0 {
		t.Fatal("no groups returned")
	}
	seenTitles := map[string]bool{}
	for _, g := range groups {
		if strings.TrimSpace(g.Title) == "" {
			t.Errorf("group with empty title: %+v", g)
		}
		if seenTitles[g.Title] {
			t.Errorf("duplicate group title %q", g.Title)
		}
		seenTitles[g.Title] = true
		if len(g.Items) == 0 {
			t.Errorf("group %q has no entries", g.Title)
		}
		for _, e := range g.Items {
			if strings.TrimSpace(e.Keys) == "" {
				t.Errorf("group %q has entry with empty Keys: %+v", g.Title, e)
			}
			if strings.TrimSpace(e.Label) == "" {
				t.Errorf("group %q has entry with empty Label: %+v", g.Title, e)
			}
			if strings.TrimSpace(e.Desc) == "" {
				t.Errorf("group %q entry %q has no tooltip", g.Title, e.Keys)
			}
		}
	}
	for _, want := range []string{"Navigation", "Item", "Global"} {
		if !seenTitles[want] {
			t.Errorf("missing expected group %q", want)
		}
	}
	for _, g := range groups {
		for _, e := range g.Items {
			if e.Keys == "t" {
				t.Errorf("old template shortcut still present: %+v", e)
			}
		}
	}
}

// menuKey condenses a verbose multi-alternative help key but preserves a literal
// "/" binding (the filter key).
func TestMenuKey(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"← / shift+tab / [": "←",
		"ctrl+d":            "ctrl+d",
		"/":                 "/",
		"g":                 "g",
	}
	for in, want := range cases {
		if got := menuKey(in); got != want {
			t.Errorf("menuKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// HelpMenu navigation skips disabled rows and SelectedRunKey returns the rune to
// inject for the highlighted entry.
func TestHelpMenu_NavSkipsDisabled(t *testing.T) {
	t.Parallel()
	m := &HelpMenu{}
	m.SetPalette(DarkPalette())
	m.Open([]HelpGroup{
		{Title: "G", Items: []HelpEntry{
			{Keys: "e", Label: "edit", RunKey: "e", Disabled: true}, // not selectable
			{Keys: "n", Label: "new", RunKey: "n"},
		}},
	})
	if !m.Active() {
		t.Fatal("menu should be active")
	}
	if got := m.SelectedRunKey(); got != "n" {
		t.Errorf("first selectable run key = %q, want n (disabled e must be skipped)", got)
	}
}

func TestRenderInlineHints(t *testing.T) {
	t.Parallel()
	items := []Shortcut{
		{Keys: "j", Label: "down"},
		{Keys: "k", Label: "up"},
	}
	got := RenderInlineHints(items)
	for _, want := range []string{"j", "down", "k", "up"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q: %q", want, got)
		}
	}
}

func TestRenderInlineHints_Empty(t *testing.T) {
	t.Parallel()
	if got := RenderInlineHints(nil); got != "" {
		t.Errorf("RenderInlineHints(nil) = %q, want empty", got)
	}
}

func TestHelpMenu_RenderContainsContent(t *testing.T) {
	t.Parallel()
	m := &HelpMenu{}
	m.SetPalette(DarkPalette())
	m.Open([]HelpGroup{
		{Title: "Group One", Items: []HelpEntry{{Keys: "x", Label: "do x", Desc: "does x", RunKey: "x"}}},
		{Title: "Group Two", Items: []HelpEntry{{Keys: "y", Label: "do y", Desc: "does y", RunKey: "y"}}},
	})
	got := m.View(80, 40)
	for _, want := range []string{"Keybindings", "Group One", "Group Two", "do x", "do y", "does x", "1 of 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("menu missing %q", want)
		}
	}
}

func TestBoardHelpActions_RunCustomCommandBuildsCommandMessage(t *testing.T) {
	col := newTestColumn(t, map[string]string{"task": "body"})
	b := NewBoard(config.Config{Path: col.Path, BoardName: "demo", NotifyBackend: "none"})
	b.columns = []*Column{col}
	b.commands = []config.Command{{Name: "Ship", ID: "ship", Description: "ship it", Template: "echo {{.fileName}}"}}

	cmd := b.helpActions().runCustomCommand("ship")
	if cmd == nil {
		t.Fatal("expected custom command tea.Cmd")
	}
	msg, ok := cmd().(runCustomCommandMsg)
	if !ok {
		t.Fatalf("command returned %T, want runCustomCommandMsg", msg)
	}
	if msg.Cmd.ID != "ship" {
		t.Fatalf("Cmd.ID = %q, want ship", msg.Cmd.ID)
	}
	if msg.Vars["fileName"] != "task" {
		t.Fatalf("fileName var = %q, want task", msg.Vars["fileName"])
	}
}
