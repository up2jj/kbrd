package model

import "testing"

func TestGroupedMenuNavNavigationAndViewport(t *testing.T) {
	nav := groupedMenuNav{nav: []int{1, 4, 7}}
	nav.Clamp(8)

	if !nav.UpdateKey("end") || nav.selected != 2 || !nav.follow {
		t.Fatalf("end navigation = selected %d, follow %t", nav.selected, nav.follow)
	}
	if row, ok := nav.SelectedRow(); !ok || row != 7 {
		t.Fatalf("selected row = (%d, %t), want (7, true)", row, ok)
	}
	if !nav.UpdateKey("down") || nav.selected != 0 {
		t.Fatalf("down from last selected %d, want 0", nav.selected)
	}
	if !nav.UpdateKey("up") || nav.selected != 2 {
		t.Fatalf("up from first selected %d, want 2", nav.selected)
	}

	nav.EnsureSelectedVisible(3)
	if nav.scroll != 5 {
		t.Fatalf("scroll after follow = %d, want 5", nav.scroll)
	}
	nav.ScrollBy(8, -2)
	if nav.scroll != 3 || nav.follow {
		t.Fatalf("scroll after wheel = (%d, %t), want (3, false)", nav.scroll, nav.follow)
	}
	if start, end := nav.Viewport(8, 3); start != 3 || end != 6 {
		t.Fatalf("viewport = [%d, %d), want [3, 6)", start, end)
	}
}

func TestGroupedMenuScrollbar(t *testing.T) {
	got := groupedMenuScrollbar(4, 10, 3, "-", "#")
	want := []string{"-", "#", "-", "-"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scrollbar[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
