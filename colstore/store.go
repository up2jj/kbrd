// Package colstore is a per-column persistent key/value store for kbrd scripts.
//
// Each filesystem column directory gets a hidden TOML file (FileName) that Lua
// scripts read and write via kbrd.column.store.*. It is mostly scripting state
// that governs nothing in the app and is never shown in the UI, with one
// app-owned reserved key: "collapsed" (bool) persists a column's collapse intent
// (model.Column). This package owns parsing and on-disk concurrency only — it
// knows nothing about board roots, column names, or display.
package colstore

import (
	"maps"
	"os"
	"path/filepath"
	"sync"

	toml "github.com/pelletier/go-toml/v2"
)

// FileName is the hidden per-column store file, placed inside the column dir.
// It begins with "." so board.Hidden() skips it as a column candidate, and it
// is not a .md item — so it never surfaces in the UI.
const FileName = ".kbrd.toml"

// dirLocks serializes load-mutate-save cycles per column directory: concurrent
// access to the same column is ordered while different columns proceed in
// parallel. Writes are infrequent, so a lazily-created lock map is plenty.
var dirLocks sync.Map // dir → *sync.Mutex

func lockFor(dir string) *sync.Mutex {
	m, _ := dirLocks.LoadOrStore(dir, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// Store is the in-memory view of one column's store file. It is short-lived:
// load, mutate, save. Callers should go through Read/Update rather than
// touching Store directly, so locking is handled for them.
type Store struct {
	dir    string
	values map[string]any
}

// load reads and parses dir/FileName. A missing file yields an empty store (no
// error) — absent means "no config yet". Invalid TOML is a real error so a
// script learns its file is corrupt rather than silently losing data.
func load(dir string) (*Store, error) {
	s := &Store{dir: dir, values: map[string]any{}}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := toml.Unmarshal(data, &s.values); err != nil {
		return nil, err
	}
	if s.values == nil {
		s.values = map[string]any{}
	}
	return s, nil
}

// Read returns a locked snapshot load of dir's store for Get/All callers.
func Read(dir string) (*Store, error) {
	mu := lockFor(dir)
	mu.Lock()
	defer mu.Unlock()
	return load(dir)
}

// Update loads dir's store, runs fn against it under the per-dir lock, and
// saves iff fn returns nil. This is the only write path, so a Set never
// clobbers a concurrent Set to a different key in the same column.
func Update(dir string, fn func(*Store) error) error {
	mu := lockFor(dir)
	mu.Lock()
	defer mu.Unlock()
	s, err := load(dir)
	if err != nil {
		return err
	}
	if err := fn(s); err != nil {
		return err
	}
	return s.save()
}

// Get returns the value for key and whether it was present. Values are the
// go-toml-decoded Go types: string, int64, float64, bool, []interface{}, or
// map[string]interface{} for nested tables.
func (s *Store) Get(key string) (any, bool) {
	v, ok := s.values[key]
	return v, ok
}

// All returns a copy of every key/value pair; the caller may mutate it freely.
func (s *Store) All() map[string]any {
	out := make(map[string]any, len(s.values))
	maps.Copy(out, s.values)
	return out
}

// Set assigns value to key in memory. value may be any go-toml-marshalable
// type: scalars, []interface{}, or map[string]interface{} (nested tables).
func (s *Store) Set(key string, value any) { s.values[key] = value }

// Delete removes key in memory (no error if absent).
func (s *Store) Delete(key string) { delete(s.values, key) }

// Path returns the absolute path of the backing file.
func (s *Store) Path() string { return filepath.Join(s.dir, FileName) }

// save marshals values to TOML and writes the file atomically. An empty store
// still writes a valid (empty) file so reads stay deterministic.
func (s *Store) save() error {
	data, err := toml.Marshal(s.values)
	if err != nil {
		return err
	}
	return writeAtomic(s.Path(), data)
}

// writeAtomic writes data to a temp file in the same directory, then renames it
// over path. os.Rename is atomic on the same filesystem, so a crash mid-write
// leaves the prior file intact rather than a truncated one.
func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
