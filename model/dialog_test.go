package model

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDialogViewEmptyBodyStartsAtButtons(t *testing.T) {
	t.Parallel()
	var d Dialog
	d.palette = DarkPalette()
	d.Open(DialogOptions{
		Title: "Complete task?",
		Buttons: []DialogButton{
			{Label: "Yes", Kind: ButtonPrimary},
			{Label: "No"},
		},
	})

	lines := strings.Split(ansi.Strip(d.View()), "\n")
	if got := firstLineContaining(lines, "Yes"); got != 2 {
		t.Fatalf("empty-body dialog button line = %d, want 2\n%s", got, strings.Join(lines, "\n"))
	}
}

func TestDialogViewBodyKeepsBodyBeforeButtons(t *testing.T) {
	t.Parallel()
	var d Dialog
	d.palette = DarkPalette()
	d.OpenConfirm("Delete item?", "task.md", deleteConfirmMsg{})

	lines := strings.Split(ansi.Strip(d.View()), "\n")
	bodyLine := firstLineContaining(lines, "task.md")
	buttonLine := firstLineContaining(lines, "Yes")
	if bodyLine < 0 {
		t.Fatalf("dialog body not rendered\n%s", strings.Join(lines, "\n"))
	}
	if buttonLine <= bodyLine {
		t.Fatalf("dialog buttons rendered before body: body=%d button=%d\n%s", bodyLine, buttonLine, strings.Join(lines, "\n"))
	}
}

func firstLineContaining(lines []string, needle string) int {
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}
