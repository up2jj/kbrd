package vimbuf

import (
	"fmt"
	"strings"
	"testing"

	"kbrd/theme"
)

// A soft-wrapped line near the top of the bottom window must not strand the last
// line: wheeling to the bottom has to bring the final line into view. Regression
// for maxTop using ">= height" (which overshoots when a wrapped line straddles
// the fold) instead of "all remaining rows fit".
func TestScrollReachesLastLineWithWrap(t *testing.T) {
	long := strings.Repeat("verylongword ", 20) // wraps to 3 visual rows at textWidth 96
	for _, wrapLine := range []int{45, 46, 47, 60, 79} {
		var sb strings.Builder
		for i := 1; i <= 79; i++ {
			if i == wrapLine {
				fmt.Fprintf(&sb, "%d - [ ] %s\n", i, long)
			} else {
				fmt.Fprintf(&sb, "%d - [ ] item %d\n", i, i)
			}
		}
		b := New(strings.TrimRight(sb.String(), "\n"))
		b.SetSize(100, 36)
		for i := 0; i < 200; i++ { // wheel to the bottom, re-sizing like the render loop
			b.Scroll(3)
			b.SetSize(100, 36)
		}
		if view := b.View(theme.Palette{}); !strings.Contains(view, "79 - [ ]") {
			t.Errorf("wrapLine=%d: last line not visible after scrolling to bottom (top=%d maxTop=%d)",
				wrapLine, b.top, b.maxTop())
		}
	}
}
