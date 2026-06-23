package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"kbrd/template"
)

func TestFieldSeed(t *testing.T) {
	// No prefill: the default seeds the field.
	f := template.Field{Type: "input", Default: "dflt"}
	if got := fieldSeed(f); got != "dflt" {
		t.Errorf("default seed = %q", got)
	}

	// prefill: clipboard — exercised only where a clipboard exists (skipped
	// in headless CI). The form must start with the clipboard's content.
	// Save and restore the user's clipboard around the check.
	f = template.Field{Type: "input", Prefill: template.PrefillClipboard}
	saved, savedErr := clipboard.ReadAll()
	if err := clipboard.WriteAll("from-clipboard"); err != nil {
		t.Skipf("no clipboard available: %v", err)
	}
	if savedErr == nil {
		defer func() { _ = clipboard.WriteAll(saved) }()
	}
	if got := fieldSeed(f); got != "from-clipboard" {
		t.Errorf("clipboard seed = %q", got)
	}
}

func TestTemplateFlowCreateMenu_EmptyOnly(t *testing.T) {
	t.Parallel()
	var flow TemplateFlow
	flow.SetPalette(DarkPalette())
	flow.SetSize(100, 40)
	flow.Open(2, columnRef{Name: "TODO", Path: "/board/TODO"}, nil)

	if !flow.Active() {
		t.Fatal("create menu should be active")
	}
	if len(flow.nav) != 1 {
		t.Fatalf("nav len = %d, want 1", len(flow.nav))
	}
	choice, ok := flow.selectedChoice()
	if !ok || choice.Kind != createChoiceEmpty {
		t.Fatalf("selected choice = %+v, %v; want empty choice", choice, ok)
	}
	view := flow.View()
	for _, want := range []string{"Create item", "Create", "Empty Markdown file"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}

	cmd := flow.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on empty choice returned nil cmd")
	}
	gotMsg := cmd()
	msg, ok := gotMsg.(createEmptyItemMsg)
	if !ok {
		t.Fatalf("cmd msg = %T, want createEmptyItemMsg", gotMsg)
	}
	if msg.ColIndex != 2 || msg.Column.Name != "TODO" {
		t.Fatalf("create msg = %+v", msg)
	}
	if flow.Active() {
		t.Fatal("flow should close after choosing empty card")
	}
}

func TestTemplateFlowCreateMenu_TemplateGroups(t *testing.T) {
	t.Parallel()
	var flow TemplateFlow
	flow.SetPalette(DarkPalette())
	flow.SetSize(100, 40)
	flow.Open(0, columnRef{Name: "TODO", Path: "/board/TODO"}, []template.Template{
		{Name: "Bug report", Scope: template.ScopeColumn, Filename: "bug"},
		{Name: "Meeting note", Scope: template.ScopeBoard, Filename: "meeting"},
	})

	view := flow.View()
	for _, want := range []string{"Create", "Column templates", "Board templates", "Bug report", "Meeting note"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if len(flow.nav) != 3 {
		t.Fatalf("nav len = %d, want empty + 2 templates", len(flow.nav))
	}
}

func TestTemplateFlowCreateMenu_FuzzySearch(t *testing.T) {
	t.Parallel()
	var flow TemplateFlow
	flow.SetPalette(DarkPalette())
	flow.SetSize(100, 40)
	flow.Open(0, columnRef{Name: "TODO", Path: "/board/TODO"}, []template.Template{
		{Name: "Bug report", Scope: template.ScopeColumn, Filename: "bug"},
		{Name: "Meeting note", Scope: template.ScopeBoard, Filename: "meeting"},
	})

	flow.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	flow.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("meet")})
	choice, ok := flow.selectedChoice()
	if !ok || choice.Label != "Meeting note" {
		t.Fatalf("selected after filter = %+v, %v; want Meeting note", choice, ok)
	}
	view := flow.View()
	if strings.Contains(view, "Column templates") || strings.Contains(view, "Board templates") {
		t.Fatalf("filtered view should be flat, got:\n%s", view)
	}
	if !strings.Contains(view, "Board template") {
		t.Fatalf("filtered row should include scope text, got:\n%s", view)
	}

	flow.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	flow.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	flow.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	flow.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	flow.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zzz")})
	if len(flow.nav) != 0 {
		t.Fatalf("nav len after no-match filter = %d, want 0", len(flow.nav))
	}
	if !strings.Contains(flow.View(), "no matches") {
		t.Fatalf("no-match view missing hint:\n%s", flow.View())
	}
	flow.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if flow.filtering {
		t.Fatal("esc should leave search mode")
	}
	if len(flow.nav) != 3 {
		t.Fatalf("nav len after esc = %d, want full menu", len(flow.nav))
	}
}

