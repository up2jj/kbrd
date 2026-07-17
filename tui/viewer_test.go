package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestViewerScrollResizeAndAction(t *testing.T) {
	var viewer Viewer
	viewer.SetSize(60, 14)
	viewer.Open(ViewerOptions{
		Title: "Diff", Content: strings.Repeat("+added line\n", 12), Format: "diff", Wrap: true, LineNumbers: true,
		Actions: []Action{{ID: "apply", Label: "Apply", Key: "ctrl+a"}},
	})
	viewer.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if viewer.Offset() != 1 {
		t.Fatalf("offset = %d", viewer.Offset())
	}
	viewer.SetSize(60, 30)
	if viewer.Offset() != 0 {
		t.Fatalf("offset after growing viewport = %d", viewer.Offset())
	}
	viewer.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	result, ok := viewer.TakeResult()
	if !ok || !result.Submitted || result.Action != "apply" {
		t.Fatalf("result = (%+v, %v)", result, ok)
	}
}

func TestViewerPrettyPrintsJSONAndCancels(t *testing.T) {
	var viewer Viewer
	viewer.SetSize(60, 20)
	viewer.Open(ViewerOptions{Content: `{"name":"kbrd"}`, Format: "json", Wrap: true})
	if got := formattedViewerContent(viewer.opts.Content, viewer.opts.Format); !strings.Contains(got, "\n  \"name\"") {
		t.Fatalf("formatted JSON = %q", got)
	}
	viewer.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	result, ok := viewer.TakeResult()
	if !ok || !result.Cancelled {
		t.Fatalf("cancel = (%+v, %v)", result, ok)
	}
}

func TestViewerWithoutWrapPansWithoutDiscardingContent(t *testing.T) {
	const content = "0123456789abcdefghijklmnopqrstuvwxyz"
	var viewer Viewer
	viewer.SetSize(30, 14)
	viewer.Open(ViewerOptions{Content: content, Wrap: false})
	if viewer.lines[0].text != content {
		t.Fatalf("stored line = %q", viewer.lines[0].text)
	}
	first := viewer.visibleText(viewer.lines[0].text)
	for range 5 {
		viewer.Update(tea.KeyPressMsg{Code: 'l'})
	}
	if viewer.left != 5 || viewer.visibleText(viewer.lines[0].text) == first {
		t.Fatalf("horizontal viewport = %d / %q", viewer.left, viewer.visibleText(viewer.lines[0].text))
	}
	viewer.SetSize(60, 14)
	if viewer.left != 0 || viewer.visibleText(viewer.lines[0].text) != content {
		t.Fatalf("resized viewport = %d / %q", viewer.left, viewer.visibleText(viewer.lines[0].text))
	}
}
