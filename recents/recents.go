package recents

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	MaxEntries = 10
	appDirName = "kbrd"
	fileName   = "recent.json"
)

type Entry struct {
	Path   string `json:"path"`
	Name   string `json:"name,omitempty"`
	Pinned bool   `json:"pinned,omitempty"`
}

type Store struct {
	Entries []Entry `json:"entries"`
}

func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, appDirName, fileName), nil
}

func Load() (Store, error) {
	p, err := Path()
	if err != nil {
		return Store{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Store{}, nil
		}
		return Store{}, fmt.Errorf("read %s: %w", p, err)
	}
	var s Store
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return Store{}, fmt.Errorf("parse %s: %w", p, err)
	}
	return s, nil
}

func (s *Store) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(p), err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func normalize(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func (s *Store) Touch(path, name string) {
	if path == "" {
		return
	}
	np := normalize(path)
	pinned := false
	for _, e := range s.Entries {
		if normalize(e.Path) == np {
			pinned = e.Pinned
			break
		}
	}
	next := make([]Entry, 0, len(s.Entries)+1)
	next = append(next, Entry{Path: np, Name: name, Pinned: pinned})
	for _, e := range s.Entries {
		if normalize(e.Path) == np {
			continue
		}
		next = append(next, e)
	}
	s.Entries = capUnpinned(next)
}

// capUnpinned trims unpinned entries past MaxEntries while preserving order
// and keeping all pinned entries regardless of count.
func capUnpinned(entries []Entry) []Entry {
	out := make([]Entry, 0, len(entries))
	unpinned := 0
	for _, e := range entries {
		if e.Pinned {
			out = append(out, e)
			continue
		}
		if unpinned >= MaxEntries {
			continue
		}
		out = append(out, e)
		unpinned++
	}
	return out
}

// SetPinned marks the entry for path as pinned/unpinned. If pinning a path not
// already present, inserts a new entry at the end so it persists.
func (s *Store) SetPinned(path, name string, pinned bool) {
	if path == "" {
		return
	}
	np := normalize(path)
	for i := range s.Entries {
		if normalize(s.Entries[i].Path) == np {
			s.Entries[i].Pinned = pinned
			return
		}
	}
	if pinned {
		s.Entries = append(s.Entries, Entry{Path: np, Name: name, Pinned: true})
	}
}

// Remove deletes the entry for path (pinned or not), preserving order. Returns
// whether an entry was removed.
func (s *Store) Remove(path string) bool {
	if path == "" {
		return false
	}
	np := normalize(path)
	kept := make([]Entry, 0, len(s.Entries))
	removed := false
	for _, e := range s.Entries {
		if normalize(e.Path) == np {
			removed = true
			continue
		}
		kept = append(kept, e)
	}
	s.Entries = kept
	return removed
}

func (s *Store) Prune() int {
	kept := make([]Entry, 0, len(s.Entries))
	removed := 0
	for _, e := range s.Entries {
		if e.Pinned {
			kept = append(kept, e)
			continue
		}
		info, err := os.Stat(e.Path)
		if err != nil || !info.IsDir() {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	s.Entries = kept
	return removed
}
