package model

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"kbrd/config"
)

func TestContextShortcuts(t *testing.T) {
	t.Parallel()

	t.Run("mnemonic mode shows jump now and cancel", func(t *testing.T) {
		got := ContextShortcuts(ShortcutContext{MnemonicMode: true})
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2: %+v", len(got), got)
		}
		if got[0].Keys != "enter" || got[0].Label != "jump now" {
			t.Errorf("got %+v, want {enter, jump now}", got[0])
		}
		if got[1].Keys != "esc" || got[1].Label != "cancel" {
			t.Errorf("got %+v, want {esc, cancel}", got[1])
		}
	})

	t.Run("mnemonic mode wins over selected item", func(t *testing.T) {
		got := ContextShortcuts(ShortcutContext{MnemonicMode: true, HasSelectedItem: true})
		if len(got) != 2 || got[0].Keys != "enter" || got[1].Keys != "esc" {
			t.Errorf("got %+v, want only {enter, jump now}, {esc, cancel}", got)
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
	foundTemplateMenu := false
	for _, g := range groups {
		for _, e := range g.Items {
			if e.Keys == "t" && e.Label == "templates" {
				foundTemplateMenu = true
			}
		}
	}
	if !foundTemplateMenu {
		t.Error("missing template management shortcut")
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
	got := ansi.Strip(m.View(80, 40))
	for _, want := range []string{"Keybindings", "Group One", "Group Two", "do x", "do y", "does x", "1 of 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("menu missing %q", want)
		}
	}
}

func TestHelpMenu_WidthStableWhileNavigating(t *testing.T) {
	t.Parallel()
	m := &HelpMenu{}
	m.SetPalette(DarkPalette())
	m.Open([]HelpGroup{
		{Title: "Actions", Items: []HelpEntry{
			{Keys: "s", Label: "short", Desc: "short action", RunKey: "s"},
			{Keys: "l", Label: "very long shortcut label that used to resize the menu", Desc: "long action", RunKey: "l"},
		}},
	})

	initial := lipgloss.Width(m.View(100, 30))
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	down := lipgloss.Width(m.View(100, 30))
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	up := lipgloss.Width(m.View(100, 30))

	if down != initial || up != initial {
		t.Fatalf("width changed while navigating: initial=%d down=%d up=%d", initial, down, up)
	}
}

func TestHelpMenu_WidthStableAcrossPositionDigitBoundary(t *testing.T) {
	t.Parallel()
	m := &HelpMenu{}
	m.SetPalette(DarkPalette())
	m.Open(helpGroupsWithEntries(12))
	for i := 0; i < 8; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}

	initial := lipgloss.Width(m.View(100, 30))
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	ten := lipgloss.Width(m.View(100, 30))
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	eleven := lipgloss.Width(m.View(100, 30))

	if ten != initial || eleven != initial {
		t.Fatalf("width changed across position boundary: initial=%d ten=%d eleven=%d", initial, ten, eleven)
	}
}

func TestHelpMenu_WidthStableAcrossDescriptions(t *testing.T) {
	t.Parallel()
	m := &HelpMenu{}
	m.SetPalette(DarkPalette())
	m.Open([]HelpGroup{
		{Title: "Actions", Items: []HelpEntry{
			{Keys: "s", Label: "short desc", Desc: "short", RunKey: "s"},
			{Keys: "l", Label: "long desc", Desc: "this-description-is-intentionally-long-enough-to-force-truncation-in-the-description-pane", RunKey: "l"},
		}},
	})

	initial := lipgloss.Width(m.View(60, 30))
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	longDesc := lipgloss.Width(m.View(60, 30))
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	shortDesc := lipgloss.Width(m.View(60, 30))

	if longDesc != initial || shortDesc != initial {
		t.Fatalf("width changed across descriptions: initial=%d long=%d short=%d", initial, longDesc, shortDesc)
	}
}

func TestHelpMenu_FilteredWidthStableWhileNavigating(t *testing.T) {
	t.Parallel()
	m := &HelpMenu{}
	m.SetPalette(DarkPalette())
	m.Open([]HelpGroup{
		{Title: "Actions", Items: []HelpEntry{
			{Keys: "r", Label: "report", Desc: "report action", RunKey: "r"},
			{Keys: "R", Label: "remarkably long report action", Desc: "long report action", RunKey: "R"},
		}},
	})
	m.StartFilter()
	m.AppendFilter("report")

	initial := lipgloss.Width(m.View(100, 30))
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	down := lipgloss.Width(m.View(100, 30))
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	up := lipgloss.Width(m.View(100, 30))

	if down != initial || up != initial {
		t.Fatalf("filtered width changed while navigating: initial=%d down=%d up=%d", initial, down, up)
	}
}

func TestHelpMenu_FilteredWidthStableWhileQueryChanges(t *testing.T) {
	t.Parallel()
	m := &HelpMenu{}
	m.SetPalette(DarkPalette())
	m.Open([]HelpGroup{
		{Title: "Actions", Items: []HelpEntry{
			{Keys: "r", Label: "report", Desc: "report action", RunKey: "r"},
			{Keys: "R", Label: "remarkably long report action", Desc: "long report action", RunKey: "R"},
			{Keys: "x", Label: "x", Desc: "tiny action", RunKey: "x"},
		}},
	})

	initial := lipgloss.Width(m.View(100, 30))
	m.StartFilter()
	filtering := lipgloss.Width(m.View(100, 30))
	m.AppendFilter("report")
	matches := lipgloss.Width(m.View(100, 30))
	m.AppendFilter("zzz")
	noMatches := lipgloss.Width(m.View(100, 30))
	m.Backspace()
	backToMatches := lipgloss.Width(m.View(100, 30))

	if filtering != initial || matches != initial || noMatches != initial || backToMatches != initial {
		t.Fatalf("filter width changed: initial=%d filtering=%d matches=%d noMatches=%d back=%d",
			initial, filtering, matches, noMatches, backToMatches)
	}
}

func TestHelpMenu_FilteredHeightStableWhileQueryChanges(t *testing.T) {
	t.Parallel()
	m := &HelpMenu{}
	m.SetPalette(DarkPalette())
	m.Open([]HelpGroup{
		{Title: "Actions", Items: []HelpEntry{
			{Keys: "r", Label: "report", Desc: "report action", RunKey: "r"},
			{Keys: "R", Label: "remarkably long report action", Desc: "long report action", RunKey: "R"},
			{Keys: "x", Label: "x", Desc: "tiny action", RunKey: "x"},
		}},
	})

	initial := lipgloss.Height(m.View(100, 30))
	m.StartFilter()
	filtering := lipgloss.Height(m.View(100, 30))
	m.AppendFilter("x")
	oneMatch := lipgloss.Height(m.View(100, 30))
	m.AppendFilter("zzz")
	noMatches := lipgloss.Height(m.View(100, 30))
	m.Backspace()
	backToOne := lipgloss.Height(m.View(100, 30))

	if filtering != initial || oneMatch != initial || noMatches != initial || backToOne != initial {
		t.Fatalf("filter height changed: initial=%d filtering=%d oneMatch=%d noMatches=%d back=%d",
			initial, filtering, oneMatch, noMatches, backToOne)
	}
}

func TestHelpMenu_FilteredSingleResultKeepsPromptHeight(t *testing.T) {
	t.Parallel()
	m := &HelpMenu{}
	m.SetPalette(DarkPalette())
	m.Open([]HelpGroup{
		{Title: "Actions", Items: []HelpEntry{
			{Keys: "o", Label: "only", Desc: "only action", RunKey: "o"},
			{Keys: "m", Label: "many words", Desc: "many action", RunKey: "m"},
		}},
	})

	m.StartFilter()
	filtering := lipgloss.Height(m.View(100, 30))
	m.AppendFilter("only")
	oneMatch := lipgloss.Height(m.View(100, 30))

	if oneMatch != filtering {
		t.Fatalf("single filtered result height = %d, want %d", oneMatch, filtering)
	}
}

func TestHelpMenu_ScrollbarAppearsOnlyWhenOverflowing(t *testing.T) {
	t.Parallel()
	overflow := &HelpMenu{}
	overflow.SetPalette(DarkPalette())
	overflow.Open(helpGroupsWithEntries(12))
	if got := overflow.View(100, 14); !strings.Contains(got, "┃") {
		t.Fatalf("overflowing menu missing scrollbar thumb:\n%s", got)
	}

	fitting := &HelpMenu{}
	fitting.SetPalette(DarkPalette())
	fitting.Open(helpGroupsWithEntries(2))
	if got := fitting.View(100, 30); strings.Contains(got, "┃") {
		t.Fatalf("non-overflowing menu unexpectedly rendered scrollbar thumb:\n%s", got)
	}
}

func TestHelpMenu_ScrollByPreservesSelectionAndClampsViewport(t *testing.T) {
	t.Parallel()
	m := &HelpMenu{}
	m.SetPalette(DarkPalette())
	m.Open(helpGroupsWithEntries(6))

	m.ScrollBy(3)
	if got := m.SelectedRunKey(); got != "0" {
		t.Fatalf("selected after scroll down = %q, want 0", got)
	}
	if m.scroll != 3 {
		t.Fatalf("scroll after scroll down = %d, want 3", m.scroll)
	}
	m.ScrollBy(99)
	_ = m.View(100, 14)
	if got := m.SelectedRunKey(); got != "0" {
		t.Fatalf("selected after scroll past bottom = %q, want 0", got)
	}
	if m.scroll <= 0 || m.scroll >= len(m.rows) {
		t.Fatalf("scroll after clamp = %d, rows=%d", m.scroll, len(m.rows))
	}
	m.ScrollBy(-99)
	if got := m.SelectedRunKey(); got != "0" {
		t.Fatalf("selected after scroll past top = %q, want 0", got)
	}
	if m.scroll != 0 {
		t.Fatalf("scroll after scroll past top = %d, want 0", m.scroll)
	}
}

func TestHelpMenu_KeyboardNavigationRestoresSelectedVisibility(t *testing.T) {
	t.Parallel()
	m := &HelpMenu{}
	m.SetPalette(DarkPalette())
	m.Open(helpGroupsWithEntries(10))

	m.ScrollBy(6)
	_ = m.View(100, 14)
	if m.scroll == 0 {
		t.Fatal("precondition: mouse-style scroll should move viewport away from top")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_ = m.View(100, 14)
	if got := m.SelectedRunKey(); got != "1" {
		t.Fatalf("selected after keyboard down = %q, want 1", got)
	}
	selRow := m.nav[m.selected]
	if selRow < m.scroll || selRow >= m.scroll+6 {
		t.Fatalf("keyboard navigation did not restore selected visibility, selected row=%d scroll=%d", selRow, m.scroll)
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

func helpGroupsWithEntries(n int) []HelpGroup {
	items := make([]HelpEntry, 0, n)
	for i := 0; i < n; i++ {
		key := string(rune('0' + i%10))
		items = append(items, HelpEntry{
			Keys:   key,
			Label:  "action " + key,
			Desc:   "run action " + key,
			RunKey: key,
		})
	}
	return []HelpGroup{{Title: "Actions", Items: items}}
}
