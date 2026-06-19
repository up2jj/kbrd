package model

import (
	"time"

	"kbrd/board"
)

// journalStamp resolves the timestamp and body for a journal entry. When
// journal.detect_date is on it reads a leading natural-language date off the text
// (board.DetectDate); otherwise it stamps the current time and keeps text as-is.
func (b *Board) journalStamp(text string) (time.Time, string) {
	if b.cfg.Journal.DetectDate {
		return board.DetectDate(text, time.Now())
	}
	return time.Now(), text
}
