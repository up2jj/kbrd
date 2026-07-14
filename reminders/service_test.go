package reminders

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"kbrd/config"
)

type fakeStore struct {
	reminders  []Reminder
	applied    []RemoteOperation
	applyCalls int
}

func (f *fakeStore) Fetch(context.Context, config.RemindersConfig, bool) ([]Reminder, error) {
	return append([]Reminder(nil), f.reminders...), nil
}

func (f *fakeStore) Apply(_ context.Context, _ config.RemindersConfig, ops []RemoteOperation) ([]Reminder, error) {
	f.applyCalls++
	var changed []Reminder
	for _, op := range ops {
		f.applied = append(f.applied, op)
		switch op.Kind {
		case "create":
			f.reminders = append(f.reminders, Reminder{RemoteID: fmt.Sprintf("remote-%d", len(f.reminders)+1), Title: op.Title, Body: op.Body, Due: op.Due, Priority: op.Priority, Completed: op.Completed})
			changed = append(changed, f.reminders[len(f.reminders)-1])
		case "update":
			for i := range f.reminders {
				if f.reminders[i].RemoteID == op.RemoteID {
					f.reminders[i] = Reminder{RemoteID: op.RemoteID, Title: op.Title, Body: op.Body, Due: op.Due, Priority: op.Priority, Completed: op.Completed}
					changed = append(changed, f.reminders[i])
				}
			}
		case "delete":
			for i := range f.reminders {
				if f.reminders[i].RemoteID == op.RemoteID && strings.HasSuffix(strings.TrimSpace(f.reminders[i].Body), "[kbrd:"+op.SyncID+"]") {
					f.reminders = append(f.reminders[:i], f.reminders[i+1:]...)
					break
				}
			}
		}
	}
	return changed, nil
}

