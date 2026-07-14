// Package clipboardring stores a small, machine-local history of clipboard
// entries. It deliberately has no board or UI dependencies so the history
// never becomes part of a board repository or sync protocol.
package clipboardring

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"kbrd/config"
)

const MaxEntries = 100

type Kind string

const (
	KindText        Kind = "text"
	KindMarkdown    Kind = "markdown"
	KindChecklist   Kind = "checklist"
	KindFrontmatter Kind = "frontmatter"
	KindCodeBlock   Kind = "code"
	KindLink        Kind = "link"
)

type Source struct {
	Board   string `json:"board,omitempty"`
	Column  string `json:"column,omitempty"`
	Card    string `json:"card,omitempty"`
	Heading string `json:"heading,omitempty"`
}

type Entry struct {
	ID       string         `json:"id"`
	Time     time.Time      `json:"time"`
	Kind     Kind           `json:"kind"`
	Text     string         `json:"text"`
	Source   Source         `json:"source,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Pinned   bool           `json:"pinned,omitempty"`
}

type fileData struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

type Store struct {
	mu      sync.RWMutex
	path    string
	entries []Entry
}

// DefaultPath returns the OS-appropriate per-user config location. The file
// is intentionally outside any board directory and is never synchronized.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("clipboard ring config dir: %w", err)
	}
	return filepath.Join(dir, config.AppDirName, "clipboard.json"), nil
}

// Open loads a ring. A missing file is an empty ring.
func Open(path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read clipboard ring: %w", err)
	}
	var f fileData
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("decode clipboard ring: %w", err)
	}
	s.entries = prune(f.Entries)
	return s, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Entries() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneEntries(s.entries)
}

func (s *Store) Add(entry Entry) error {
	if strings.TrimSpace(entry.Text) == "" {
		return errors.New("clipboard entry is empty")
	}
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}
	if entry.Kind == "" {
		entry.Kind = KindText
	}
	entry.Metadata = cloneMap(entry.Metadata)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = prune(append([]Entry{entry}, s.entries...))
	return s.saveLocked()
}

func (s *Store) TogglePinned(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.entries {
		if s.entries[i].ID != id {
			continue
		}
		s.entries[i].Pinned = !s.entries[i].Pinned
		if err := s.saveLocked(); err != nil {
			return false, err
		}
		return s.entries[i].Pinned, nil
	}
	return false, os.ErrNotExist
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.entries {
		if s.entries[i].ID != id {
			continue
		}
		s.entries = append(s.entries[:i], s.entries[i+1:]...)
		return s.saveLocked()
	}
	return os.ErrNotExist
}

func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = nil
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create clipboard ring directory: %w", err)
	}
	data, err := json.MarshalIndent(fileData{Version: 1, Entries: s.entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode clipboard ring: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".clipboard-*.tmp")
	if err != nil {
		return fmt.Errorf("create clipboard ring temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("protect clipboard ring: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write clipboard ring: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close clipboard ring: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace clipboard ring: %w", err)
	}
	return nil
}

func prune(entries []Entry) []Entry {
	if len(entries) <= MaxEntries {
		return cloneEntries(entries)
	}
	out := cloneEntries(entries)
	for len(out) > MaxEntries {
		remove := -1
		for i := len(out) - 1; i >= 0; i-- {
			if !out[i].Pinned {
				remove = i
				break
			}
		}
		if remove < 0 {
			remove = len(out) - 1
		}
		out = append(out[:remove], out[remove+1:]...)
	}
	return out
}

func cloneEntries(entries []Entry) []Entry {
	out := make([]Entry, len(entries))
	for i, entry := range entries {
		out[i] = entry
		out[i].Metadata = cloneMap(entry.Metadata)
	}
	return out
}

func cloneMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func DetectKind(text string) Kind {
	trimmed := strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if strings.HasPrefix(trimmed, "---\n") && strings.Contains(trimmed[4:], "\n---") {
		return KindFrontmatter
	}
	if strings.HasPrefix(trimmed, "```") || strings.Contains(trimmed, "\n```") {
		return KindCodeBlock
	}
	lines := strings.Split(trimmed, "\n")
	checklist := len(lines) > 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) < 6 || (line[0] != '-' && line[0] != '*' && line[0] != '+') || line[1] != ' ' || line[2] != '[' || line[4] != ']' || line[5] != ' ' {
			checklist = false
			break
		}
	}
	if checklist {
		return KindChecklist
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return KindLink
	}
	if strings.Contains(trimmed, "#") || strings.Contains(trimmed, "[") || strings.Contains(trimmed, "- ") {
		return KindMarkdown
	}
	return KindText
}
