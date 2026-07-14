package reminders

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kbrd/board"
	"kbrd/config"
	"kbrd/frontmatter"
	kbrdfs "kbrd/fs"
	"kbrd/natdate"
)

func scanCards(boardPath string, cfg config.RemindersConfig, now time.Time) ([]Card, error) {
	done := make(map[string]bool, len(cfg.DoneColumns))
	for _, name := range cfg.DoneColumns {
		done[strings.ToLower(strings.TrimSpace(name))] = true
	}

	items, err := board.ScanItems(boardPath, func(item board.ScannedItem) bool {
		data := item.Frontmatter.Data
		return stringValue(data[FrontmatterIDKey]) != "" || data["due"] != nil
	})
	if err != nil {
		return nil, err
	}
	cards := make([]Card, 0, len(items))
	for _, item := range items {
		data := item.Frontmatter.Data
		due, err := parseDue(data["due"], now)
		if err != nil {
			return nil, fmt.Errorf("card %s due: %w", item.Path, err)
		}
		cards = append(cards, Card{
			Path: item.Path, Column: item.Column, Name: item.Name,
			SyncID: stringValue(data[FrontmatterIDKey]), Title: item.Name,
			Body: strings.TrimSpace(item.Body), Due: due.Value, DueRelative: due.Relative,
			Priority:  intValue(data["priority"]),
			Completed: done[strings.ToLower(item.Column)], Raw: item.Raw,
		})
	}
	return cards, nil
}

func normalizeDue(raw any, now time.Time) (string, error) {
	due, err := parseDue(raw, now)
	return due.Value, err
}

type normalizedDue struct {
	Value    string
	Relative bool
}

func parseDue(raw any, now time.Time) (normalizedDue, error) {
	if raw == nil {
		return normalizedDue{}, nil
	}
	if value, ok := raw.(time.Time); ok {
		if value.Hour() == 0 && value.Minute() == 0 && value.Second() == 0 && value.Nanosecond() == 0 {
			return normalizedDue{Value: value.Format(time.DateOnly)}, nil
		}
		return normalizedDue{Value: formatTimedDue(value)}, nil
	}
	value := stringValue(raw)
	if value == "" {
		return normalizedDue{}, nil
	}
	if t, err := time.ParseInLocation(time.DateOnly, value, now.Location()); err == nil {
		return normalizedDue{Value: t.Format(time.DateOnly)}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return normalizedDue{Value: formatTimedDue(t)}, nil
	}
	for _, layout := range []string{
		"2006-01-02 15:04", "2006-01-02 15:04:05",
		"2006-01-02T15:04", "2006-01-02T15:04:05",
	} {
		if t, err := time.ParseInLocation(layout, value, now.Location()); err == nil {
			return normalizedDue{Value: formatTimedDue(t)}, nil
		}
	}
	t, err := natdate.Parse(value, now)
	if err != nil {
		return normalizedDue{}, err
	}
	due := normalizedDue{Relative: !hasAbsoluteDate(value)}
	if hasExplicitClock(value) {
		due.Value = formatTimedDue(t)
		return due, nil
	}
	due.Value = t.Format(time.DateOnly)
	return due, nil
}

func formatTimedDue(value time.Time) string {
	return value.UTC().Truncate(time.Second).Format(time.RFC3339)
}

func hasExplicitClock(value string) bool {
	for _, token := range strings.Fields(strings.ToLower(value)) {
		token = strings.Trim(token, ",.;()")
		for _, layout := range []string{"15:04", "3pm", "3:04pm"} {
			if _, err := time.Parse(layout, token); err == nil {
				return true
			}
		}
	}
	return false
}

func hasAbsoluteDate(value string) bool {
	for _, token := range strings.Fields(value) {
		token = strings.Trim(token, ",;()")
		for _, layout := range []string{time.DateOnly, "2006/01/02", "02.01.2006"} {
			if _, err := time.Parse(layout, token); err == nil {
				return true
			}
		}
	}
	return false
}

