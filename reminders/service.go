package reminders

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"kbrd/config"
)

var (
	markerPattern = regexp.MustCompile(`(?m)\n?\[kbrd:([0-9a-fA-F-]{36}|[a-z2-7]{26,})\]\s*$`)
	processLocks  sync.Map
)

func NewService() *Service {
	return &Service{Store: newPlatformStore(), Now: time.Now}
}

func (s *Service) Sync(ctx context.Context, boardPath string, cfg config.RemindersConfig, opts Options) (Report, error) {
	if opts.DryRun && opts.CreateList {
		return Report{}, errors.New("--dry-run and --create-list cannot be used together")
	}
	if !cfg.Enabled {
		return Report{}, errors.New("reminders integration is disabled; set reminders.enabled = true")
	}
	if strings.TrimSpace(cfg.List) == "" {
		return Report{}, errors.New("reminders.list is required")
	}
	if strings.TrimSpace(cfg.InboxColumn) == "" {
		return Report{}, errors.New("reminders.inbox_column is required")
	}
	if len(cfg.DoneColumns) == 0 || firstConfiguredColumn(cfg.DoneColumns) == "" {
		return Report{}, errors.New("reminders.done_columns must contain at least one column")
	}
	if s.Store == nil {
		return Report{}, errors.New("reminders store is unavailable")
	}
	abs, err := filepath.Abs(boardPath)
	if err != nil {
		return Report{}, fmt.Errorf("resolve board path: %w", err)
	}
	lockValue, _ := processLocks.LoadOrStore(abs, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	if !lock.TryLock() {
		return Report{}, errors.New("reminders sync is already running for this board")
	}
	defer lock.Unlock()
	if !opts.DryRun {
		statePath, err := s.statePath(abs)
		if err != nil {
			return Report{}, err
		}
		unlock, err := acquireFileLock(statePath + ".lock")
		if err != nil {
			return Report{}, err
		}
		defer unlock()
	}

	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	emitProgress(opts, "Scanning cards", 0, 0)
	cards, err := scanCards(abs, cfg, now)
	if err != nil {
		return Report{}, err
	}
	emitProgress(opts, "Reading Apple Reminders", 0, 0)
	remote, err := s.Store.Fetch(ctx, cfg, opts.CreateList)
	if err != nil {
		return Report{}, err
	}
	for i := range remote {
		remote[i].SyncID, remote[i].Body = splitMarker(remote[i].Body)
	}
	state, statePath, err := s.loadState(abs)
	if err != nil {
		return Report{}, err
	}
	emitProgress(opts, "Planning changes", 0, 0)
	actions, err := plan(cards, remote, state, opts.ImportExisting, cfg.DeleteRemoteOnCardDelete)
	if err != nil {
		return Report{}, err
	}
	actions = addDueMaterializations(actions, cards)
	report := reportFor(actions, opts.DryRun)
	if opts.DryRun {
		emitProgress(opts, "Plan ready", 0, 0)
		return report, nil
	}

	total := applicableActions(actions)
	prepared := 0
	remoteOps := make([]RemoteOperation, 0, total)
	var deletedIDs []string
	var deletedRemoteIDs []string
	for i := range actions {
		a := &actions[i]
		switch a.Kind {
		case CreateReminder:
			id := a.SyncID
			if id == "" {
				id = newSyncID()
				state.Pairs[id] = pairState{CardPath: a.Card.Path, Pending: "create_remote"}
				if err := saveState(statePath, state); err != nil {
					return report, err
				}
				if err := setCardIdentity(a.Card, id); err != nil {
					return report, err
				}
			} else if a.Card.DueRelative {
				if err := setCardIdentity(a.Card, id); err != nil {
					return report, err
				}
			}
			desired := reminderFromCard(*a.Card)
			desired.SyncID = id
			remoteOps = append(remoteOps, remoteCreate(desired))

		case PushReminder:
			desired := reminderFromCard(*a.Card)
			desired.SyncID = a.Card.SyncID
			desired.RemoteID = a.Reminder.RemoteID
			remoteOps = append(remoteOps, remoteUpdate(desired))

		case DeleteReminder:
			remoteOps = append(remoteOps, remoteDelete(*a.Reminder))
			deletedIDs = append(deletedIDs, a.SyncID)
			deletedRemoteIDs = append(deletedRemoteIDs, a.Reminder.RemoteID)

		case PullCard:
			if err := writeRemoteToCard(a.Card, *a.Reminder, cfg); err != nil {
				return report, err
			}
			report.Applied++
			report.Changed = true

		case MaterializeDue:
			if err := setCardIdentity(a.Card, a.Card.SyncID); err != nil {
				return report, err
			}
			report.Applied++
			report.Changed = true

		case CreateCard:
			id := newSyncID()
			a.Reminder.SyncID = id
			state.Pairs[id] = pairState{RemoteID: a.Reminder.RemoteID, Pending: "import_remote"}
			if err := saveState(statePath, state); err != nil {
				return report, err
			}
			card, err := createCardFromReminder(abs, *a.Reminder, cfg)
			if err != nil {
				return report, err
			}
			desired := reminderFromCard(card)
			desired.RemoteID = a.Reminder.RemoteID
			desired.SyncID = id
			cards = append(cards, card)
			remoteOps = append(remoteOps, remoteUpdate(desired))
		}
		if isApplicable(a.Kind) {
			prepared++
			emitProgress(opts, "Preparing changes", prepared, total)
		}
	}
	if len(remoteOps) > 0 {
		emitProgress(opts, "Updating Apple Reminders", 0, len(remoteOps))
		changedRemote, err := s.Store.Apply(ctx, cfg, remoteOps)
		if err != nil {
			return report, err
		}
		for i := range changedRemote {
			changedRemote[i].SyncID, changedRemote[i].Body = splitMarker(changedRemote[i].Body)
		}
		remote = mergeRemoteChanges(remote, changedRemote, deletedRemoteIDs)
		for _, id := range deletedIDs {
			delete(state.Pairs, id)
		}
		if len(deletedIDs) > 0 {
			if err := saveState(statePath, state); err != nil {
				return report, err
			}
		}
		report.Applied += len(remoteOps)
		report.Changed = true
		emitProgress(opts, "Updating Apple Reminders", len(remoteOps), len(remoteOps))
	}
	if cfg.DeleteRemoteOnCardDelete {
		markMissingCards(&state, actions)
	}

	// Re-read local cards after applying. A remote apply returns the refreshed
	// list in the same osascript process, avoiding a second expensive launch.
	if report.Changed {
		emitProgress(opts, "Refreshing cards", 0, 0)
		cards, err = scanCards(abs, cfg, now)
		if err != nil {
			return report, err
		}
	}

	state.Initialized = true
	for _, action := range actions {
		if action.Kind == Unmanaged {
			state.Initialized = false
			break
		}
	}
	refreshState(&state, cards, remote)
	emitProgress(opts, "Saving sync state", 0, 0)
	if err := saveState(statePath, state); err != nil {
		return report, err
	}
	emitProgress(opts, "Sync complete", report.Applied, report.Applied)
	return report, nil
}

func emitProgress(opts Options, stage string, current, total int) {
	if opts.Progress != nil {
		opts.Progress(Progress{Stage: stage, Current: current, Total: total})
	}
}

func applicableActions(actions []plannedAction) int {
	total := 0
	for _, action := range actions {
		if isApplicable(action.Kind) {
			total++
		}
	}
	return total
}

func isApplicable(kind OperationKind) bool {
	switch kind {
	case CreateReminder, PushReminder, DeleteReminder, PullCard, CreateCard, MaterializeDue:
		return true
	default:
		return false
	}
}

func mergeRemoteChanges(current, changed []Reminder, deletedIDs []string) []Reminder {
	deleted := make(map[string]bool, len(deletedIDs))
	for _, id := range deletedIDs {
		deleted[id] = true
	}
	byID := make(map[string]Reminder, len(changed))
	for _, reminder := range changed {
		byID[reminder.RemoteID] = reminder
	}
	merged := make([]Reminder, 0, len(current)+len(changed))
	for _, reminder := range current {
		if deleted[reminder.RemoteID] {
			continue
		}
		if updated, ok := byID[reminder.RemoteID]; ok {
			merged = append(merged, updated)
			delete(byID, reminder.RemoteID)
			continue
		}
		merged = append(merged, reminder)
	}
	for _, reminder := range changed {
		if _, ok := byID[reminder.RemoteID]; ok {
			merged = append(merged, reminder)
			delete(byID, reminder.RemoteID)
		}
	}
	return merged
}

func reportFor(actions []plannedAction, dryRun bool) Report {
	report := Report{DryRun: dryRun}
	for _, action := range actions {
		report.Operations = append(report.Operations, action.Operation)
		switch action.Kind {
		case Conflict:
			report.Conflicts++
		case Orphan:
			report.Orphans++
		case Unmanaged:
			report.Unmanaged++
		}
	}
	return report
}

func refreshState(state *syncState, cards []Card, remote []Reminder) {
	remindersByID := make(map[string]Reminder, len(remote))
	for _, reminder := range remote {
		if reminder.SyncID != "" {
			remindersByID[reminder.SyncID] = reminder
		}
	}
	for _, card := range cards {
		if card.SyncID == "" {
			continue
		}
		reminder, ok := remindersByID[card.SyncID]
		if !ok || hashCard(card) != hashReminder(reminder) {
			continue
		}
		state.Pairs[card.SyncID] = pairState{
			CardPath: card.Path, RemoteID: reminder.RemoteID,
			CardHash: hashCard(card), ReminderHash: hashReminder(reminder),
		}
	}
}

func markMissingCards(state *syncState, actions []plannedAction) {
	for _, action := range actions {
		if action.Kind != Orphan || action.Card != nil || action.Reminder == nil {
			continue
		}
		pair, ok := state.Pairs[action.SyncID]
		if !ok || pair.RemoteID != action.Reminder.RemoteID {
			continue
		}
		pair.CardMissing = true
		state.Pairs[action.SyncID] = pair
	}
}

func reminderFromCard(card Card) Reminder {
	return Reminder{SyncID: card.SyncID, Title: card.Title, Body: card.Body, Due: card.Due,
		Priority: card.Priority, Completed: card.Completed}
}

func remoteCreate(r Reminder) RemoteOperation {
	return RemoteOperation{Kind: "create", SyncID: r.SyncID, Title: r.Title,
		Body: withMarker(r.Body, r.SyncID), Due: r.Due, Priority: r.Priority, Completed: r.Completed}
}

func remoteUpdate(r Reminder) RemoteOperation {
	return RemoteOperation{Kind: "update", RemoteID: r.RemoteID, SyncID: r.SyncID, Title: r.Title,
		Body: withMarker(r.Body, r.SyncID), Due: r.Due, Priority: r.Priority, Completed: r.Completed}
}

func remoteDelete(r Reminder) RemoteOperation {
	return RemoteOperation{Kind: "delete", RemoteID: r.RemoteID, SyncID: r.SyncID}
}

func splitMarker(body string) (string, string) {
	match := markerPattern.FindStringSubmatch(body)
	if match == nil {
		return "", strings.TrimSpace(body)
	}
	return strings.ToLower(match[1]), strings.TrimSpace(markerPattern.ReplaceAllString(body, ""))
}

func withMarker(body, id string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return "[kbrd:" + id + "]"
	}
	return body + "\n\n[kbrd:" + id + "]"
}

// removeState is kept small and local to tests and future reset plumbing.
func removeState(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
