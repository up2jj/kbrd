// Package harpoon persists the five quick-jump slots for each kbrd board.
//
// The store is machine-local, like the recent-board list: paths are specific
// to a checkout and must not make a board repository dirty.
package harpoon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	kbrdfs "kbrd/fs"
)

const SlotCount = 5

const fileName = "harpoon.json"

// Slots contains the absolute markdown file paths assigned to a board's five
// slots. An empty string means that slot is unassigned.
type Slots [SlotCount]string

type Store struct {
	Boards map[string]Slots `json:"boards"`
}

// Path returns the machine-local store path.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, "kbrd", fileName), nil
}

// Load reads the persisted slot store. A missing store has no slots yet.
func Load() (Store, error) {
	path, err := Path()
	if err != nil {
		return Store{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Store{Boards: map[string]Slots{}}, nil
		}
		return Store{}, fmt.Errorf("read %s: %w", path, err)
	}
	store := Store{Boards: map[string]Slots{}}
	if len(data) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return Store{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if store.Boards == nil {
		store.Boards = map[string]Slots{}
	}
	return store, nil
}

// ForBoard returns a board's slots. The board key is normalized so equivalent
// relative and absolute paths share one set of assignments.
func (s Store) ForBoard(boardPath string) Slots { return s.Boards[normalize(boardPath)] }

// Set assigns slot (0 through SlotCount-1) for boardPath. Pass an empty file
// path to clear a slot.
func (s *Store) Set(boardPath string, slot int, filePath string) error {
	if slot < 0 || slot >= SlotCount {
		return fmt.Errorf("slot %d out of range", slot+1)
	}
	if s.Boards == nil {
		s.Boards = map[string]Slots{}
	}
	key := normalize(boardPath)
	slots := s.Boards[key]
	slots[slot] = normalizeFile(filePath)
	s.Boards[key] = slots
	return nil
}

// Save atomically persists the complete store.
func (s Store) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return kbrdfs.WriteFileAtomicDurable(path, data, 0o644)
}

func normalize(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func normalizeFile(path string) string { return normalize(path) }
