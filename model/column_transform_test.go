package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/config"
)

// newTransformBoard builds a board over one "todo" column with three unpinned
// cards carrying a `priority` frontmatter key plus one pinned card, and the
// given .kbrd.lua body driving the real Lua host.
func newTransformBoard(t *testing.T, luaBody string) *Board {
	t.Helper()
	dir := t.TempDir()
	todo := filepath.Join(dir, "todo")
	if err := os.Mkdir(todo, 0o755); err != nil {
		t.Fatal(err)
	}
	cards := map[string]string{
		"alpha":   "---\npriority: 2\n---\nbody",
		"beta":    "---\npriority: 1\n---\nbody",
		"gamma":   "---\npriority: 3\n---\nbody",
		"p_first": "pinned body",
	}
	for name, content := range cards {
		if err := os.WriteFile(filepath.Join(todo, name+".md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(luaBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{Path: dir, ColumnWidth: 20, PreviewLines: 3}
	cfg.Scripting = config.ScriptingConfig{Enabled: true, CommandTimeoutMs: 2000, HookTimeoutMs: 500, InstructionLimit: 10_000_000}
	b := NewBoard(cfg)
	if b.scripts == nil {
		t.Fatal("scripting host not initialized")
	}
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestColumnTransform_SortByPriority(t *testing.T) {
	b := newTransformBoard(t, `
		kbrd.on("column_items", function(ev)
			table.sort(ev.items, function(a, c)
				return (a.data.priority or 99) < (c.data.priority or 99)
			end)
			return ev.items
		end)`)

	b.applyColumnTransforms()

	got := itemNames(b.columns[0].Items)
	// Pinned stays on top; unpinned follow script order (priority asc).
	want := []string{"first", "beta", "alpha", "gamma"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got order %v, want %v", got, want)
	}
}

func TestColumnTransform_FilterAndSeparator(t *testing.T) {
	b := newTransformBoard(t, `
		kbrd.on("column_items", function(ev)
			local out = { {separator = true, title = "urgent"} }
			for _, it in ipairs(ev.items) do
				if it.data.priority and it.data.priority <= 2 then
					table.insert(out, it)
				end
			end
			return out
		end)`)

	b.applyColumnTransforms()

	col := b.columns[0]
	got := itemNames(col.Items)
	// pinned, separator (empty name), alpha+beta in original order; gamma hidden.
	if len(got) != 4 || got[0] != "first" || got[2] != "alpha" || got[3] != "beta" {
		t.Fatalf("unexpected order: %v", got)
	}
	if !col.Items[1].Separator || col.Items[1].Title != "urgent" {
		t.Fatalf("expected separator at index 1, got %+v", col.Items[1])
	}

	// Re-applying must not stack separators or resurrect hidden items.
	b.applyColumnTransforms()
	if again := itemNames(b.columns[0].Items); len(again) != 4 {
		t.Fatalf("transform not idempotent: %v", again)
	}
}

func TestColumnTransform_PinnedImmuneToHiding(t *testing.T) {
	// A hook that returns an empty list hides every unpinned item — but the
	// pinned group must survive untouched.
	b := newTransformBoard(t, `kbrd.on("column_items", function(ev) return {} end)`)

	b.applyColumnTransforms()

	got := itemNames(b.columns[0].Items)
	if len(got) != 1 || got[0] != "first" {
		t.Fatalf("expected only pinned item to remain, got %v", got)
	}
}

func TestColumnTransform_UnknownEntriesIgnored(t *testing.T) {
	b := newTransformBoard(t, `
		kbrd.on("column_items", function(ev)
			return {
				{name = "ghost"},          -- unknown → ignored
				{name = "alpha"},
				{name = "alpha"},          -- duplicate → ignored
				{name = "first"},          -- pinned → ignored (already on top)
				{name = "beta"},
			}
		end)`)

	b.applyColumnTransforms()

	got := itemNames(b.columns[0].Items)
	want := []string{"first", "alpha", "beta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestColumnTransform_HeaderMarker(t *testing.T) {
	// The hook transforms only while the marker file exists, so the test can
	// observe the ƒ marker appearing and then clearing on the next apply.
	b := newTransformBoard(t, `
		kbrd.on("column_items", function(ev)
			if kbrd.fs.exists("transform-on") then return ev.items end
			return nil
		end)`)
	on := filepath.Join(b.cfg.Path, "transform-on")
	col := b.columns[0]

	b.applyColumnTransforms()
	if col.transformed {
		t.Fatal("marker set although the hook declined")
	}

	if err := os.WriteFile(on, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	b.applyColumnTransforms()
	if !col.transformed {
		t.Fatal("marker not set after the hook transformed the column")
	}
	if header := col.renderHeader(false, 0, 40); !strings.Contains(header, "ƒ") {
		t.Fatalf("expected ƒ in header, got %q", header)
	}

	if err := os.Remove(on); err != nil {
		t.Fatal(err)
	}
	b.applyColumnTransforms()
	if col.transformed {
		t.Fatal("marker not cleared after the hook declined again")
	}
	if header := col.renderHeader(false, 0, 40); strings.Contains(header, "ƒ") {
		t.Fatalf("ƒ still in header after clearing: %q", header)
	}
}

func TestColumnTransform_ErrorFallsBackToDefault(t *testing.T) {
	b := newTransformBoard(t, `kbrd.on("column_items", function(ev) error("boom") end)`)

	before := itemNames(b.columns[0].Items)
	b.applyColumnTransforms()
	after := itemNames(b.columns[0].Items)
	if strings.Join(before, ",") != strings.Join(after, ",") {
		t.Fatalf("order changed despite hook error: %v -> %v", before, after)
	}
}

func TestColumnTransform_NoHookIsNoop(t *testing.T) {
	b := newTransformBoard(t, `-- no hooks registered`)

	before := itemNames(b.columns[0].Items)
	b.applyColumnTransforms()
	after := itemNames(b.columns[0].Items)
	if strings.Join(before, ",") != strings.Join(after, ",") {
		t.Fatalf("order changed with no hook: %v -> %v", before, after)
	}
}

func TestColumnTransform_SkipsVirtualColumns(t *testing.T) {
	// The hook would reverse anything it gets; the virtual column's
	// script-pushed order must be left alone (and the hook must not fire for
	// it at all — firing would hand it filesystem-shaped payloads).
	b := newTransformBoard(t, `
		kbrd.column.set("v", { name = "V", items = { {id="z"}, {id="a"} } })
		kbrd.on("column_items", function(ev)
			if ev.column == "V" then error("must not fire for virtual columns") end
			return nil
		end)`)
	b.appendVirtualColumns()

	b.applyColumnTransforms()

	for _, col := range b.columns {
		if col.Virtual {
			if got := itemNames(col.Items); strings.Join(got, ",") != "z,a" {
				t.Fatalf("virtual column order changed: %v", got)
			}
		}
	}
}

func TestColumnTransform_PendingDrainAfterScript(t *testing.T) {
	// kbrd.board.refresh() from a command body reloads columns while the
	// script is still running — the transform cannot enter the busy VM, so it
	// must go pending and be applied by drainColumnTransform afterwards.
	b := newTransformBoard(t, `
		kbrd.on("column_items", function(ev)
			return { {name = "gamma"}, {name = "beta"}, {name = "alpha"} }
		end)
		kbrd.command("r", "Refresh", function(ctx) kbrd.board.refresh() end)`)

	cmds := b.scripts.Commands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if _, err := b.scripts.RunCommand(cmds[0].LuaRef, map[string]string{}); err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if !b.transformPending {
		t.Fatal("expected transformPending after in-script refresh")
	}

	b.drainColumnTransform()
	if b.transformPending {
		t.Fatal("expected pending flag cleared after drain")
	}
	got := itemNames(b.columns[0].Items)
	want := []string{"first", "gamma", "beta", "alpha"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}
