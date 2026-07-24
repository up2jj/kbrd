package model

import (
	"strings"
	"time"

	"kbrd/board"
)

// journalStampWith resolves the timestamp and body for a journal entry given the
// detect-date setting. When detectDate is on it reads a leading natural-language
// date off the text (board.DetectDate); otherwise it stamps the current time and
// keeps text as-is. The flag is a parameter (not read from Board) so a worker
// goroutine can be handed a value captured on the UI goroutine, never racing a
// concurrent config reload / board switch.
func journalStampWith(detectDate bool, text string) (time.Time, string) {
	return journalStampAtWith(detectDate, text, time.Now())
}

func journalStampAtWith(detectDate bool, text string, now time.Time) (time.Time, string) {
	if detectDate {
		return board.DetectDate(text, now)
	}
	return now, text
}

// journalEntriesWith turns every non-empty editor line into its own entry. A
// single reference time keeps undated lines from acquiring subtly different
// timestamps while date detection is still applied independently per line.
func journalEntriesWith(detectDate bool, text string) []board.JournalEntry {
	now := time.Now()
	entries := make([]board.JournalEntry, 0, strings.Count(text, "\n")+1)
	for line := range strings.Lines(text) {
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		at, body := journalStampAtWith(detectDate, line, now)
		entries = append(entries, board.JournalEntry{At: at, Text: body})
	}
	return entries
}

// journalStamp is the UI-goroutine wrapper that reads the live config. Callers on
// a worker goroutine must capture b.cfg.Journal.DetectDate first and use
// journalStampWith instead.
func (b *Board) journalStamp(text string) (time.Time, string) {
	return journalStampWith(b.cfg.Journal.DetectDate, text)
}
