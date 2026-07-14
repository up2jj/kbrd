package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/config"
	"kbrd/reminders"
)

type fakeRemindersSyncer struct {
	opts reminders.Options
	cfg  config.RemindersConfig
}

func (f *fakeRemindersSyncer) Sync(_ context.Context, _ string, cfg config.RemindersConfig, opts reminders.Options) (reminders.Report, error) {
	f.opts, f.cfg = opts, cfg
	return reminders.Report{DryRun: opts.DryRun, Operations: []reminders.Operation{{Kind: reminders.CreateReminder, Target: "Inbox/task.md"}}}, nil
}

func TestRemindersSyncCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.FolderConfigFile), []byte(`
[reminders]
enabled = true
list = "kbrd"
inbox_column = "Inbox"
done_columns = ["Done"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	fake := &fakeRemindersSyncer{}
	old := newRemindersSyncer
	newRemindersSyncer = func() remindersSyncer { return fake }
	t.Cleanup(func() { newRemindersSyncer = old })

	root := NewRootCmd()
	var output strings.Builder
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"reminders", "sync", "--dry-run", "--import-existing"})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !fake.opts.DryRun || fake.opts.CreateList || !fake.opts.ImportExisting || fake.cfg.List != "kbrd" {
		t.Fatalf("sync options/config: opts=%+v cfg=%+v", fake.opts, fake.cfg)
	}
	if got := output.String(); !strings.Contains(got, "CREATE REMINDER") || !strings.Contains(got, "dry run") {
		t.Fatalf("output = %q", got)
	}
}

func TestRemindersProgressWriter(t *testing.T) {
	var output strings.Builder
	progress := remindersProgressWriter(&output)
	progress(reminders.Progress{Stage: "Reading Apple Reminders"})
	progress(reminders.Progress{Stage: "Updating Apple Reminders", Current: 0, Total: 3})
	progress(reminders.Progress{Stage: "Updating Apple Reminders", Current: 3, Total: 3})

	got := output.String()
	for _, want := range []string{"Reading Apple Reminders", "0/3", "3/3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("progress output %q does not contain %q", got, want)
		}
	}
}
