package model

import "testing"

func TestSlotWidth(t *testing.T) {
	t.Parallel()
	if got := slotWidth(32); got != 35 {
		t.Errorf("slotWidth(32) = %d, want 35", got)
	}
}

// uniform32 is a widthOf that gives every column a 32-wide content (slot 35),
// reproducing the old fixed-width geometry packWindow must still satisfy.
func uniform32(int) int { return 32 }

// TestPackWindow_Count covers how many uniform columns fit in a terminal —
// the contract the old visibleCount held. Selecting column 0 from the left
// edge isolates the fit count from the keep-selected-visible sliding.
func TestPackWindow_Count(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		termWidth, numCols int
		want               int
	}{
		{"three fit in 120", 120, 10, 3},
		{"two fit in 80", 80, 10, 2},
		{"five fit in 200", 200, 10, 5},
		{"at least one on narrow terminal", 30, 10, 1},
		{"capped at column count", 500, 3, 3},
		{"zero width falls back to 80", 0, 10, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			first, count := packWindow(tt.termWidth, 0, 0, tt.numCols, uniform32)
			if first != 0 {
				t.Errorf("first = %d, want 0", first)
			}
			if count != tt.want {
				t.Errorf("packWindow count = %d, want %d", count, tt.want)
			}
		})
	}
}

// TestPackWindow_Window covers the keep-selected-visible sliding that the old
// clampFirstVisible held. termWidth is chosen so a uniform window holds 3
// columns (120) or 5 (200), matching the counts those cases assumed.
func TestPackWindow_Window(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                              string
		termWidth, selected, firstVisible int
		numCols                           int
		wantFirst, wantCount              int
	}{
		{"selection within window", 120, 3, 2, 10, 2, 3},
		{"selection left of window", 120, 2, 5, 10, 2, 3},
		{"selection right of window", 120, 7, 0, 10, 5, 3},
		{"window clamped at end", 120, 9, 9, 10, 7, 3},
		{"negative first clamps to zero", 120, 0, -2, 10, 0, 3},
		{"window wider than columns", 200, 0, 1, 3, 0, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			first, count := packWindow(tt.termWidth, tt.selected, tt.firstVisible, tt.numCols, uniform32)
			if first != tt.wantFirst || count != tt.wantCount {
				t.Errorf("packWindow(%d, sel=%d, fv=%d, n=%d) = (%d, %d), want (%d, %d)",
					tt.termWidth, tt.selected, tt.firstVisible, tt.numCols, first, count, tt.wantFirst, tt.wantCount)
			}
		})
	}
}

// TestPackWindow_VariableWidth covers a wide column that crowds out neighbors
// and stays visible when selected, with the window sliding to fit it.
func TestPackWindow_VariableWidth(t *testing.T) {
	t.Parallel()
	// Column 2 is 60 wide (slot 63); the rest are 32 (slot 35). avail at term
	// 120 is 114, so the wide column plus only one neighbor fit.
	widthOf := func(i int) int {
		if i == 2 {
			return 60
		}
		return 32
	}
	first, count := packWindow(120, 2, 0, 5, widthOf)
	if first != 1 || count != 2 {
		t.Fatalf("packWindow with wide selected col = (%d, %d), want (1, 2)", first, count)
	}
}

func TestGutterWidth(t *testing.T) {
	t.Parallel()
	if got := gutterWidth(0); got != 2 {
		t.Errorf("gutterWidth(0) = %d, want 2", got)
	}
	if got := gutterWidth(3); got != 4 {
		t.Errorf("gutterWidth(3) = %d, want 4", got)
	}
}

func TestZoomedColumnWidth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                      string
		termWidth, normalColWidth int
		want                      int
	}{
		{"wide terminal caps at zoomMaxWidth", 300, 32, zoomMaxWidth},
		{"mid terminal leaves edge reserve", 90, 32, 90 - zoomEdgeReserve},
		{"narrow terminal floors at normal width", 30, 32, 32},
		{"zero width falls back to 80", 0, 32, 80 - zoomEdgeReserve},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := zoomedColumnWidth(tt.termWidth, tt.normalColWidth); got != tt.want {
				t.Errorf("zoomedColumnWidth(%d, %d) = %d, want %d", tt.termWidth, tt.normalColWidth, got, tt.want)
			}
		})
	}
}

func TestComputeSlots_Normal(t *testing.T) {
	t.Parallel()
	slots, first := computeSlots(false, 120, 4, 2, 10, uniform32)
	if first != 2 {
		t.Fatalf("first = %d, want 2", first)
	}
	if len(slots) != 3 {
		t.Fatalf("len(slots) = %d, want 3", len(slots))
	}
	for i, s := range slots {
		if s.Col != first+i {
			t.Errorf("slots[%d].Col = %d, want %d", i, s.Col, first+i)
		}
		if s.Width != 32 {
			t.Errorf("slots[%d].Width = %d, want 32", i, s.Width)
		}
		if s.PreviewLines != 1 {
			t.Errorf("slots[%d].PreviewLines = %d, want 1", i, s.PreviewLines)
		}
	}
}

func TestComputeSlots_Zoomed(t *testing.T) {
	t.Parallel()
	slots, first := computeSlots(true, 120, 4, 2, 10, uniform32)
	if first != 2 {
		t.Errorf("zoom must not disturb firstVisible: got %d, want 2", first)
	}
	if len(slots) != 1 {
		t.Fatalf("len(slots) = %d, want 1", len(slots))
	}
	s := slots[0]
	if s.Col != 4 {
		t.Errorf("Col = %d, want selected column 4", s.Col)
	}
	if want := zoomedColumnWidth(120, 32); s.Width != want {
		t.Errorf("Width = %d, want %d", s.Width, want)
	}
	if s.PreviewLines != zoomPreviewLines {
		t.Errorf("PreviewLines = %d, want %d", s.PreviewLines, zoomPreviewLines)
	}
}

func TestComputeSlots_NoColumns(t *testing.T) {
	t.Parallel()
	slots, first := computeSlots(false, 120, 0, 0, 0, uniform32)
	if slots != nil || first != 0 {
		t.Errorf("computeSlots with no columns = (%v, %d), want (nil, 0)", slots, first)
	}
}

func TestZoomToggle(t *testing.T) {
	t.Parallel()
	var z Zoom
	if z.Active() {
		t.Error("zero-value Zoom should be off")
	}
	z.Toggle()
	if !z.Active() {
		t.Error("Toggle should turn zoom on")
	}
	z.Toggle()
	if z.Active() {
		t.Error("second Toggle should turn zoom off")
	}
	z.Toggle()
	z.Off()
	if z.Active() {
		t.Error("Off should turn zoom off")
	}
}

func TestCardRows(t *testing.T) {
	t.Parallel()
	if got := cardRows(0); got != 5 {
		t.Errorf("cardRows(0) = %d, want 5 (compact default)", got)
	}
	if got := cardRows(1); got != 5 {
		t.Errorf("cardRows(1) = %d, want 5", got)
	}
	if got := cardRows(zoomPreviewLines); got != zoomPreviewLines+4 {
		t.Errorf("cardRows(%d) = %d, want %d", zoomPreviewLines, got, zoomPreviewLines+4)
	}
}
