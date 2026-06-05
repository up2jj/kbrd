package model

import "testing"

func TestSlotWidth(t *testing.T) {
	t.Parallel()
	if got := slotWidth(32); got != 35 {
		t.Errorf("slotWidth(32) = %d, want 35", got)
	}
}

func TestVisibleCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                      string
		termWidth, slotW, numCols int
		want                      int
	}{
		{"three fit in 120", 120, 35, 10, 3},
		{"two fit in 80", 80, 35, 10, 2},
		{"five fit in 200", 200, 35, 10, 5},
		{"at least one on narrow terminal", 30, 35, 10, 1},
		{"capped at column count", 500, 35, 3, 3},
		{"zero width falls back to 80", 0, 35, 10, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := visibleCount(tt.termWidth, tt.slotW, tt.numCols); got != tt.want {
				t.Errorf("visibleCount(%d, %d, %d) = %d, want %d", tt.termWidth, tt.slotW, tt.numCols, got, tt.want)
			}
		})
	}
}

func TestClampFirstVisible(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                                   string
		firstVisible, selected, count, numCols int
		want                                   int
	}{
		{"selection within window", 2, 3, 3, 10, 2},
		{"selection left of window", 5, 2, 3, 10, 2},
		{"selection right of window", 0, 7, 3, 10, 5},
		{"window clamped at end", 9, 9, 3, 10, 7},
		{"negative first clamps to zero", -2, 0, 3, 10, 0},
		{"window wider than columns", 1, 0, 5, 3, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := clampFirstVisible(tt.firstVisible, tt.selected, tt.count, tt.numCols)
			if got != tt.want {
				t.Errorf("clampFirstVisible(%d, %d, %d, %d) = %d, want %d",
					tt.firstVisible, tt.selected, tt.count, tt.numCols, got, tt.want)
			}
		})
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
	slots, first := computeSlots(false, 120, 4, 2, 10, 32)
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
	slots, first := computeSlots(true, 120, 4, 2, 10, 32)
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
	slots, first := computeSlots(false, 120, 0, 0, 0, 32)
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
