// Package scratchpad persists one machine-local Markdown note per board.
// Notes deliberately live outside board checkouts so they cannot enter Git
// until the user explicitly promotes text into a card.
package scratchpad

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"kbrd/config"
	kbrdfs "kbrd/fs"
)

// Store owns the directory containing board-scoped scratchpad files.
type Store struct {
	dir string
}

// Open returns a store rooted at dir. An empty dir selects the OS-appropriate
// machine-local kbrd configuration directory.
func Open(dir string) (*Store, error) {
	if dir == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("scratchpad config dir: %w", err)
		}
		dir = filepath.Join(configDir, config.AppDirName, "scratchpads")
	}
	return &Store{dir: dir}, nil
}

// Path returns the machine-local file used for boardPath. The normalized
// absolute board path is hashed so arbitrary directory names never become
// path components inside the store.
func (s *Store) Path(boardPath string) (string, error) {
	if s == nil || s.dir == "" {
		return "", errors.New("scratchpad store is not initialized")
	}
	key, err := boardKey(boardPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.dir, key+".md"), nil
}

// Load reads a board's note. A missing note is empty.
func (s *Store) Load(boardPath string) (string, error) {
	path, err := s.Path(boardPath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read scratchpad: %w", err)
	}
	return string(data), nil
}

// Save atomically writes a board's note. Scratchpads may contain meeting notes
// and other sensitive text, so the store is private to the current user.
func (s *Store) Save(boardPath, text string) error {
	path, err := s.Path(boardPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create scratchpad directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("secure scratchpad directory: %w", err)
	}
	if err := kbrdfs.WriteFileAtomicDurable(path, []byte(text), 0o600); err != nil {
		return fmt.Errorf("save scratchpad: %w", err)
	}
	return nil
}

// Append adds text after a blank-line separator and returns the resulting note.
func (s *Store) Append(boardPath, text string) (string, error) {
	current, err := s.Load(boardPath)
	if err != nil {
		return "", err
	}
	next := text
	if current != "" && text != "" {
		next = current + "\n\n" + text
	} else if current != "" {
		next = current
	}
	if err := s.Save(boardPath, next); err != nil {
		return "", err
	}
	return next, nil
}

func boardKey(boardPath string) (string, error) {
	abs, err := filepath.Abs(boardPath)
	if err != nil {
		return "", fmt.Errorf("resolve board path: %w", err)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	return fmt.Sprintf("%x", sum), nil
}
