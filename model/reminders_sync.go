package model

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"kbrd/config"
	"kbrd/reminders"
)

const helpActionRemindersSync = "reminders_sync"

// ReminderSyncer is the shared domain service consumed by the TUI. Keeping the
// interface here lets model tests use a small fake without invoking osascript.
type ReminderSyncer interface {
	Sync(context.Context, string, config.RemindersConfig, reminders.Options) (reminders.Report, error)
}

type remindersSyncFinishedMsg struct {
	Report reminders.Report
	Err    error
}

type remindersSyncProgressMsg struct {
	Progress reminders.Progress
	next     <-chan reminders.Progress
}

type remindersSyncProgressClosedMsg struct{}

func (b *Board) startRemindersSync() tea.Cmd {
	if b.remindersSyncing {
		return b.notifier.Error("Reminders sync is already running")
	}
	if b.remindersSyncer == nil {
		return b.notifier.Error("Reminders sync is unavailable")
	}
	b.remindersSyncing = true
	b.remindersStatus = "starting reminders sync"
	service := b.remindersSyncer
	path := b.cfg.Path
	cfg := b.cfg.Reminders
	progress := make(chan reminders.Progress, 1)
	run := func() tea.Msg {
		report, err := service.Sync(context.Background(), path, cfg, reminders.Options{
			Progress: func(update reminders.Progress) { publishRemindersProgress(progress, update) },
		})
		close(progress)
		return remindersSyncFinishedMsg{Report: report, Err: err}
	}
	return tea.Batch(run, waitRemindersProgress(progress))
}

func publishRemindersProgress(ch chan reminders.Progress, update reminders.Progress) {
	select {
	case ch <- update:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- update:
	default:
	}
}

func waitRemindersProgress(ch <-chan reminders.Progress) tea.Cmd {
	return func() tea.Msg {
		progress, ok := <-ch
		if !ok {
			return remindersSyncProgressClosedMsg{}
		}
		return remindersSyncProgressMsg{Progress: progress, next: ch}
	}
}

func (b *Board) handleRemindersSyncProgress(msg remindersSyncProgressMsg) (tea.Model, tea.Cmd) {
	if !b.remindersSyncing {
		return b, nil
	}
	b.remindersStatus = msg.Progress.Stage
	if msg.Progress.Total > 0 {
		b.remindersStatus = fmt.Sprintf("%s %d/%d", msg.Progress.Stage, msg.Progress.Current, msg.Progress.Total)
	}
	return b, waitRemindersProgress(msg.next)
}

func (b *Board) handleRemindersSyncFinished(msg remindersSyncFinishedMsg) (tea.Model, tea.Cmd) {
	b.remindersSyncing = false
	b.remindersStatus = ""
	if msg.Err != nil {
		return b, b.notifier.SyncError("Reminders sync failed: "+msg.Err.Error(), "reminders")
	}
	message := "Reminders: " + msg.Report.Summary()
	if msg.Report.Conflicts > 0 || msg.Report.Orphans > 0 || msg.Report.Unmanaged > 0 {
		message = fmt.Sprintf("%s; review with kbrd reminders sync --dry-run", message)
	}
	notify := b.notifier.Success(message)
	if msg.Report.Changed {
		return b, tea.Batch(notify, b.utilityActions().refresh())
	}
	return b, notify
}
