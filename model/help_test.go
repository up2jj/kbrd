package model

import (
	"strings"
	"testing"
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

func TestGlobalShortcuts_Structure(t *testing.T) {
	t.Parallel()
	groups := GlobalShortcuts(ShortcutContext{})
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
			t.Errorf("group %q has no shortcuts", g.Title)
		}
		for _, s := range g.Items {
			if strings.TrimSpace(s.Keys) == "" {
				t.Errorf("group %q has shortcut with empty Keys: %+v", g.Title, s)
			}
			if strings.TrimSpace(s.Label) == "" {
				t.Errorf("group %q has shortcut with empty Label: %+v", g.Title, s)
			}
		}
	}
	// Spot-check expected groups exist.
	for _, want := range []string{"Navigation", "Item", "Global"} {
		if !seenTitles[want] {
			t.Errorf("missing expected group %q", want)
		}
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

func TestRenderHelpOverlay_ContainsContent(t *testing.T) {
	t.Parallel()
	groups := []ShortcutGroup{
		{Title: "Group One", Items: []Shortcut{{Keys: "x", Label: "do x"}}},
		{Title: "Group Two", Items: []Shortcut{{Keys: "y", Label: "do y"}}},
	}
	got := RenderHelpOverlay(80, 30, groups)
	for _, want := range []string{"Shortcuts", "Group One", "Group Two", "x", "do x", "y", "do y"} {
		if !strings.Contains(got, want) {
			t.Errorf("overlay missing %q", want)
		}
	}
}
