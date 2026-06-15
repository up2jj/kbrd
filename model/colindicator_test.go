package model

import (
	"strings"
	"testing"
)

func TestRenderHeaderShowsIndicator(t *testing.T) {
	col := newTestColumn(t, map[string]string{"a": "x"})

	// No indicator → label absent.
	if h := col.renderHeader(true, 0, 40, colIndicator{}); strings.Contains(h, "↓ prio") {
		t.Fatalf("indicator text leaked with empty indicator: %q", h)
	}

	// With an indicator → its text appears in the header.
	h := col.renderHeader(true, 0, 40, colIndicator{Text: "↓ prio"})
	if !strings.Contains(h, "↓ prio") {
		t.Fatalf("indicator text missing from header: %q", h)
	}
}

func TestColIndicatorsSetGetClear(t *testing.T) {
	var c colIndicators // zero value: nil map

	// get on a nil map is safe and reads as "none".
	if got := c.get("todo"); got.Text != "" {
		t.Errorf("nil-map get = %+v, want empty", got)
	}

	c.set("todo", colIndicator{Text: "↓ prio", FG: "#e0af68", Bold: true})
	got := c.get("todo")
	if got.Text != "↓ prio" || got.FG != "#e0af68" || !got.Bold {
		t.Errorf("get after set = %+v", got)
	}

	// Empty text clears the entry.
	c.set("todo", colIndicator{Text: ""})
	if got := c.get("todo"); got.Text != "" {
		t.Errorf("empty-text set did not clear: %+v", got)
	}

	c.set("a", colIndicator{Text: "x"})
	c.set("b", colIndicator{Text: "y"})
	c.clear("a")
	if c.get("a").Text != "" {
		t.Errorf("clear did not remove a")
	}
	if c.get("b").Text != "y" {
		t.Errorf("clear removed the wrong key")
	}

	c.clearAll()
	if c.get("b").Text != "" {
		t.Errorf("clearAll did not wipe")
	}
}
