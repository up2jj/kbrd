package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"kbrd/board"
	"kbrd/frontmatter"
	"kbrd/natdate"
	"kbrd/notifyroute"
)

func (b *Board) handleNotificationAction(command notifyroute.Command) (tea.Model, tea.Cmd) {
	var switchCmd tea.Cmd
	if !samePath(command.BoardPath, b.cfg.Path) {
		cmd, err := b.session().loadBoard(command.BoardPath)
		if err != nil {
			return b, b.notifier.ErrorCause("open notification board", err)
		}
		switchCmd = cmd
	}

	var actionCmd tea.Cmd
	switch command.Action {
	case notifyroute.OpenCard:
		_, actionCmd = b.searchActions().activateFile(command.BoardPath, command.CardPath)
	case notifyroute.MarkDone:
		actionCmd = b.markNotificationCardDone(command.CardPath)
	case notifyroute.SnoozeDue:
		actionCmd = b.snoozeNotificationCard(command.CardPath, time.Now())
	case notifyroute.RetrySync:
		switch command.SyncKind {
		case "git":
			actionCmd = b.git.StartupSyncOnce()
			if actionCmd == nil {
				actionCmd = b.notifier.Error("Git sync cannot retry while the board is busy or has uncommitted changes")
			}
		case "reminders":
			actionCmd = b.startRemindersSync()
		}
	}
	return b, tea.Batch(switchCmd, actionCmd)
}

func (b *Board) notificationCardPath(path string) (string, error) {
	boardPath, err := filepath.Abs(b.cfg.Path)
	if err != nil {
		return "", err
	}
	cardPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(boardPath, cardPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("card is outside the board")
	}
	if filepath.Ext(cardPath) != ".md" {
		return "", fmt.Errorf("card is not a Markdown file")
	}
	if info, err := os.Stat(cardPath); err != nil || info.IsDir() {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("card path is a directory")
	}
	return cardPath, nil
}

func (b *Board) markNotificationCardDone(path string) tea.Cmd {
	cardPath, err := b.notificationCardPath(path)
	if err != nil {
		return b.notifier.ErrorCause("mark done", err)
	}
	done := ""
	for _, name := range b.cfg.Reminders.DoneColumns {
		if strings.TrimSpace(name) != "" {
			done = name
			break
		}
	}
	if done == "" {
		return b.notifier.Error("mark done: reminders.done_columns is empty")
	}
	destination, err := board.ResolveColumn(b.cfg.Path, done, false)
	if err != nil {
		return b.notifier.ErrorCause("mark done", err)
	}
	name := strings.TrimSuffix(filepath.Base(cardPath), filepath.Ext(cardPath))
	if !samePath(filepath.Dir(cardPath), destination) {
		if err := board.MoveItem(filepath.Dir(cardPath), destination, name); err != nil {
			return b.notifier.ErrorCause("mark done", err)
		}
		cardPath = filepath.Join(destination, filepath.Base(cardPath))
	}
	if err := b.loadColumns(); err != nil {
		return b.notifier.ErrorCause("refresh after mark done", err)
	}
	b.selectNotificationCard(cardPath)
	return b.notifier.Success("marked done: " + name)
}

func (b *Board) snoozeNotificationCard(path string, now time.Time) tea.Cmd {
	cardPath, err := b.notificationCardPath(path)
	if err != nil {
		return b.notifier.ErrorCause("snooze due date", err)
	}
	raw, err := os.ReadFile(cardPath)
	if err != nil {
		return b.notifier.ErrorCause("snooze due date", err)
	}
	block, _, _ := frontmatter.Split(string(raw))
	parsed, err := frontmatter.Parse([]byte(block))
	if err != nil {
		return b.notifier.ErrorCause("snooze due date", err)
	}
	due, timed := notificationDue(parsed.Data["due"], now)
	due = snoozedNotificationDue(due, timed, now)
	value := due.Format(time.DateOnly)
	if timed {
		value = due.UTC().Truncate(time.Second).Format(time.RFC3339)
	}
	updated := frontmatter.Set(string(raw), "due", strconv.Quote(value))
	if err := board.ReplaceFileContent(cardPath, updated); err != nil {
		return b.notifier.ErrorCause("snooze due date", err)
	}
	if err := b.loadColumns(); err != nil {
		return b.notifier.ErrorCause("refresh after snooze", err)
	}
	b.selectNotificationCard(cardPath)
	return b.notifier.Success("snoozed until " + value)
}

func snoozedNotificationDue(due time.Time, timed bool, now time.Time) time.Time {
	if !timed {
		due = due.In(now.Location())
		if !due.After(now) {
			due = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		}
		return due.AddDate(0, 0, 1)
	}
	if due.After(now) {
		return due.AddDate(0, 0, 1)
	}
	localNow := now.In(due.Location())
	return time.Date(
		localNow.Year(), localNow.Month(), localNow.Day(),
		due.Hour(), due.Minute(), due.Second(), due.Nanosecond(), due.Location(),
	).AddDate(0, 0, 1)
}

func notificationDue(raw any, now time.Time) (time.Time, bool) {
	value := strings.TrimSpace(fmt.Sprint(raw))
	if value == "" || value == "<nil>" {
		return now, false
	}
	if due, err := time.ParseInLocation(time.DateOnly, value, now.Location()); err == nil {
		return due, false
	}
	if due, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return due, true
	}
	if due, err := natdate.Parse(value, now); err == nil {
		timed := due.Hour() != 0 || due.Minute() != 0 || due.Second() != 0
		return due, timed
	}
	return now, false
}

func (b *Board) selectNotificationCard(path string) {
	if col, item, ok := locateVisibleSearchFile(b.columns, b.selectedCol, path); ok {
		b.selectedCol = col
		b.columns[col].SelectIndex(item)
	}
}
