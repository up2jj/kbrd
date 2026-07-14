package model

import (
	"strings"
	"testing"
	"time"

	"kbrd/config"
)

func TestFrontmatterPresetMenuGroupsAndSearches(t *testing.T) {
	t.Parallel()
	var menu frontmatterPresetMenu
	menu.SetPalette(DarkPalette())
	menu.Open(0, columnRef{Name: "Doing", Path: "/board/Doing"}, []config.FrontmatterPreset{
		{ID: "column", Name: "Column preset", Description: "Only for this column", Columns: []any{"Doing"}},
		{ID: "board", Name: "Board preset", Description: "Available everywhere"},
	}, nil, "")

	view := menu.View(100, 40)
	for _, want := range []string{"Column presets", "Board presets", "Column preset", "Board preset"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if len(menu.nav) != 2 {
		t.Fatalf("nav len = %d, want 2 presets", len(menu.nav))
	}
	if selected, ok := menu.SelectedPreset(); !ok || selected.ID != "column" {
		t.Fatalf("initial selection = %+v, %v; want column preset", selected, ok)
	}

	menu.StartFilter()
	menu.AppendFilter("board")
	if selected, ok := menu.SelectedPreset(); !ok || selected.ID != "board" {
		t.Fatalf("filtered selection = %+v, %v; want board preset", selected, ok)
	}
	filtered := menu.View(100, 40)
	if strings.Contains(filtered, "Column presets") || strings.Contains(filtered, "Board presets") {
		t.Fatalf("filtered view should be flat:\n%s", filtered)
	}
}

func TestFrontmatterPresetMenuOverviewShowsChanges(t *testing.T) {
	var menu frontmatterPresetMenu
	menu.SetPalette(DarkPalette())
	menu.Open(0, columnRef{Name: "Doing", Path: "/board/Doing"}, []config.FrontmatterPreset{
		{
			ID:          "start-work",
			Name:        "Start work",
			Description: "Mark the card as active",
			Set:         map[string]any{"started_at": "{{now}}", "status": "doing"},
			Unset:       []string{"blocked_by"},
		},
	}, nil, "")

	view := menu.View(100, 40)
	for _, want := range []string{
		"Preset: Start work",
		"Mark the card as active",
		"action",
		"set    started_at",
		"set    status",
		"unset  blocked_by",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("overview missing %q:\n%s", want, view)
		}
	}
}

func TestBuildPresetPatchExpandsDynamicValues(t *testing.T) {
	preset := config.FrontmatterPreset{
		Set: map[string]any{
			"status": "{{column}}/{{filename}}",
			"tags":   []any{"work", "{{user}}"},
			"due":    "{{today+1d}}",
		},
		Unset: []string{"blocked_by"},
	}
	patch, err := buildPresetPatch(preset, &Column{Name: "Doing"}, &Item{Name: "card.md"}, map[string]string{
		"user": "alice",
	}, time.Date(2026, time.January, 15, 10, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("buildPresetPatch: %v", err)
	}
	if got, want := patch.Set["status"], "Doing/card.md"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if got, want := patch.Set["tags"], "[work, alice]"; got != want {
		t.Fatalf("tags = %q, want %q", got, want)
	}
	if got, want := patch.Set["due"], `"2026-01-16"`; got != want {
		t.Fatalf("due = %q, want %q", got, want)
	}
	if len(patch.Unset) != 1 || patch.Unset[0] != "blocked_by" {
		t.Fatalf("unset = %#v, want blocked_by", patch.Unset)
	}
}

func TestExpandPresetStringRejectsUnknownVariable(t *testing.T) {
	if _, err := expandPresetString("{{missing}}", map[string]string{}, time.Time{}); err == nil {
		t.Fatal("expandPresetString accepted an unknown variable")
	}
}
