package model

import (
	"strings"
	"testing"
)

// The :lua editor ctx must bind to the card whose buffer is open
// (b.editor.FileName), not the column's current selection — a script/timer/hook
// can move selection while the editor stays open, and a registered function that
// reads ctx.filePath or writes frontmatter must hit the edited card.
func TestBuildEditorEvalCtx_BindsToEditorFileNotSelection(t *testing.T) {
	col := newTestColumn(t, map[string]string{"a": "alpha", "b": "bravo"})
	b := &Board{columns: []*Column{col}}
	b.editor = NewEditor(false)
	b.editor.ColIndex = 0
	b.editor.ColPath = col.Path
	b.editor.FileName = "a"
	b.editor.ItemPath = col.ItemByName("a").FullPath

	// Selection moves to "b" while the editor stays open on "a".
	col.SelectByName("b")
	if sel := col.SelectedItem(); sel == nil || sel.Name != "b" {
		t.Fatalf("precondition: expected selection on b, got %+v", sel)
	}

	ctx := b.editorEval().ctx(nil)

	if ctx["fileName"] != "a" {
		t.Fatalf("fileName = %v, want a (bound to the editor's file, not the selection)", ctx["fileName"])
	}
	if path, _ := ctx["filePath"].(string); !strings.HasSuffix(path, "a.md") {
		t.Fatalf("filePath = %v, want it to point at a.md", ctx["filePath"])
	}
}

func TestBuildEditorEvalCtx_ResolvesByStableItemIdentity(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "alpha"})
	colB := newTestColumn(t, map[string]string{"a": "bravo", "z": "zed"})
	b := &Board{columns: []*Column{colA, colB}}
	b.editor = NewEditor(false)
	item := colB.ItemByName("a")
	b.editor.ColIndex = 1
	b.editor.ColPath = colB.Path
	b.editor.FileName = item.Name
	b.editor.ItemPath = item.FullPath

	b.columns = []*Column{colB, colA}
	colB.SelectByName("z")
	ctx := b.editorEval().ctx(nil)

	if ctx["columnPath"] != colB.Path {
		t.Fatalf("columnPath = %v, want stable target %q", ctx["columnPath"], colB.Path)
	}
	if path, _ := ctx["filePath"].(string); !strings.HasPrefix(path, colB.Path) {
		t.Fatalf("filePath = %v, want item in %q", ctx["filePath"], colB.Path)
	}
	if ctx["fileName"] != "a" {
		t.Fatalf("fileName = %v, want edited item a despite current selection", ctx["fileName"])
	}
}
