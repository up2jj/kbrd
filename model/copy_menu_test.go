package model

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestBuildCopyMenuEntriesWithFrontmatter(t *testing.T) {
	t.Parallel()
	const raw = "---\ntitle: Ship it\ntags: [urgent]\n---\n\n# Body\n"
	col := newTestColumn(t, map[string]string{"task": raw})
	targets := targetsForItems(col, []Item{*col.SelectedItem()})

	entries, err := buildCopyMenuEntries(col, targets)
	if err != nil {
		t.Fatal(err)
	}
	wantLabels := []string{"Copy content", "Copy file path", "Copy file name", "Copy frontmatter", "Copy body only"}
	if len(entries) != len(wantLabels) {
		t.Fatalf("entries = %d, want %d: %+v", len(entries), len(wantLabels), entries)
	}
	for i, want := range wantLabels {
		if entries[i].Label != want {
			t.Errorf("entry %d label = %q, want %q", i, entries[i].Label, want)
		}
	}
	if entries[0].Content != raw {
		t.Errorf("content = %q, want raw file", entries[0].Content)
	}
	if entries[1].Content != filepath.Join(col.Path, "task.md") {
		t.Errorf("path = %q", entries[1].Content)
	}
	if entries[2].Content != "task.md" {
		t.Errorf("name = %q, want task.md", entries[2].Content)
	}
	if entries[3].Content != "---\ntitle: Ship it\ntags: [urgent]\n---\n" {
		t.Errorf("frontmatter = %q", entries[3].Content)
	}
	if entries[4].Content != "\n# Body\n" {
		t.Errorf("body = %q", entries[4].Content)
	}
}

func TestBuildCopyMenuEntriesWithoutFrontmatter(t *testing.T) {
	t.Parallel()
	const raw = "# Body\n"
	col := newTestColumn(t, map[string]string{"task": raw})
	targets := targetsForItems(col, []Item{*col.SelectedItem()})

	entries, err := buildCopyMenuEntries(col, targets)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("entries = %d, want 4: %+v", len(entries), entries)
	}
	if entries[3].Label != "Copy body only" || entries[3].Content != raw {
		t.Fatalf("body entry = %+v, want unchanged body", entries[3])
	}
}

func TestCopyActionOpensMenuAndFilters(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 1)
	writeColItem(t, b.columns[0], "task")

	b.handleKey(keyPressText("c"))
	if !b.copyMenu.Active() {
		t.Fatal("copy key did not open copy menu")
	}
	if entry, ok := b.copyMenu.SelectedEntry(); !ok || entry.Label != "Copy content" {
		t.Fatalf("default entry = %+v, ok=%v", entry, ok)
	}
	b.handleKey(keyPressText("name"))
	if entry, ok := b.copyMenu.SelectedEntry(); !ok || entry.Label != "Copy file name" {
		t.Fatalf("filtered entry = %+v, ok=%v", entry, ok)
	}
	b.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if b.copyMenu.Active() {
		t.Fatal("escape did not close copy menu")
	}
}

func TestCopyMenuShiftEnterRequestsClipboardHistory(t *testing.T) {
	t.Parallel()
	entries := []copyMenuEntry{{Label: "Copy content", Content: "body"}}

	var normal CopyMenu
	normal.Open(columnRef{}, nil, entries)
	confirmed, saveHistory := normal.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !confirmed || saveHistory {
		t.Fatalf("enter = confirmed %v, history %v; want true, false", confirmed, saveHistory)
	}

	var shifted CopyMenu
	shifted.Open(columnRef{}, nil, entries)
	confirmed, saveHistory = shifted.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	if !confirmed || !saveHistory {
		t.Fatalf("shift+enter = confirmed %v, history %v; want true, true", confirmed, saveHistory)
	}
}

func TestCopyMenuShiftCRequestsClipboardHistoryThroughMultiplexers(t *testing.T) {
	t.Parallel()
	entries := []copyMenuEntry{{Label: "Copy content", Content: "body"}}
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "legacy uppercase text", key: tea.KeyPressMsg{Code: 'C', Text: "C"}},
		{name: "enhanced shifted key", key: tea.KeyPressMsg{Code: 'c', Mod: tea.ModShift}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var menu CopyMenu
			menu.Open(columnRef{}, nil, entries)
			confirmed, saveHistory := menu.Update(tt.key)
			if !confirmed || !saveHistory {
				t.Fatalf("shift+c = confirmed %v, history %v; want true, true", confirmed, saveHistory)
			}
		})
	}
}

func TestBuildCopyMenuEntriesForMarkedCards(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, map[string]string{"a": "A", "b": "B"})
	targets := targetsForItems(col, col.Items)

	entries, err := buildCopyMenuEntries(col, targets)
	if err != nil {
		t.Fatal(err)
	}
	if entries[2].Content != "a.md\nb.md" {
		t.Errorf("names = %q, want newline-separated names", entries[2].Content)
	}
	if entries[0].Content != "--- a.md ---\nA\n\n--- b.md ---\nB" {
		t.Errorf("batch content = %q", entries[0].Content)
	}
}
