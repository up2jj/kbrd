package model

import (
	"slices"
	"strings"
	"testing"
)

func TestBuiltinCellSlotsAreNegativeAndOrdered(t *testing.T) {
	seen := make(map[int]bool, builtinCellSlotCount)
	for slot := builtinCellSlot(0); slot < builtinCellSlotCount; slot++ {
		id := slot.id()
		if id >= 0 {
			t.Fatalf("slot %d ID = %d, want negative", slot, id)
		}
		if seen[id] {
			t.Fatalf("slot %d duplicates ID %d", slot, id)
		}
		seen[id] = true
	}
}

func TestCellBarBuiltinSlotsUseRegistryIDsAndRenderBeforeScripts(t *testing.T) {
	var bar CellBar
	bar.setBuiltin(builtinCellMCP, Cell{ID: 999, Text: "mcp"})
	bar.setBuiltin(builtinCellGitStatus, Cell{ID: 999, Text: "git"})
	bar.Set(Cell{ID: 1, Text: "script"})

	if got := bar.cells[builtinCellMCP.id()].ID; got != builtinCellMCP.id() {
		t.Fatalf("MCP cell ID = %d, want registry ID %d", got, builtinCellMCP.id())
	}
	if got, want := bar.sortedIDs(), []int{builtinCellMCP.id(), builtinCellGitStatus.id(), 1}; !slices.Equal(got, want) {
		t.Fatalf("sorted IDs = %v, want %v", got, want)
	}

	view := bar.render(80)
	mcp := strings.Index(view, "mcp")
	git := strings.Index(view, "git")
	script := strings.Index(view, "script")
	if mcp < 0 || git < 0 || script < 0 || !(mcp < git && git < script) {
		t.Fatalf("rendered order = %q, want mcp, git, script", view)
	}
}
