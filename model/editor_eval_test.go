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
	b.editor.FileName = "a"

	// Selection moves to "b" while the editor stays open on "a".
	col.SelectByName("b")
	if sel := col.SelectedItem(); sel == nil || sel.Name != "b" {
		t.Fatalf("precondition: expected selection on b, got %+v", sel)
	}

	ctx := b.buildEditorEvalCtx(nil)

	if ctx["fileName"] != "a" {
		t.Fatalf("fileName = %v, want a (bound to the editor's file, not the selection)", ctx["fileName"])
	}
	if path, _ := ctx["filePath"].(string); !strings.HasSuffix(path, "a.md") {
		t.Fatalf("filePath = %v, want it to point at a.md", ctx["filePath"])
	}
}
