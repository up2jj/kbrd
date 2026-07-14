package reminders

import (
	"strings"
	"testing"
)

func TestPlanIdentityAndSafety(t *testing.T) {
	baseCard := Card{Path: "/board/Inbox/task.md", Column: "Inbox", Name: "task", Title: "task", Body: "body", Due: "2020-01-01"}
	baseReminder := Reminder{RemoteID: "remote-1", Title: "task", Body: "body", Due: "2020-01-01"}

	tests := []struct {
		name           string
		cards          []Card
		reminders      []Reminder
		state          syncState
		importExisting bool
		wantKind       OperationKind
	}{
		{name: "past due new card creates once", cards: []Card{baseCard}, state: syncState{Pairs: map[string]pairState{}}, wantKind: CreateReminder},
		{name: "unmarked reminder is safe on first sync", reminders: []Reminder{baseReminder}, state: syncState{Pairs: map[string]pairState{}}, wantKind: Unmanaged},
		{name: "explicit first import creates card", reminders: []Reminder{baseReminder}, state: syncState{Pairs: map[string]pairState{}}, importExisting: true, wantKind: CreateCard},
		{name: "later unmarked reminder creates card", reminders: []Reminder{baseReminder}, state: syncState{Initialized: true, Pairs: map[string]pairState{}}, wantKind: CreateCard},
		{name: "linked missing reminder is orphan", cards: []Card{func() Card { c := baseCard; c.SyncID = "11111111-1111-4111-8111-111111111111"; return c }()}, state: syncState{Initialized: true, Pairs: map[string]pairState{}}, wantKind: Orphan},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actions, err := plan(tt.cards, tt.reminders, tt.state, tt.importExisting, false)
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			if len(actions) != 1 || actions[0].Kind != tt.wantKind {
				t.Fatalf("actions = %+v, want one %s", actions, tt.wantKind)
			}
		})
	}
}

func TestPlanUsesHashesForDirectionAndConflict(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	card := Card{Path: "/board/Inbox/task.md", Title: "task", Body: "card", SyncID: id}
	reminder := Reminder{RemoteID: "r", Title: "task", Body: "remote", SyncID: id}

	tests := []struct {
		name     string
		previous pairState
		want     OperationKind
		count    int
	}{
		{name: "card changed", previous: pairState{CardHash: hashValues("old"), ReminderHash: hashReminder(reminder)}, want: PushReminder, count: 1},
		{name: "reminder changed", previous: pairState{CardHash: hashCard(card), ReminderHash: hashValues("old")}, want: PullCard, count: 1},
		{name: "both changed", previous: pairState{CardHash: hashValues("old-card"), ReminderHash: hashValues("old-reminder")}, want: Conflict, count: 1},
		{name: "unchanged", previous: pairState{CardHash: hashCard(card), ReminderHash: hashReminder(reminder)}, count: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actions, err := plan([]Card{card}, []Reminder{reminder}, syncState{Pairs: map[string]pairState{id: tt.previous}}, false, false)
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			if len(actions) != tt.count {
				t.Fatalf("actions = %+v, want count %d", actions, tt.count)
			}
			if tt.count > 0 && actions[0].Kind != tt.want {
				t.Fatalf("kind = %s, want %s", actions[0].Kind, tt.want)
			}
		})
	}
}

func TestPlanAcceptsConvergedContentAfterInterruptedStateSave(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	card := Card{Path: "/board/Inbox/task.md", Title: "task", Body: "same", SyncID: id}
	reminder := Reminder{RemoteID: "r", Title: "task", Body: "same", SyncID: id}
	state := syncState{Pairs: map[string]pairState{id: {
		CardHash: hashValues("old-card"), ReminderHash: hashValues("old-reminder"),
	}}}
	actions, err := plan([]Card{card}, []Reminder{reminder}, state, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Fatalf("actions = %+v, want converged no-op", actions)
	}
}

func TestPlanRejectsDuplicateIDs(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	_, err := plan([]Card{{Path: "/a.md", SyncID: id}, {Path: "/b.md", SyncID: id}}, nil, syncState{Pairs: map[string]pairState{}}, false, false)
	if err == nil || !strings.Contains(err.Error(), "duplicate kbrd_reminder_id") {
		t.Fatalf("error = %v, want duplicate id", err)
	}

	_, err = plan(nil, []Reminder{{Title: "a", SyncID: id}, {Title: "b", SyncID: id}}, syncState{Pairs: map[string]pairState{}}, false, false)
	if err == nil || !strings.Contains(err.Error(), "duplicate kbrd marker") {
		t.Fatalf("error = %v, want duplicate marker", err)
	}
}

func TestPlanTreatsRemovedIdentityAsConflict(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	card := Card{Path: "/board/Inbox/task.md", Title: "task", Due: "2026-07-15", SyncID: id}
	reminder := Reminder{RemoteID: "remote-1", Title: "task", Due: "2026-07-15"}
	state := syncState{Initialized: true, Pairs: map[string]pairState{id: {
		CardPath: card.Path, RemoteID: reminder.RemoteID,
	}}}

	actions, err := plan([]Card{card}, []Reminder{reminder}, state, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Kind != Conflict {
		t.Fatalf("removed reminder marker actions = %+v", actions)
	}

	card.SyncID = ""
	reminder.SyncID = id
	actions, err = plan([]Card{card}, []Reminder{reminder}, state, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 || actions[0].Kind != Conflict || actions[1].Kind != Orphan {
		t.Fatalf("removed card identity actions = %+v", actions)
	}
}

func TestPlanDeletesOnlyPreviouslyReportedExactPair(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	reminder := Reminder{RemoteID: "remote-1", SyncID: id, Title: "task"}
	tests := []struct {
		name  string
		state syncState
		want  OperationKind
	}{
		{name: "first missing observation", state: syncState{Pairs: map[string]pairState{id: {RemoteID: "remote-1"}}}, want: Orphan},
		{name: "second matching observation", state: syncState{Pairs: map[string]pairState{id: {RemoteID: "remote-1", CardMissing: true}}}, want: DeleteReminder},
		{name: "remote id mismatch", state: syncState{Pairs: map[string]pairState{id: {RemoteID: "other", CardMissing: true}}}, want: Orphan},
		{name: "no baseline", state: syncState{Pairs: map[string]pairState{}}, want: Orphan},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actions, err := plan(nil, []Reminder{reminder}, tt.state, false, true)
			if err != nil {
				t.Fatal(err)
			}
			if len(actions) != 1 || actions[0].Kind != tt.want {
				t.Fatalf("actions = %+v, want %s", actions, tt.want)
			}
		})
	}
}

func TestMarkerRoundTrip(t *testing.T) {
	for _, id := range []string{
		"11111111-1111-4111-8111-111111111111", // legacy UUID identity
		newSyncID(),
	} {
		marked := withMarker("notes", id)
		gotID, gotBody := splitMarker(marked)
		if gotID != id || gotBody != "notes" {
			t.Fatalf("splitMarker = %q, %q for %q", gotID, gotBody, id)
		}
	}
}

func TestNewSyncIDUsesStandardRandomText(t *testing.T) {
	first, second := newSyncID(), newSyncID()
	if first == second || len(first) < 26 {
		t.Fatalf("generated ids %q and %q", first, second)
	}
	if id, _ := splitMarker(withMarker("", first)); id != first {
		t.Fatalf("generated id %q is not marker-safe", first)
	}
}