func TestSyncPastDueCardIsIdempotent(t *testing.T) {
	root := t.TempDir()
	for _, column := range []string{"Inbox", "Done"} {
		if err := os.Mkdir(filepath.Join(root, column), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cardPath := filepath.Join(root, "Inbox", "overdue.md")
	if err := os.WriteFile(cardPath, []byte("---\ndue: 2020-01-01\n---\n\ncall back\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{}
	service := &Service{Store: store, StateDir: t.TempDir(), Now: func() time.Time {
		return time.Date(2026, 7, 14, 12, 0, 0, 0, time.Local)
	}}
	cfg := config.RemindersConfig{Enabled: true, List: "kbrd", InboxColumn: "Inbox", DoneColumns: []string{"Done"}}

	first, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if first.Applied != 1 || len(store.reminders) != 1 {
		t.Fatalf("first report = %+v, reminders = %+v", first, store.reminders)
	}
	raw, err := os.ReadFile(cardPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), FrontmatterIDKey+":") || !strings.Contains(store.applied[0].Body, "[kbrd:") {
		t.Fatalf("identity not persisted: card=%q op=%+v", raw, store.applied[0])
	}

	second, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if second.Applied != 0 || len(store.reminders) != 1 {
		t.Fatalf("second report = %+v, reminders = %+v", second, store.reminders)
	}
	if store.applyCalls != 1 {
		t.Fatalf("apply calls = %d, want one batched call", store.applyCalls)
	}
}

func TestSyncTimedDueIsBidirectionalAndIdempotent(t *testing.T) {
	root := t.TempDir()
	for _, column := range []string{"Inbox", "Done"} {
		if err := os.Mkdir(filepath.Join(root, column), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cardPath := filepath.Join(root, "Inbox", "timed.md")
	if err := os.WriteFile(cardPath, []byte("---\ndue: 2026-07-15 14:30\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	location := time.FixedZone("CEST", 2*60*60)
	store := &fakeStore{}
	service := &Service{Store: store, StateDir: t.TempDir(), Now: func() time.Time {
		return time.Date(2026, 7, 14, 12, 0, 0, 0, location)
	}}
	cfg := config.RemindersConfig{Enabled: true, List: "kbrd", InboxColumn: "Inbox", DoneColumns: []string{"Done"}}

	first, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Applied != 1 || len(store.reminders) != 1 || store.reminders[0].Due != "2026-07-15T12:30:00Z" {
		t.Fatalf("first=%+v reminders=%+v", first, store.reminders)
	}
	store.reminders[0].Due = "2026-07-15T13:45:00Z"
	pull, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if pull.Applied != 1 || len(pull.Operations) != 1 || pull.Operations[0].Kind != PullCard {
		t.Fatalf("pull report = %+v", pull)
	}
	raw, err := os.ReadFile(cardPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `due: "2026-07-15T13:45:00Z"`) {
		t.Fatalf("timed due was not written to card: %s", raw)
	}
	final, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if final.Applied != 0 {
		t.Fatalf("final report = %+v", final)
	}
}

func TestSyncMaterializesRelativeTimedDueOnEnrollment(t *testing.T) {
	root := t.TempDir()
	for _, column := range []string{"Inbox", "Done"} {
		if err := os.Mkdir(filepath.Join(root, column), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cardPath := filepath.Join(root, "Inbox", "relative.md")
	if err := os.WriteFile(cardPath, []byte("---\ndue: tomorrow at 7am\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	location := time.FixedZone("CEST", 2*60*60)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, location)
	store := &fakeStore{}
	service := &Service{Store: store, StateDir: t.TempDir(), Now: func() time.Time { return now }}
	cfg := config.RemindersConfig{Enabled: true, List: "kbrd", InboxColumn: "Inbox", DoneColumns: []string{"Done"}}

	first, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Applied != 1 || store.reminders[0].Due != "2026-07-15T05:00:00Z" {
		t.Fatalf("first=%+v reminders=%+v", first, store.reminders)
	}
	raw, err := os.ReadFile(cardPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "tomorrow") || !strings.Contains(string(raw), `due: "2026-07-15T05:00:00Z"`) || !strings.Contains(string(raw), FrontmatterIDKey+":") {
		t.Fatalf("relative due and identity were not materialized atomically: %s", raw)
	}

	now = now.AddDate(0, 0, 1)
	second, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Applied != 0 || store.applyCalls != 1 {
		t.Fatalf("second=%+v applyCalls=%d", second, store.applyCalls)
	}
}

func TestSyncMaterializesRelativeDueOnExistingLinkedCard(t *testing.T) {
	root := t.TempDir()
	for _, column := range []string{"Inbox", "Done"} {
		if err := os.Mkdir(filepath.Join(root, column), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	id := newSyncID()
	cardPath := filepath.Join(root, "Inbox", "linked.md")
	raw := fmt.Sprintf("---\ndue: tomorrow\n%s: %q\n---\n", FrontmatterIDKey, id)
	if err := os.WriteFile(cardPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{reminders: []Reminder{{
		RemoteID: "remote-1", Title: "linked", Due: "2026-07-15", Body: withMarker("", id),
	}}}
	location := time.FixedZone("CEST", 2*60*60)
	service := &Service{Store: store, StateDir: t.TempDir(), Now: func() time.Time {
		return time.Date(2026, 7, 14, 12, 0, 0, 0, location)
	}}
	cfg := config.RemindersConfig{Enabled: true, List: "kbrd", InboxColumn: "Inbox", DoneColumns: []string{"Done"}}

	dryRun, err := service.Sync(t.Context(), root, cfg, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(dryRun.Operations) != 1 || dryRun.Operations[0].Kind != MaterializeDue {
		t.Fatalf("dry-run report = %+v", dryRun)
	}
	unmodified, err := os.ReadFile(cardPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unmodified), "due: tomorrow") {
		t.Fatalf("dry-run modified the card: %s", unmodified)
	}

	report, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied != 1 || len(report.Operations) != 1 || report.Operations[0].Kind != MaterializeDue || store.applyCalls != 0 {
		t.Fatalf("report=%+v applyCalls=%d", report, store.applyCalls)
	}
	materialized, err := os.ReadFile(cardPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(materialized), "tomorrow") || !strings.Contains(string(materialized), `due: "2026-07-15"`) {
		t.Fatalf("existing relative due was not materialized: %s", materialized)
	}
}

func TestSyncMaterializesRelativeDueWhenResumingCreate(t *testing.T) {
	root := t.TempDir()
	for _, column := range []string{"Inbox", "Done"} {
		if err := os.Mkdir(filepath.Join(root, column), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	id := newSyncID()
	cardPath := filepath.Join(root, "Inbox", "pending.md")
	raw := fmt.Sprintf("---\ndue: tomorrow at 7am\n%s: %q\n---\n", FrontmatterIDKey, id)
	if err := os.WriteFile(cardPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	location := time.FixedZone("CEST", 2*60*60)
	store := &fakeStore{}
	service := &Service{Store: store, StateDir: t.TempDir(), Now: func() time.Time {
		return time.Date(2026, 7, 14, 12, 0, 0, 0, location)
	}}
	statePath, err := service.statePath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveState(statePath, syncState{Pairs: map[string]pairState{id: {
		CardPath: cardPath, Pending: "create_remote",
	}}}); err != nil {
		t.Fatal(err)
	}
	cfg := config.RemindersConfig{Enabled: true, List: "kbrd", InboxColumn: "Inbox", DoneColumns: []string{"Done"}}

	report, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied != 1 || len(store.reminders) != 1 || store.reminders[0].Due != "2026-07-15T05:00:00Z" {
		t.Fatalf("report=%+v reminders=%+v", report, store.reminders)
	}
	materialized, err := os.ReadFile(cardPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(materialized), "tomorrow") || !strings.Contains(string(materialized), `due: "2026-07-15T05:00:00Z"`) {
		t.Fatalf("pending create did not materialize due: %s", materialized)
	}
}

func TestSyncBatchesMultipleRemoteOperations(t *testing.T) {
	root := t.TempDir()
	for _, column := range []string{"Inbox", "Done"} {
		if err := os.Mkdir(filepath.Join(root, column), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"one", "two"} {
		path := filepath.Join(root, "Inbox", name+".md")
		if err := os.WriteFile(path, []byte("---\ndue: 2026-07-15\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store := &fakeStore{}
	service := &Service{Store: store, StateDir: t.TempDir(), Now: time.Now}
	cfg := config.RemindersConfig{Enabled: true, List: "kbrd", InboxColumn: "Inbox", DoneColumns: []string{"Done"}}

	report, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied != 2 || store.applyCalls != 1 || len(store.applied) != 2 {
		t.Fatalf("report=%+v applyCalls=%d operations=%+v", report, store.applyCalls, store.applied)
	}
}

func TestSyncReportsProgressStages(t *testing.T) {
	root := t.TempDir()
	for _, column := range []string{"Inbox", "Done"} {
		if err := os.Mkdir(filepath.Join(root, column), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	store := &fakeStore{}
	service := &Service{Store: store, StateDir: t.TempDir(), Now: time.Now}
	cfg := config.RemindersConfig{Enabled: true, List: "kbrd", InboxColumn: "Inbox", DoneColumns: []string{"Done"}}
	var stages []string

	_, err := service.Sync(t.Context(), root, cfg, Options{Progress: func(progress Progress) {
		stages = append(stages, progress.Stage)
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Scanning cards", "Reading Apple Reminders", "Planning changes", "Saving sync state", "Sync complete"} {
		if !slices.Contains(stages, want) {
			t.Fatalf("missing stage %q in %v", want, stages)
		}
	}
}

func TestFirstSyncDoesNotImportExistingWithoutOptIn(t *testing.T) {
	root := t.TempDir()
	for _, column := range []string{"Inbox", "Done"} {
		if err := os.Mkdir(filepath.Join(root, column), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	store := &fakeStore{reminders: []Reminder{{RemoteID: "r", Title: "phone task", Body: "notes"}}}
	service := &Service{Store: store, StateDir: t.TempDir(), Now: time.Now}
	cfg := config.RemindersConfig{Enabled: true, List: "kbrd", InboxColumn: "Inbox", DoneColumns: []string{"Done"}}

	report, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if report.Applied != 0 || len(report.Operations) != 1 || report.Operations[0].Kind != Unmanaged {
		t.Fatalf("report = %+v", report)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "Inbox"))
	if len(entries) != 0 {
		t.Fatalf("unexpected imported cards: %v", entries)
	}
}

func TestDryRunCannotCreateList(t *testing.T) {
	service := &Service{Store: &fakeStore{}, StateDir: t.TempDir(), Now: time.Now}
	cfg := config.RemindersConfig{Enabled: true, List: "kbrd", InboxColumn: "Inbox", DoneColumns: []string{"Done"}}

	_, err := service.Sync(t.Context(), t.TempDir(), cfg, Options{DryRun: true, CreateList: true})
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("error = %v", err)
	}
}

func TestSyncDeletesRemoteAfterCardIsMissingTwice(t *testing.T) {
	root := t.TempDir()
	for _, column := range []string{"Inbox", "Done"} {
		if err := os.Mkdir(filepath.Join(root, column), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cardPath := filepath.Join(root, "Inbox", "delete-me.md")
	if err := os.WriteFile(cardPath, []byte("---\ndue: 2026-07-15\n---\n\nremove remotely\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{}
	service := &Service{Store: store, StateDir: t.TempDir(), Now: time.Now}
	cfg := config.RemindersConfig{
		Enabled: true, List: "kbrd", InboxColumn: "Inbox", DoneColumns: []string{"Done"},
		DeleteRemoteOnCardDelete: true,
	}

	if report, err := service.Sync(t.Context(), root, cfg, Options{}); err != nil || report.Applied != 1 {
		t.Fatalf("initial sync report=%+v err=%v", report, err)
	}
	linkedCard, err := os.ReadFile(cardPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cardPath); err != nil {
		t.Fatal(err)
	}
	second, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatalf("first missing sync: %v", err)
	}
	if second.Applied != 0 || second.Orphans != 1 || len(store.reminders) != 1 {
		t.Fatalf("first missing report=%+v reminders=%+v", second, store.reminders)
	}
	if err := os.WriteFile(cardPath, linkedCard, 0o644); err != nil {
		t.Fatal(err)
	}
	restored, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatalf("restored card sync: %v", err)
	}
	if restored.Applied != 0 || restored.Orphans != 0 || len(store.reminders) != 1 {
		t.Fatalf("restored report=%+v reminders=%+v", restored, store.reminders)
	}
	if err := os.Remove(cardPath); err != nil {
		t.Fatal(err)
	}
	again, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatalf("missing again sync: %v", err)
	}
	if again.Applied != 0 || again.Orphans != 1 || len(store.reminders) != 1 {
		t.Fatalf("missing again report=%+v reminders=%+v", again, store.reminders)
	}

	deleted, err := service.Sync(t.Context(), root, cfg, Options{})
	if err != nil {
		t.Fatalf("second missing sync: %v", err)
	}
	if deleted.Applied != 1 || len(deleted.Operations) != 1 || deleted.Operations[0].Kind != DeleteReminder || len(store.reminders) != 0 {
		t.Fatalf("delete report=%+v reminders=%+v", deleted, store.reminders)
	}
	last := store.applied[len(store.applied)-1]
	if last.Kind != "delete" || last.RemoteID == "" || last.SyncID == "" {
		t.Fatalf("delete operation = %+v", last)
	}
}
