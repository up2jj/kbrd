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
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
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
	next := make([]Entry, 0, len(s.Entries)+1)
	next = append(next, Entry{Path: np, Name: name})
	for _, e := range s.Entries {
		if normalize(e.Path) == np {
			continue
		}
		next = append(next, e)
	}
	if len(next) > MaxEntries {
		next = next[:MaxEntries]
	}
	s.Entries = next
}

func (s *Store) Prune() int {
	kept := make([]Entry, 0, len(s.Entries))
	removed := 0
	for _, e := range s.Entries {
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
