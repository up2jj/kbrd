package vlist

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// fake is a test Delegate: each item has a height, a selectable flag, and a
// filter string. Render emits exactly height rows so offsets line up.
type fake struct {
	items []fakeItem
}

type fakeItem struct {
	h    int
	sel  bool
	fv   string
	name string
}

func (f fake) Len() int                 { return len(f.items) }
func (f fake) Height(i int) int         { return f.items[i].h }
func (f fake) FilterValue(i int) string { return f.items[i].fv }
func (f fake) Selectable(i int) bool    { return f.items[i].sel }
func (f fake) Render(i int, selected bool) string {
	rows := make([]string, f.items[i].h)
	for r := range rows {
		rows[r] = f.items[i].name
	}
	return strings.Join(rows, "\n")
}

func testKeys() KeyMap {
	return KeyMap{
		Up:   key.NewBinding(key.WithKeys("up", "k")),
		Down: key.NewBinding(key.WithKeys("down", "j")),
	}
}

func newModel(f fake, w, h int) Model {
	m := New(testKeys())
	m.SetSize(w, h)
	m.SetDelegate(f)
	m.Reload()
	return m
}

func press(m *Model, k string) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
}

func TestCursorSkipsNonSelectable(t *testing.T) {
	f := fake{items: []fakeItem{
		{h: 3, sel: true, fv: "a", name: "a"},
		{h: 1, sel: false, fv: "", name: "sep"}, // separator
		{h: 3, sel: true, fv: "b", name: "b"},
	}}
	m := newModel(f, 20, 40)

	if m.Index() != 0 {
		t.Fatalf("start index = %d, want 0", m.Index())
	}
	press(&m, "j") // one press should skip the separator
	if got := m.Index(); got != 2 {
		t.Fatalf("after one down, index = %d, want 2 (skipped separator)", got)
	}
	press(&m, "k")
	if got := m.Index(); got != 0 {
		t.Fatalf("after up, index = %d, want 0", got)
	}
}

func TestAtTopBottomWithSeparatorsAtEdges(t *testing.T) {
	f := fake{items: []fakeItem{
		{h: 1, sel: false, fv: "", name: "top-sep"},
		{h: 2, sel: true, fv: "a", name: "a"},
		{h: 1, sel: false, fv: "", name: "bot-sep"},
	}}
	m := newModel(f, 20, 40)
	// Cursor should have snapped onto the only selectable row (index 1).
	if m.Index() != 1 {
		t.Fatalf("index = %d, want 1 (snapped to selectable)", m.Index())
	}
	if !m.AtTop() {
		t.Errorf("AtTop = false, want true (no selectable above)")
	}
	if !m.AtBottom() {
		t.Errorf("AtBottom = false, want true (no selectable below)")
	}
}

func TestVariableHeightScrollKeepsCursorVisible(t *testing.T) {
	// Five 5-row cards in a 12-row viewport: only ~2 fit. Moving to the last
	// must scroll so the cursor row is within the viewport.
	items := make([]fakeItem, 5)
	for i := range items {
		items[i] = fakeItem{h: 5, sel: true, fv: "x", name: "x"}
	}
	m := newModel(fake{items: items}, 20, 12)

	m.SelectLast()
	m.View() // scrolling happens during View, after content is laid out

	top := m.offsetOf(m.Index()) // 4 * 5 = 20
	if top < m.vp.YOffset || top+5 > m.vp.YOffset+m.vp.Height {
		t.Fatalf("cursor row [%d,%d) not within viewport [%d,%d)",
			top, top+5, m.vp.YOffset, m.vp.YOffset+m.vp.Height)
	}
}

func TestFilterNarrowsAndExcludesEmpty(t *testing.T) {
	f := fake{items: []fakeItem{
		{h: 1, sel: true, fv: "apple", name: "apple"},
		{h: 1, sel: false, fv: "", name: "sep"}, // empty filter value: excluded
		{h: 1, sel: true, fv: "banana", name: "banana"},
		{h: 1, sel: true, fv: "grape", name: "grape"},
	}}
	m := newModel(f, 20, 40)

	m.BeginFilter()
	if !m.Filtering() {
		t.Fatal("Filtering = false after BeginFilter")
	}
	press(&m, "a") // matches apple, banana, grape all contain 'a'? grape has a; ok
	// Narrow harder.
	press(&m, "p")
	vis := m.Visible()
	for _, ui := range vis {
		if f.items[ui].fv == "" {
			t.Fatalf("separator (empty FilterValue) leaked into filtered set: %v", vis)
		}
	}
	if len(vis) == 0 {
		t.Fatal("no matches for 'ap', want at least apple")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // apply
	if m.Filtering() {
		t.Error("still Filtering after Enter")
	}
	if !m.Filtered() {
		t.Error("Filtered = false after applying a query")
	}

	m.ClearFilter()
	if m.Filtered() || m.Filtering() {
		t.Error("filter not cleared")
	}
	if len(m.Visible()) != 4 {
		t.Errorf("visible count = %d after clear, want 4", len(m.Visible()))
	}
}

func TestAboveBelow(t *testing.T) {
	// Six 3-row items in a 9-row viewport (3 visible). Scroll to the middle.
	items := make([]fakeItem, 6)
	for i := range items {
		items[i] = fakeItem{h: 3, sel: true, fv: "x", name: "x"}
	}
	m := newModel(fake{items: items}, 20, 9)
	m.View()           // lay out content so the viewport knows its line count
	m.vp.SetYOffset(6) // first two items (rows 0-5) fully above

	above, below := m.AboveBelow()
	if above != 2 {
		t.Errorf("above = %d, want 2", above)
	}
	if below != 1 {
		t.Errorf("below = %d, want 1", below)
	}
}

func TestHitTest(t *testing.T) {
	f := fake{items: []fakeItem{
		{h: 2, sel: true, fv: "a", name: "a"}, // rows 0-1
		{h: 4, sel: true, fv: "b", name: "b"}, // rows 2-5
		{h: 3, sel: true, fv: "c", name: "c"}, // rows 6-8
	}}
	m := newModel(f, 20, 40)

	cases := []struct {
		y    int
		want int
		ok   bool
	}{
		{0, 0, true},
		{1, 0, true},
		{2, 1, true},
		{5, 1, true},
		{6, 2, true},
		{8, 2, true},
		{9, 0, false}, // past the last item
	}
	for _, c := range cases {
		got, ok := m.HitTest(c.y)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("HitTest(%d) = (%d,%v), want (%d,%v)", c.y, got, ok, c.want, c.ok)
		}
	}
}
