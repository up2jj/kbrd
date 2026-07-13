// Package history turns Git's file history into a card-centric timeline.
package history

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kbrd/frontmatter"
	kbrdfs "kbrd/fs"
)

type EventType string

const (
	EventCreated  EventType = "created"
	EventEdited   EventType = "edited"
	EventMoved    EventType = "moved"
	EventRenamed  EventType = "renamed"
	EventDeleted  EventType = "deleted"
	EventMetadata EventType = "metadata"
)

type Event struct {
	Time         time.Time
	Type         EventType
	Author       string
	Revision     string
	Short        string
	Subject      string
	Summary      string
	Path         string
	PreviousPath string
}

type GitProvider struct{ RepoRoot string }

// History returns the selected card's semantic events, newest first. Git's
// --follow keeps the lookup attached to the card across file and column moves.
func (p GitProvider) History(cardPath string) ([]Event, error) {
	rel, err := p.relativePath(cardPath)
	if err != nil {
		return nil, err
	}
	if _, err := kbrdfs.GitOutput(p.RepoRoot, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return nil, nil
	}
	out, err := kbrdfs.GitOutput(p.RepoRoot, "log", "--follow", "--find-renames", "--name-status", "--format=%x1e%H%x1f%h%x1f%an%x1f%at%x1f%s", "--", rel)
	if err != nil {
		return nil, err
	}
	var events []Event
	for record := range strings.SplitSeq(out, "\x1e") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		lines := strings.Split(record, "\n")
		head := strings.Split(lines[0], "\x1f")
		if len(head) != 5 {
			continue
		}
		sec, err := strconv.ParseInt(head[3], 10, 64)
		if err != nil {
			continue
		}
		e := Event{Revision: head[0], Short: head[1], Author: head[2], Time: time.Unix(sec, 0), Subject: head[4]}
		for _, line := range lines[1:] {
			fields := strings.Split(line, "\t")
			if len(fields) < 2 {
				continue
			}
			status := fields[0]
			switch status[0] {
			case 'A':
				e.Type, e.Path = EventCreated, fields[1]
			case 'D':
				e.Type, e.Path = EventDeleted, fields[1]
			case 'R':
				if len(fields) < 3 {
					continue
				}
				e.PreviousPath, e.Path = fields[1], fields[2]
				if filepath.Dir(e.PreviousPath) != filepath.Dir(e.Path) {
					e.Type = EventMoved
				} else {
					e.Type = EventRenamed
				}
			case 'M':
				e.Type, e.Path = p.classifyEdit(e.Revision, fields[1]), fields[1]
			}
			if e.Type != "" {
				break
			}
		}
		if e.Type == "" {
			continue
		}
		e.Summary = eventSummary(e)
		events = append(events, e)
	}
	return events, nil
}

func (p GitProvider) Snapshot(event Event) ([]byte, error) {
	revision := event.Revision
	if event.Type == EventDeleted {
		revision += "^"
	}
	out, err := kbrdfs.GitOutput(p.RepoRoot, "show", revision+":"+event.Path)
	return []byte(out), err
}

func (p GitProvider) Diff(event Event) (string, error) {
	args := []string{"show", "--format=", "--no-ext-diff", "--no-color", "--find-renames", event.Revision, "--"}
	if event.PreviousPath != "" {
		args = append(args, event.PreviousPath)
	}
	if event.Path != event.PreviousPath {
		args = append(args, event.Path)
	}
	out, err := kbrdfs.GitOutput(p.RepoRoot, args...)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		return "(no textual differences)", nil
	}
	return strings.TrimRight(out, "\n"), nil
}

func (p GitProvider) classifyEdit(revision, path string) EventType {
	current, err := kbrdfs.GitOutput(p.RepoRoot, "show", revision+":"+path)
	if err != nil {
		return EventEdited
	}
	previous, err := kbrdfs.GitOutput(p.RepoRoot, "show", revision+"^:"+path)
	if err != nil {
		return EventEdited
	}
	_, currentBody, currentFM := frontmatter.Split(current)
	_, previousBody, previousFM := frontmatter.Split(previous)
	if (currentFM || previousFM) && currentBody == previousBody && current != previous {
		return EventMetadata
	}
	return EventEdited
}

func (p GitProvider) relativePath(path string) (string, error) {
	if p.RepoRoot == "" || path == "" {
		return "", fmt.Errorf("card history path is incomplete")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(p.RepoRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("card is outside the Git repository")
	}
	return filepath.ToSlash(rel), nil
}

func eventSummary(e Event) string {
	switch e.Type {
	case EventCreated:
		return "Created"
	case EventDeleted:
		return "Deleted"
	case EventMoved:
		return fmt.Sprintf("Moved: %s → %s", columnName(e.PreviousPath), columnName(e.Path))
	case EventRenamed:
		return fmt.Sprintf("Renamed: %s → %s", cardName(e.PreviousPath), cardName(e.Path))
	case EventMetadata:
		return "Metadata changed"
	default:
		return "Edited"
	}
}

func columnName(path string) string { return filepath.Base(filepath.Dir(filepath.FromSlash(path))) }
func cardName(path string) string {
	base := filepath.Base(filepath.FromSlash(path))
	return strings.TrimSuffix(base, filepath.Ext(base))
}