func TestTemplateFlowCreateMenu_WidthStableWhileNavigating(t *testing.T) {
	t.Parallel()
	var flow TemplateFlow
	flow.SetPalette(DarkPalette())
	flow.SetSize(100, 40)
	flow.Open(0, columnRef{Name: "TODO", Path: "/board/TODO"}, []template.Template{
		{Name: "Tiny", Scope: template.ScopeColumn, Filename: "tiny"},
		{Name: "Very long template name that used to stretch the selected row", Scope: template.ScopeBoard, Filename: "long"},
	})

	initial := lipgloss.Width(flow.View())
	flow.Update(tea.KeyMsg{Type: tea.KeyDown})
	down := lipgloss.Width(flow.View())
	flow.Update(tea.KeyMsg{Type: tea.KeyDown})
	long := lipgloss.Width(flow.View())
	flow.Update(tea.KeyMsg{Type: tea.KeyUp})
	up := lipgloss.Width(flow.View())

	if down != initial || long != initial || up != initial {
		t.Fatalf("width changed while navigating: initial=%d down=%d long=%d up=%d", initial, down, long, up)
	}
}

func TestTemplateFlowCreateMenu_FilteredWidthStableWhileNavigating(t *testing.T) {
	t.Parallel()
	var flow TemplateFlow
	flow.SetPalette(DarkPalette())
	flow.SetSize(100, 40)
	flow.Open(0, columnRef{Name: "TODO", Path: "/board/TODO"}, []template.Template{
		{Name: "Report", Scope: template.ScopeColumn, Filename: "report"},
		{Name: "Remarkably long report template", Scope: template.ScopeBoard, Filename: "long-report"},
	})

	flow.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	flow.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("report")})
	initial := lipgloss.Width(flow.View())
	flow.Update(tea.KeyMsg{Type: tea.KeyDown})
	down := lipgloss.Width(flow.View())
	flow.Update(tea.KeyMsg{Type: tea.KeyUp})
	up := lipgloss.Width(flow.View())

	if down != initial || up != initial {
		t.Fatalf("filtered width changed while navigating: initial=%d down=%d up=%d", initial, down, up)
	}
}

func TestTemplateFlowCreateMenu_ShadowedTemplatesStayHidden(t *testing.T) {
	t.Parallel()
	boardDir := t.TempDir()
	colDir := filepath.Join(boardDir, "TODO")
	if err := os.MkdirAll(filepath.Join(boardDir, template.Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(colDir, template.Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTemplateFile(t, filepath.Join(boardDir, template.Dir, "bug.md"), "Bug report", "board-bug")
	writeTemplateFile(t, filepath.Join(colDir, template.Dir, "bug.md"), "Bug report", "col-bug")
	writeTemplateFile(t, filepath.Join(boardDir, template.Dir, "chore.md"), "Chore", "chore")

	tmpls, warns, err := template.List(boardDir, colDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings = %+v", warns)
	}
	var flow TemplateFlow
	flow.SetPalette(DarkPalette())
	flow.SetSize(100, 40)
	flow.Open(0, columnRef{Name: "TODO", Path: colDir}, tmpls)

	var bugCount int
	for _, choice := range flow.menuChoices() {
		if choice.Label == "Bug report" {
			bugCount++
			if choice.Template.Scope != template.ScopeColumn {
				t.Fatalf("shadow winner scope = %q, want column", choice.Template.Scope)
			}
		}
	}
	if bugCount != 1 {
		t.Fatalf("bug template count = %d, want 1", bugCount)
	}
}

func writeTemplateFile(t *testing.T, path, name, filename string) {
	t.Helper()
	body := "---\nname: " + name + "\nfilename: " + filename + "\n---\nbody\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
