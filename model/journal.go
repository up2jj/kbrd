package model

import (
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
	if detectDate {
		return board.DetectDate(text, time.Now())
	}
	return time.Now(), text
}

// journalStamp is the UI-goroutine wrapper that reads the live config. Callers on
// a worker goroutine must capture b.cfg.Journal.DetectDate first and use
// journalStampWith instead.
func (b *Board) journalStamp(text string) (time.Time, string) {
	return journalStampWith(b.cfg.Journal.DetectDate, text)
}
