package model

import (
	"os"
	"strings"
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

func TestJournalEntriesWith(t *testing.T) {
	entries := journalEntriesWith(true, "yesterday shipped it\n\nnext monday call client\r\nreview notes")
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}
	wantBodies := []string{"shipped it", "call client", "review notes"}
	for i, want := range wantBodies {
		if entries[i].Text != want {
			t.Errorf("entry %d body = %q, want %q", i, entries[i].Text, want)
		}
	}
	if !entries[0].At.Before(entries[2].At) {
		t.Errorf("yesterday timestamp %s is not before undated timestamp %s", entries[0].At, entries[2].At)
	}
	if !entries[1].At.After(entries[2].At) {
		t.Errorf("next Monday timestamp %s is not after undated timestamp %s", entries[1].At, entries[2].At)
	}
}

func TestHandleJournalWritesEachEditorLineAsAnEntry(t *testing.T) {
	col := newTestColumn(t, map[string]string{"note": "body"})
	b := &Board{columns: []*Column{col}}
	item := col.ItemByName("note")

	b.mutationHandlers().handleJournal(editorJournalMsg{
		Target:   refForItem(col, item),
		FileName: "note",
		Text:     "first update\nsecond update",
	})

	content, err := os.ReadFile(item.FullPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %q, want body plus two journal entries", lines)
	}
	if !strings.HasSuffix(lines[1], " - first update") {
		t.Errorf("first entry = %q", lines[1])
	}
	if !strings.HasSuffix(lines[2], " - second update") {
		t.Errorf("second entry = %q", lines[2])
	}
}
