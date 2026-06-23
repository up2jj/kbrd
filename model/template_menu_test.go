package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"kbrd/config"
	"kbrd/template"
)

func TestTemplateMenuGroupsAndSearch(t *testing.T) {
	t.Parallel()
	var menu TemplateMenu
	menu.SetPalette(DarkPalette())
	menu.Open(0, columnRef{Name: "TODO", Path: "/board/TODO"}, []template.Template{
		{Name: "Bug report", Scope: template.ScopeColumn, Path: "/board/TODO/.kbrd_templates/bug.md"},
		{Name: "Meeting note", Scope: template.ScopeBoard, Path: "/board/.kbrd_templates/meeting.md"},
	})

	view := menu.View(100, 40)
	for _, want := range []string{"Template authoring", "Column templates", "Board templates", "New column template", "Bug report", "Meeting note"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if len(menu.nav) != 3 {
		t.Fatalf("nav len = %d, want author + 2 templates", len(menu.nav))
	}

	menu.StartFilter()
	menu.AppendFilter("meeting")
	entry := menu.SelectedEntry()
	if entry.Label != "Meeting note" {
		t.Fatalf("filtered selection = %+v, want Meeting note", entry)
	}
	if strings.Contains(menu.View(100, 40), "Board templates") {
		t.Fatalf("filtered view should be flat:\n%s", menu.View(100, 40))
	}
}

func TestTemplateMenuWidthStableWhileFiltering(t *testing.T) {
	t.Parallel()
	var menu TemplateMenu
	menu.SetPalette(DarkPalette())
	menu.Open(0, columnRef{Name: "TODO", Path: "/board/TODO"}, []template.Template{
		{Name: "Bug report", Scope: template.ScopeColumn, Path: "/board/TODO/.kbrd_templates/bug.md"},
		{Name: "Meeting note", Scope: template.ScopeBoard, Path: "/board/.kbrd_templates/meeting.md"},
	})

	width := menu.contentWidth(100)
	menu.StartFilter()
	if got := menu.contentWidth(100); got != width {
		t.Fatalf("filter start width = %d, want %d", got, width)
	}
	menu.AppendFilter("bug")
	if got := menu.contentWidth(100); got != width {
		t.Fatalf("filtered match width = %d, want %d", got, width)
	}
	menu.Backspace()
	menu.Backspace()
	menu.Backspace()
	menu.AppendFilter("zzz")
	if got := menu.contentWidth(100); got != width {
		t.Fatalf("filtered empty width = %d, want %d", got, width)
	}
}

func TestTemplateMenuHeightStableWhileFiltering(t *testing.T) {
	t.Parallel()
	var menu TemplateMenu
	menu.SetPalette(DarkPalette())
	menu.Open(0, columnRef{Name: "TODO", Path: "/board/TODO"}, []template.Template{
		{Name: "Bug report", Scope: template.ScopeColumn, Path: "/board/TODO/.kbrd_templates/bug.md"},
		{Name: "Meeting note", Scope: template.ScopeBoard, Path: "/board/.kbrd_templates/meeting.md"},
	})

	height := lipgloss.Height(menu.View(100, 40))
	menu.StartFilter()
	if got := lipgloss.Height(menu.View(100, 40)); got != height {
		t.Fatalf("filter start height = %d, want %d", got, height)
	}
	menu.AppendFilter("bug")
	if got := lipgloss.Height(menu.View(100, 40)); got != height {
		t.Fatalf("filtered match height = %d, want %d", got, height)
	}
	menu.Backspace()
	menu.Backspace()
	menu.Backspace()
	menu.AppendFilter("zzz")
	if got := lipgloss.Height(menu.View(100, 40)); got != height {
		t.Fatalf("filtered empty height = %d, want %d", got, height)
	}
}

func TestTemplateMenuSelectActions(t *testing.T) {
	t.Parallel()
	var menu TemplateMenu
	menu.SetPalette(DarkPalette())
	menu.Open(0, columnRef{Name: "TODO", Path: "/board/TODO"}, []template.Template{
		{Name: "Bug report", Scope: template.ScopeColumn, Path: "/board/TODO/.kbrd_templates/bug.md"},
	})

	entry, ok := menu.SelectAction(templateMenuUse)
	if !ok || entry.Kind != templateMenuEntryAuthor {
		t.Fatalf("enter on first row = %+v, %v; want author action", entry, ok)
	}

	menu.Update(tea.KeyMsg{Type: tea.KeyDown})
	for _, action := range []templateMenuAction{templateMenuUse, templateMenuEdit, templateMenuRemove} {
		entry, ok = menu.SelectAction(action)
		if !ok || entry.Label != "Bug report" {
			t.Fatalf("action %v on template = %+v, %v", action, entry, ok)
		}
	}
	if _, ok := menu.SelectAction(templateMenuAuthor); ok {
		t.Fatal("author action should not run from a template row")
	}
}

func TestTemplateMenuOpenRejectsVirtualColumn(t *testing.T) {
	t.Parallel()
	b := &Board{
		cfg:      config.Config{Path: t.TempDir(), NotifyBackend: "none"},
		notifier: NewNotifier("none"),
	}
	col := NewVirtualColumn("tasks", "Tasks", DarkPalette())
	b.templateMenuActions().open(col)
	if b.templateMenu.Active() {
		t.Fatal("template menu should not open for virtual columns")
	}
}

func TestManagedFileSaveValidatesTemplatesBeforeWrite(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), template.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bug.md")
	original := "---\nname: Bug\nfilename: bug\n---\nbody\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &Board{
		cfg:      config.Config{Path: filepath.Dir(dir), NotifyBackend: "none"},
		editor:   NewEditor(false),
		notifier: NewNotifier("none"),
	}
	_ = b.editor.OpenManagedFile("Bug", path)
	b.editor.textarea.SetValue("not frontmatter")

	b.mutationHandlers().handleManagedFileSave(managedFileSaveMsg{
		Path:    path,
		Label:   "Bug",
		Content: b.editor.textarea.Value(),
	})

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("invalid template save overwrote file: %q", got)
	}
	if !b.editor.IsDirty() {
		t.Fatal("editor should remain dirty after failed managed-file save")
	}
}

