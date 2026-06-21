package model

import (
	"testing"
	"time"
)

// journalStampWith takes the detect-date flag as a parameter (captured on the UI
// goroutine) so a worker never reads live Board config. With detection off the
// text is kept verbatim and stamped now; with it on, a leading date is split off.
func TestJournalStampWith(t *testing.T) {
	t.Run("detection off keeps text and stamps now", func(t *testing.T) {
		before := time.Now()
		at, body := journalStampWith(false, "yesterday shipped it")
		if body != "yesterday shipped it" {
			t.Fatalf("body = %q, want the text unchanged", body)
		}
		if at.Before(before) {
			t.Fatalf("timestamp %s predates the call", at)
		}
	})

	t.Run("detection on splits a leading date off the body", func(t *testing.T) {
		_, body := journalStampWith(true, "yesterday shipped it")
		if body != "shipped it" {
			t.Fatalf("body = %q, want the date stripped", body)
		}
	})
}
