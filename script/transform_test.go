package script

import (
	"strings"
	"testing"
)

// fireItems is a small helper: fire column_items for a column with three
// unpinned items (a, b, c) and one pinned (p) as context.
func fireItems(h *Host, column string) ColumnItemsResult {
	pinned := []map[string]interface{}{
		{"name": "p", "title": "p", "pinned": true, "path": "/col/p_p.md"},
	}
	unpinned := []map[string]interface{}{
		{"name": "a", "title": "a", "pinned": false, "path": "/col/a.md", "data": map[string]interface{}{"priority": 2}},
		{"name": "b", "title": "b", "pinned": false, "path": "/col/b.md", "data": map[string]interface{}{"priority": 1}},
		{"name": "c", "title": "c", "pinned": false, "path": "/col/c.md", "data": map[string]interface{}{"priority": 3}},
	}
	return h.FireColumnItems(column, pinned, unpinned)
}

func TestFireColumnItemsNoHook(t *testing.T) {
	dir := writeInit(t, `kbrd.on("board_load", function() end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	res := fireItems(h, "TODO")
	if res.Changed || res.Skipped {
		t.Fatalf("expected untouched result, got %+v", res)
	}
}

func TestFireColumnItemsReorder(t *testing.T) {
	dir := writeInit(t, `
		kbrd.on("column_items", function(ev)
			if ev.column ~= "TODO" then return nil end
			table.sort(ev.items, function(x, y)
				return x.data.priority < y.data.priority
			end)
			return ev.items
		end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	res := fireItems(h, "TODO")
	if !res.Changed {
		t.Fatal("expected Changed")
	}
	got := make([]string, len(res.Items))
	for i, ci := range res.Items {
		got[i] = ci.Name
	}
	if strings.Join(got, ",") != "b,a,c" {
		t.Fatalf("expected priority order b,a,c, got %v", got)
	}

	// A different column: the hook declines (returns nil).
	if res := fireItems(h, "DONE"); res.Changed {
		t.Fatal("expected unchanged result for declined column")
	}
}

func TestFireColumnItemsFilterAndSeparator(t *testing.T) {
	dir := writeInit(t, `
		kbrd.on("column_items", function(ev)
			return {
				{separator = true, title = "Hot"},
				ev.items[3],
				ev.items[1],
				-- ev.items[2] omitted → hidden
			}
		end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	res := fireItems(h, "TODO")
	if !res.Changed || len(res.Items) != 3 {
		t.Fatalf("expected 3 entries, got %+v", res)
	}
	if !res.Items[0].Separator || res.Items[0].Title != "Hot" {
		t.Fatalf("expected separator first, got %+v", res.Items[0])
	}
	if res.Items[1].Name != "c" || res.Items[2].Name != "a" {
		t.Fatalf("unexpected order: %+v", res.Items)
	}
}

func TestFireColumnItemsPinnedContext(t *testing.T) {
	dir := writeInit(t, `
		kbrd.on("column_items", function(ev)
			-- echo what the hook sees so the test can assert on the payload
			kbrd.notify("pinned:" .. #ev.pinned .. " items:" .. #ev.items, "info")
			return nil
		end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	fireItems(h, "TODO")
	want := "info:pinned:1 items:3"
	if len(api.notifies) != 1 || api.notifies[0] != want {
		t.Fatalf("expected %q, got %v", want, api.notifies)
	}
}

func TestFireColumnItemsLuaError(t *testing.T) {
	dir := writeInit(t, `kbrd.on("column_items", function(ev) error("boom") end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	res := fireItems(h, "TODO")
	if res.Changed {
		t.Fatal("expected unchanged result on error")
	}
	if len(api.notifies) == 0 || !strings.Contains(api.notifies[0], "column_items") {
		t.Fatalf("expected error notify, got %v", api.notifies)
	}
}

func TestFireColumnItemsMalformedReturn(t *testing.T) {
	dir := writeInit(t, `kbrd.on("column_items", function(ev) return "nope" end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	res := fireItems(h, "TODO")
	if res.Changed {
		t.Fatal("expected unchanged result on malformed return")
	}
	if len(api.notifies) == 0 || !strings.Contains(api.notifies[0], "must return a table or nil") {
		t.Fatalf("expected malformed notify, got %v", api.notifies)
	}
}

func TestFireColumnItemsErrorDisablesHook(t *testing.T) {
	dir := writeInit(t, `kbrd.on("column_items", function(ev) error("boom") end)`)
	api := &fakeAPI{}
	cfg := defaultCfg()
	cfg.ErrorThreshold = 2
	h, err := New(cfg, api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	fireItems(h, "TODO")
	fireItems(h, "TODO") // second consecutive error hits the threshold
	if got := len(h.hooks["column_items"]); got != 0 {
		t.Fatalf("expected hook disabled, still %d registered", got)
	}
	found := false
	for _, n := range api.notifies {
		if strings.Contains(n, "disabled after 2 errors") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected disable notify, got %v", api.notifies)
	}
}

func TestFireColumnItemsSkippedWhileRunning(t *testing.T) {
	dir := writeInit(t, `kbrd.on("column_items", function(ev) return {} end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	h.running = true
	res := fireItems(h, "TODO")
	if !res.Skipped || res.Changed {
		t.Fatalf("expected Skipped while running, got %+v", res)
	}
	if !h.Busy() {
		t.Fatal("expected Busy while running")
	}
	h.running = false
	if res := fireItems(h, "TODO"); !res.Changed {
		t.Fatal("expected Changed once idle")
	}
}

func TestFireColumnItemsFirstTableWins(t *testing.T) {
	dir := writeInit(t, `
		kbrd.on("column_items", function(ev) return nil end) -- declines
		kbrd.on("column_items", function(ev) return { ev.items[2] } end)
		kbrd.on("column_items", function(ev) return { ev.items[3] } end) -- never reached`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	res := fireItems(h, "TODO")
	if !res.Changed || len(res.Items) != 1 || res.Items[0].Name != "b" {
		t.Fatalf("expected first non-nil return (b) to win, got %+v", res)
	}
}