func TestTemplateRemoveConfirmDeletesTemplate(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), template.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bug.md")
	if err := os.WriteFile(path, []byte("---\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &Board{notifier: NewNotifier("none")}
	b.mutationHandlers().handleTemplateRemoveConfirm(templateRemoveConfirmMsg{Path: path, Name: "Bug", Scope: template.ScopeBoard})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("template still exists after remove: %v", err)
	}
}

func TestTemplateRemoveConfirmReopensTemplateMenu(t *testing.T) {
	t.Parallel()
	boardDir := t.TempDir()
	colDir := filepath.Join(boardDir, "Todo")
	templateDir := filepath.Join(colDir, template.Dir)
	if err := os.MkdirAll(templateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(templateDir, "bug.md")
	if err := os.WriteFile(path, []byte("---\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	col := &Column{Name: "Todo", Path: colDir}
	b := &Board{
		cfg:         config.Config{Path: boardDir, NotifyBackend: "none"},
		columns:     []*Column{col},
		selectedCol: 0,
		notifier:    NewNotifier("none"),
	}
	b.mutationHandlers().handleTemplateRemoveConfirm(templateRemoveConfirmMsg{
		Column: refForColumn(col),
		Path:   path,
		Name:   "Bug",
		Scope:  template.ScopeColumn,
	})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("template still exists after remove: %v", err)
	}
	if !b.templateMenu.Active() {
		t.Fatal("template menu should reopen after removing a template")
	}
	if got := b.templateMenu.SelectedEntry().Kind; got != templateMenuEntryAuthor {
		t.Fatalf("selected entry kind = %v, want author action", got)
	}
}
