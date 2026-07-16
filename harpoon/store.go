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
	"reflect"

	kbrdfs "kbrd/fs"
)

const SlotCount = 5

const fileName = "harpoon.json"

// Slots contains the absolute markdown file paths assigned to a board's five
// slots. An empty string means that slot is unassigned.
type Slots [SlotCount]string

type Store struct {
	Boards     map[string]Slots `json:"boards"`
	Identities map[string]Slots `json:"identities,omitempty"`
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
			return newStore(), nil
		}
		return Store{}, fmt.Errorf("read %s: %w", path, err)
	}
	store := newStore()
	if len(data) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return Store{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if store.Boards == nil {
		store.Boards = map[string]Slots{}
	}
	if store.Identities == nil {
		store.Identities = map[string]Slots{}
	}
	return store, nil
}

func newStore() Store {
	return Store{
		Boards:     map[string]Slots{},
		Identities: map[string]Slots{},
	}
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
	if s.Identities == nil {
		s.Identities = map[string]Slots{}
	}
	key := normalize(boardPath)
	slots := s.Boards[key]
	path := normalizeFile(filePath)
	identity, _ := identityForPath(path)
	slots[slot] = path
	s.Boards[key] = slots
	identities := s.Identities[key]
	identities[slot] = identity
	s.Identities[key] = identities
	return nil
}

// Reconcile updates slots whose persisted filesystem identity now appears at a
// different candidate path. Existing paths also refresh their identity, which
// handles editors that replace a file atomically while keeping its name.
func (s *Store) Reconcile(boardPath string, candidates []string) bool {
	if s.Boards == nil {
		return false
	}
	if s.Identities == nil {
		s.Identities = map[string]Slots{}
	}
	key := normalize(boardPath)
	slots := s.Boards[key]
	identities := s.Identities[key]
	hasSlots := false
	for _, path := range slots {
		if path != "" {
			hasSlots = true
			break
		}
	}
	if !hasSlots {
		return false
	}
	current := make(map[string]string, len(candidates))
	byIdentity := make(map[string][]string, len(candidates))
	for _, path := range candidates {
		path = normalizeFile(path)
		identity, err := identityForPath(path)
		if err != nil || identity == "" {
			continue
		}
		current[path] = identity
		byIdentity[identity] = append(byIdentity[identity], path)
	}

	changed := false
	for i, path := range slots {
		if path == "" {
			if identities[i] != "" {
				identities[i] = ""
				changed = true
			}
			continue
		}
		if identity, ok := current[path]; ok {
			if identities[i] != identity {
				identities[i] = identity
				changed = true
			}
			continue
		}
		if identities[i] == "" {
			continue
		}
		matches := byIdentity[identities[i]]
		if len(matches) == 1 {
			slots[i] = matches[0]
			changed = true
		}
	}
	if changed {
		s.Boards[key] = slots
		s.Identities[key] = identities
	}
	return changed
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

func identityForPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return "", nil
	}
	device := uintField(value.FieldByName("Dev"))
	file := uintField(value.FieldByName("Ino"))
	if file == 0 {
		return "", nil
	}
	generation := uintField(value.FieldByName("Gen"))
	return fmt.Sprintf("%x:%x:%x", device, file, generation), nil
}

func uintField(value reflect.Value) uint64 {
	if !value.IsValid() {
		return 0
	}
	switch value.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return uint64(value.Int())
	default:
		return 0
	}
}