func stringValue(v any) string {
	switch v := v.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func intValue(v any) int {
	switch v := v.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}

func newSyncID() string {
	return strings.ToLower(rand.Text())
}

func setCardIdentity(card *Card, id string) error {
	updated := card.Raw
	if card.DueRelative {
		updated = frontmatter.Set(updated, "due", strconv.Quote(card.Due))
	}
	updated = frontmatter.Set(updated, FrontmatterIDKey, strconv.Quote(id))
	if err := kbrdfs.WriteExistingFileAtomicDurable(card.Path, []byte(updated)); err != nil {
		return fmt.Errorf("write reminder metadata to %s: %w", card.Path, err)
	}
	card.SyncID = id
	card.DueRelative = false
	card.Raw = updated
	return nil
}

func writeRemoteToCard(card *Card, reminder Reminder, cfg config.RemindersConfig) error {
	raw := card.Raw
	if reminder.Due == "" {
		raw = frontmatter.Delete(raw, "due")
	} else {
		raw = frontmatter.Set(raw, "due", strconv.Quote(reminder.Due))
	}
	if reminder.Priority == 0 {
		raw = frontmatter.Delete(raw, "priority")
	} else {
		raw = frontmatter.Set(raw, "priority", strconv.Itoa(reminder.Priority))
	}
	raw = replaceBody(raw, reminder.Body)
	if err := kbrdfs.WriteExistingFileAtomicDurable(card.Path, []byte(raw)); err != nil {
		return fmt.Errorf("update card %s: %w", card.Path, err)
	}
	card.Raw, card.Body, card.Due, card.Priority = raw, reminder.Body, reminder.Due, reminder.Priority

	wantColumn := card.Column
	if reminder.Completed && !card.Completed {
		wantColumn = firstConfiguredColumn(cfg.DoneColumns)
	} else if !reminder.Completed && card.Completed {
		wantColumn = cfg.InboxColumn
	}
	if wantColumn != "" && !strings.EqualFold(wantColumn, card.Column) {
		dest := filepath.Join(filepath.Dir(filepath.Dir(card.Path)), wantColumn)
		if info, err := os.Stat(dest); err != nil || !info.IsDir() {
			return fmt.Errorf("reminders target column %q does not exist", wantColumn)
		}
		if err := board.MoveItem(filepath.Dir(card.Path), dest, card.Name); err != nil {
			return fmt.Errorf("move card %s to %s: %w", card.Name, wantColumn, err)
		}
		card.Column = wantColumn
		card.Path = filepath.Join(dest, card.Name+".md")
		card.Completed = reminder.Completed
	}
	cleanTitle, err := board.SanitizeName(reminder.Title)
	if err != nil {
		return fmt.Errorf("reminder title %q is not a valid card name: %w", reminder.Title, err)
	}
	if cleanTitle != card.Name {
		columnPath := filepath.Dir(card.Path)
		if err := board.RenameItem(columnPath, card.Name, cleanTitle); err != nil {
			return fmt.Errorf("rename card %s to %s: %w", card.Name, cleanTitle, err)
		}
		card.Name = cleanTitle
		card.Title = cleanTitle
		card.Path = filepath.Join(columnPath, cleanTitle+".md")
	}
	return nil
}

func createCardFromReminder(boardPath string, reminder Reminder, cfg config.RemindersConfig) (Card, error) {
	column := cfg.InboxColumn
	if reminder.Completed {
		column = firstConfiguredColumn(cfg.DoneColumns)
	}
	columnPath := filepath.Join(boardPath, column)
	if info, err := os.Stat(columnPath); err != nil || !info.IsDir() {
		return Card{}, fmt.Errorf("reminders target column %q does not exist", column)
	}
	raw := strings.TrimSpace(reminder.Body)
	raw = frontmatter.Set(raw, FrontmatterIDKey, strconv.Quote(reminder.SyncID))
	if reminder.Due != "" {
		raw = frontmatter.Set(raw, "due", strconv.Quote(reminder.Due))
	}
	if reminder.Priority != 0 {
		raw = frontmatter.Set(raw, "priority", strconv.Itoa(reminder.Priority))
	}
	path, err := board.CreateItem(columnPath, reminder.Title, raw)
	if err != nil {
		return Card{}, fmt.Errorf("create card for reminder %q: %w", reminder.Title, err)
	}
	name := strings.TrimSuffix(filepath.Base(path), ".md")
	return Card{Path: path, Column: column, Name: name, SyncID: reminder.SyncID,
		Title: name, Body: strings.TrimSpace(reminder.Body), Due: reminder.Due,
		Priority: reminder.Priority, Completed: reminder.Completed, Raw: raw}, nil
}

func replaceBody(raw, body string) string {
	front, _, fenced := frontmatter.Split(raw)
	body = strings.TrimSpace(body)
	if !fenced {
		if body == "" {
			return ""
		}
		return body + "\n"
	}
	if body == "" {
		return "---\n" + front + "---\n"
	}
	return "---\n" + front + "---\n\n" + body + "\n"
}

func firstConfiguredColumn(names []string) string {
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			return name
		}
	}
	return ""
}
