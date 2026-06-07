package web

import "testing"

func TestFilterColumns(t *testing.T) {
	cols := []Column{
		{Name: "todo", Cards: []Card{
			{Name: "p_alpha", Title: "Alpha", Pinned: true, search: "alpha\nurgent\nfix the login page"},
			{Name: "beta", Title: "Beta", search: "beta\nrefactor the parser"},
		}},
		{Name: "done", Cards: []Card{
			{Name: "gamma", Title: "Gamma", search: "gamma\nlogin polish shipped"},
		}},
	}

	// Empty and blank queries are no-ops returning the input as-is.
	for _, q := range []string{"", "   "} {
		if got := filterColumns(cols, q); len(got[0].Cards) != 2 || len(got[1].Cards) != 1 {
			t.Errorf("q=%q filtered something", q)
		}
	}

	// Case-insensitive match across columns; order and names preserved.
	got := filterColumns(cols, "LOGIN")
	if got[0].Name != "todo" || got[1].Name != "done" {
		t.Fatal("column names/order changed")
	}
	if len(got[0].Cards) != 1 || got[0].Cards[0].Name != "p_alpha" {
		t.Errorf("todo matches: %+v", got[0].Cards)
	}
	if len(got[1].Cards) != 1 || got[1].Cards[0].Name != "gamma" {
		t.Errorf("done matches: %+v", got[1].Cards)
	}

	// No match: empty card slices, columns kept.
	got = filterColumns(cols, "nothing-here")
	if len(got) != 2 || len(got[0].Cards) != 0 || len(got[1].Cards) != 0 {
		t.Errorf("no-match result: %+v", got)
	}

	// Original input untouched.
	if len(cols[0].Cards) != 2 {
		t.Fatal("filterColumns mutated its input")
	}
}
