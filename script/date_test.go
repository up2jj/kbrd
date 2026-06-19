package script

import (
	"testing"
	"time"
)

// TestDateParse verifies kbrd.date.parse resolves a natural-language phrase
// (English or Polish), honors an optional Go layout, and reports failure via the
// (nil, message) error-tuple convention.
func TestDateParse(t *testing.T) {
	dir := writeInit(t, `
today_default = kbrd.date.parse("today")
today_custom  = kbrd.date.parse("dziś", "2006/01/02")
bad, err = kbrd.date.parse("florble")
bad_is_nil = bad == nil
err_is_string = type(err) == "string"
`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if got, want := luaString(t, h, "today_default"), time.Now().Format("2006-01-02"); got != want {
		t.Errorf("today_default = %q, want %q", got, want)
	}
	if got, want := luaString(t, h, "today_custom"), time.Now().Format("2006/01/02"); got != want {
		t.Errorf("today_custom = %q, want %q", got, want)
	}
	if !luaBool(t, h, "bad_is_nil") {
		t.Error("expected nil result for unparseable phrase")
	}
	if !luaBool(t, h, "err_is_string") {
		t.Error("expected an error message string for unparseable phrase")
	}
}
