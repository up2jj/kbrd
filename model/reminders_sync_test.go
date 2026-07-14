package model

import (
	"context"
	"runtime"
	"testing"

	tea "charm.land/bubbletea/v2"

	"kbrd/config"
	"kbrd/reminders"
)

type fakeModelRemindersSyncer struct {
	report reminders.Report
	err    error
	calls  int
}

func (f *fakeModelRemindersSyncer) Sync(context.Context, string, config.RemindersConfig, reminders.Options) (reminders.Report, error) {
	f.calls++
	return f.report, f.err
}

func TestHelpMenuIncludesRemindersSyncAction(t *testing.T) {
	fake := &fakeModelRemindersSyncer{}
	b := NewBoardWithOptions(config.Config{
		Path: t.TempDir(), Theme: "auto",
		Reminders: config.RemindersConfig{Enabled: true, List: "kbrd", InboxColumn: "Inbox", DoneColumns: []string{"Done"}},
	}, BoardOptions{Reminders: fake})

	groups := b.helpActions().groups()
	found := false
	for _, group := range groups {
		for _, entry := range group.Items {
			if entry.ActionID == helpActionRemindersSync {
				found = true
				if entry.Disabled != (runtime.GOOS != "darwin") {
					t.Fatalf("disabled=%v on %s", entry.Disabled, runtime.GOOS)
				}
			}
		}
	}
	if !found {
		t.Fatal("Reminders sync action not found")
	}
}

func TestTUIRemindersSyncLifecycle(t *testing.T) {
	fake := &fakeModelRemindersSyncer{report: reminders.Report{Applied: 1}}
	b := NewBoardWithOptions(config.Config{
		Path: t.TempDir(), Theme: "auto",
		Reminders: config.RemindersConfig{Enabled: true, List: "kbrd", InboxColumn: "Inbox", DoneColumns: []string{"Done"}},
	}, BoardOptions{Reminders: fake})

	cmd := b.startRemindersSync()
	if !b.remindersSyncing || b.remindersStatus == "" || cmd == nil {
		t.Fatal("sync did not enter running state")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("command returned %T", cmd())
	}
	msg, ok := batch[0]().(remindersSyncFinishedMsg)
	if !ok {
		t.Fatalf("run command returned %T", batch[0]())
	}
	if _, follow := b.handleRemindersSyncFinished(msg); follow == nil {
		t.Fatal("finished sync did not return notification")
	}
	if b.remindersSyncing || b.remindersStatus != "" || fake.calls != 1 {
		t.Fatalf("running=%v status=%q calls=%d", b.remindersSyncing, b.remindersStatus, fake.calls)
	}
}

func TestTUIRemindersProgressUpdatesStatus(t *testing.T) {
	b := &Board{remindersSyncing: true}
	progress := make(chan reminders.Progress)
	_, cmd := b.handleRemindersSyncProgress(remindersSyncProgressMsg{
		Progress: reminders.Progress{Stage: "Updating Apple Reminders", Current: 2, Total: 5},
		next:     progress,
	})
	if b.remindersStatus != "Updating Apple Reminders 2/5" || cmd == nil {
		t.Fatalf("status=%q cmd=%v", b.remindersStatus, cmd)
	}
	close(progress)
}
