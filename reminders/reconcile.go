package reminders

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

type plannedAction struct {
	Operation
	SyncID   string
	Card     *Card
	Reminder *Reminder
}

func plan(cards []Card, reminders []Reminder, state syncState, importExisting, deleteRemote bool) ([]plannedAction, error) {
	cardByID := make(map[string]*Card)
	for i := range cards {
		id := cards[i].SyncID
		if id == "" {
			continue
		}
		if prev := cardByID[id]; prev != nil {
			return nil, fmt.Errorf("duplicate %s %q on %s and %s", FrontmatterIDKey, id, prev.Path, cards[i].Path)
		}
		cardByID[id] = &cards[i]
	}
	reminderByID := make(map[string]*Reminder)
	reminderByRemoteID := make(map[string]*Reminder)
	pendingRemote := make(map[string]string)
	knownRemote := make(map[string]string)
	knownCardPath := make(map[string]string)
	for id, pair := range state.Pairs {
		if pair.RemoteID != "" {
			knownRemote[pair.RemoteID] = id
		}
		if pair.CardPath != "" {
			knownCardPath[filepath.Clean(pair.CardPath)] = id
		}
		if pair.Pending == "import_remote" && pair.RemoteID != "" {
			pendingRemote[pair.RemoteID] = id
		}
	}
	for i := range reminders {
		if reminders[i].RemoteID != "" {
			reminderByRemoteID[reminders[i].RemoteID] = &reminders[i]
		}
		id := reminders[i].SyncID
		if id == "" {
			continue
		}
		if prev := reminderByID[id]; prev != nil {
			return nil, fmt.Errorf("duplicate kbrd marker %q on reminders %q and %q", id, prev.Title, reminders[i].Title)
		}
		reminderByID[id] = &reminders[i]
	}

	var actions []plannedAction
	for i := range cards {
		card := &cards[i]
		if card.SyncID == "" {
			if id := knownCardPath[filepath.Clean(card.Path)]; id != "" {
				actions = append(actions, plannedAction{Operation: Operation{Kind: Conflict, Target: relativeTarget(card.Path), Detail: FrontmatterIDKey + " was removed"}, SyncID: id, Card: card})
				continue
			}
			actions = append(actions, plannedAction{Operation: Operation{Kind: CreateReminder, Target: relativeTarget(card.Path)}, Card: card})
			continue
		}
		reminder := reminderByID[card.SyncID]
		previous, known := state.Pairs[card.SyncID]
		if reminder == nil {
			if known && previous.RemoteID != "" {
				if unmarked := reminderByRemoteID[previous.RemoteID]; unmarked != nil && unmarked.SyncID == "" {
					actions = append(actions, plannedAction{Operation: Operation{Kind: Conflict, Target: relativeTarget(card.Path), Detail: "kbrd marker was removed from linked reminder"}, SyncID: card.SyncID, Card: card, Reminder: unmarked})
					continue
				}
			}
			if known && previous.Pending == "create_remote" {
				actions = append(actions, plannedAction{Operation: Operation{Kind: CreateReminder, Target: relativeTarget(card.Path), Detail: "resume interrupted create"}, SyncID: card.SyncID, Card: card})
				continue
			}
			if known && previous.Pending == "import_remote" {
				for i := range reminders {
					if reminders[i].RemoteID == previous.RemoteID {
						actions = append(actions, plannedAction{Operation: Operation{Kind: PushReminder, Target: relativeTarget(card.Path), Detail: "resume interrupted import"}, SyncID: card.SyncID, Card: card, Reminder: &reminders[i]})
						reminder = &reminders[i]
						break
					}
				}
				if reminder != nil {
					continue
				}
			}
			actions = append(actions, plannedAction{Operation: Operation{Kind: Orphan, Target: relativeTarget(card.Path), Detail: "linked reminder is missing"}, SyncID: card.SyncID, Card: card})
			continue
		}
		cardHash, reminderHash := hashCard(*card), hashReminder(*reminder)
		// A prior interrupted apply may have updated both sides before the
		// baseline was saved. Equal content is already converged, not a conflict.
		if cardHash == reminderHash {
			continue
		}
		if !known {
			actions = append(actions, plannedAction{Operation: Operation{Kind: Conflict, Target: relativeTarget(card.Path), Detail: "no shared sync baseline"}, SyncID: card.SyncID, Card: card, Reminder: reminder})
			continue
		}
		cardChanged := cardHash != previous.CardHash
		reminderChanged := reminderHash != previous.ReminderHash
		switch {
		case cardChanged && reminderChanged:
			actions = append(actions, plannedAction{Operation: Operation{Kind: Conflict, Target: relativeTarget(card.Path), Detail: "card and reminder both changed"}, SyncID: card.SyncID, Card: card, Reminder: reminder})
		case cardChanged:
			actions = append(actions, plannedAction{Operation: Operation{Kind: PushReminder, Target: relativeTarget(card.Path)}, SyncID: card.SyncID, Card: card, Reminder: reminder})
		case reminderChanged:
			actions = append(actions, plannedAction{Operation: Operation{Kind: PullCard, Target: relativeTarget(card.Path)}, SyncID: card.SyncID, Card: card, Reminder: reminder})
		}
	}

	for i := range reminders {
		reminder := &reminders[i]
		if _, pending := pendingRemote[reminder.RemoteID]; pending {
			continue
		}
		if reminder.SyncID == "" && knownRemote[reminder.RemoteID] != "" {
			continue // marker loss was reported against the linked card above
		}
		if reminder.SyncID != "" {
			if cardByID[reminder.SyncID] == nil {
				previous, known := state.Pairs[reminder.SyncID]
				if deleteRemote && known && previous.CardMissing && previous.RemoteID == reminder.RemoteID {
					actions = append(actions, plannedAction{Operation: Operation{Kind: DeleteReminder, Target: reminder.Title, Detail: "linked card is still missing"}, SyncID: reminder.SyncID, Reminder: reminder})
				} else {
					actions = append(actions, plannedAction{Operation: Operation{Kind: Orphan, Target: reminder.Title, Detail: "linked card is missing"}, SyncID: reminder.SyncID, Reminder: reminder})
				}
			}
			continue
		}
		if state.Initialized || importExisting {
			actions = append(actions, plannedAction{Operation: Operation{Kind: CreateCard, Target: reminder.Title}, Reminder: reminder})
		} else {
			actions = append(actions, plannedAction{Operation: Operation{Kind: Unmanaged, Target: reminder.Title, Detail: "use --import-existing on first sync"}, Reminder: reminder})
		}
	}

	slices.SortFunc(actions, func(a, b plannedAction) int {
		if n := strings.Compare(string(a.Kind), string(b.Kind)); n != 0 {
			return n
		}
		return strings.Compare(a.Target, b.Target)
	})
	return actions, nil
}

