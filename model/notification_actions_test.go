package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kbrd/config"
	"kbrd/notifyroute"
)

func TestNotificationCardActions(t *testing.T) {
	root := t.TempDir()
	todo := filepath.Join(root, "Todo")
	done := filepath.Join(root, "Done")
	if err := os.Mkdir(todo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(done, 0o755); err != nil {
		t.Fatal(err)
	}
	card := filepath.Join(todo, "task.md")
	if err := os.WriteFile(card, []byte("---\ndue: 2026-07-23\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := NewBoard(config.Config{Path: root, NotifyBackend: "none", Reminders: config.RemindersConfig{DoneColumns: []string{"Done"}}})
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}

	if cmd := b.snoozeNotificationCard(card, time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)); cmd == nil {
		t.Fatal("snooze returned no notification")
	}
	raw, err := os.ReadFile(card)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `due: "2026-07-24"`) {
		t.Fatalf("snoozed card = %s", raw)
	}

	if cmd := b.markNotificationCardDone(card); cmd == nil {
		t.Fatal("mark done returned no notification")
	}
	moved := filepath.Join(done, "task.md")
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("moved card: %v", err)
	}
	if item := b.columns[b.selectedCol].SelectedItem(); item == nil || item.FullPath != moved {
		t.Fatalf("selected item = %#v, want %s", item, moved)
	}
}

func TestNotificationActionRejectsCardOutsideBoard(t *testing.T) {
	root := t.TempDir()
	b := NewBoard(config.Config{Path: root, NotifyBackend: "none"})
	_, cmd := b.handleNotificationAction(notifyroute.Command{Action: notifyroute.OpenCard, BoardPath: root, CardPath: filepath.Join(t.TempDir(), "card.md")})
	if cmd == nil {
		t.Fatal("invalid open action returned no error notification")
	}
}

func TestSnoozedNotificationDueAdvancesPastNow(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	tests := []struct {
		name  string
		due   time.Time
		timed bool
		want  time.Time
	}{
		{
			name: "overdue all-day",
			due:  time.Date(2026, 7, 1, 0, 0, 0, 0, now.Location()),
			want: time.Date(2026, 7, 25, 0, 0, 0, 0, now.Location()),
		},
		{
			name:  "overdue timed preserves clock time",
			due:   time.Date(2026, 7, 1, 9, 30, 0, 0, now.Location()),
			timed: true,
			want:  time.Date(2026, 7, 25, 9, 30, 0, 0, now.Location()),
		},
		{
			name: "future all-day advances from due",
			due:  time.Date(2026, 7, 30, 0, 0, 0, 0, now.Location()),
			want: time.Date(2026, 7, 31, 0, 0, 0, 0, now.Location()),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := snoozedNotificationDue(tt.due, tt.timed, now); !got.Equal(tt.want) {
				t.Fatalf("snoozed due = %s, want %s", got, tt.want)
			}
		})
	}
}
