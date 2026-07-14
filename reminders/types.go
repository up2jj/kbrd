// Package reminders synchronizes due-bearing board cards with one macOS
// Reminders list. It owns reconciliation and persistence; CLI and TUI callers
// only select options and render the resulting report.
package reminders

import (
	"context"
	"fmt"
	"strings"
	"time"

	"kbrd/config"
)

const FrontmatterIDKey = "kbrd_reminder_id"

type Options struct {
	DryRun         bool
	CreateList     bool
	ImportExisting bool
	Progress       func(Progress)
}

// Progress describes a sync stage. Current and Total are set for stages that
// process a known number of operations.
type Progress struct {
	Stage   string
	Current int
	Total   int
}

type OperationKind string

const (
	CreateReminder OperationKind = "CREATE REMINDER"
	DeleteReminder OperationKind = "DELETE REMINDER"
	MaterializeDue OperationKind = "MATERIALIZE DUE"
	CreateCard     OperationKind = "CREATE CARD"
	PushReminder   OperationKind = "PUSH"
	PullCard       OperationKind = "PULL"
	Conflict       OperationKind = "CONFLICT"
	Orphan         OperationKind = "ORPHAN"
	Unmanaged      OperationKind = "UNMANAGED"
)

type Operation struct {
	Kind   OperationKind
	Target string
	Detail string
}

type Report struct {
	Operations []Operation
	Applied    int
	Conflicts  int
	Orphans    int
	Unmanaged  int
	Changed    bool
	DryRun     bool
}

func (r Report) Summary() string {
	parts := []string{fmt.Sprintf("%d applied", r.Applied)}
	if r.Conflicts > 0 {
		parts = append(parts, fmt.Sprintf("%d conflicts", r.Conflicts))
	}
	if r.Orphans > 0 {
		parts = append(parts, fmt.Sprintf("%d orphans", r.Orphans))
	}
	if r.Unmanaged > 0 {
		parts = append(parts, fmt.Sprintf("%d unmanaged", r.Unmanaged))
	}
	if r.DryRun {
		parts = append(parts, "dry run")
	}
	return strings.Join(parts, ", ")
}

type Reminder struct {
	RemoteID  string `json:"id"`
	SyncID    string `json:"sync_id,omitempty"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Due       string `json:"due,omitempty"` // YYYY-MM-DD (all-day) or UTC RFC 3339 (timed)
	Priority  int    `json:"priority"`
	Completed bool   `json:"completed"`
	Modified  string `json:"modified,omitempty"`
}

type RemoteOperation struct {
	Kind      string `json:"kind"` // create | update | delete
	RemoteID  string `json:"id,omitempty"`
	SyncID    string `json:"sync_id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Due       string `json:"due,omitempty"`
	Priority  int    `json:"priority"`
	Completed bool   `json:"completed"`
}

type Store interface {
	Fetch(context.Context, config.RemindersConfig, bool) ([]Reminder, error)
	// Apply executes one mutation batch and returns the created or updated
	// reminders. Deleted reminders are omitted.
	Apply(context.Context, config.RemindersConfig, []RemoteOperation) ([]Reminder, error)
}

type Card struct {
	Path        string
	Column      string
	Name        string
	SyncID      string
	Title       string
	Body        string
	Due         string
	DueRelative bool
	Priority    int
	Completed   bool
	Raw         string
}

type pairState struct {
	CardPath     string `json:"card_path"`
	RemoteID     string `json:"remote_id"`
	CardHash     string `json:"card_hash"`
	ReminderHash string `json:"reminder_hash"`
	Pending      string `json:"pending,omitempty"`
	CardMissing  bool   `json:"card_missing,omitempty"`
}

type syncState struct {
	Initialized bool                 `json:"initialized"`
	Pairs       map[string]pairState `json:"pairs"`
}

type Service struct {
	Store    Store
	StateDir string
	Now      func() time.Time
}