func addDueMaterializations(actions []plannedAction, cards []Card) []plannedAction {
	blocked := make(map[string]bool)
	for _, action := range actions {
		if action.Card == nil {
			continue
		}
		switch action.Kind {
		case CreateReminder, PullCard, Conflict, Orphan:
			blocked[action.Card.Path] = true
		}
	}
	for i := range cards {
		card := &cards[i]
		if card.SyncID == "" || !card.DueRelative || blocked[card.Path] {
			continue
		}
		actions = append(actions, plannedAction{
			Operation: Operation{Kind: MaterializeDue, Target: relativeTarget(card.Path), Detail: "replace relative due with stable value"},
			SyncID:    card.SyncID, Card: card,
		})
	}
	return actions
}

func hashCard(card Card) string {
	return hashValues(card.Title, card.Body, card.Due, fmt.Sprint(card.Priority), fmt.Sprint(card.Completed))
}

func hashReminder(reminder Reminder) string {
	return hashValues(reminder.Title, reminder.Body, reminder.Due, fmt.Sprint(reminder.Priority), fmt.Sprint(reminder.Completed))
}

func hashValues(values ...string) string {
	h := sha256.New()
	for _, value := range values {
		h.Write([]byte(value))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func relativeTarget(path string) string {
	return filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path))
}
