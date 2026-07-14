package reminders

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	kbrdfs "kbrd/fs"
)

func (s *Service) statePath(boardPath string) (string, error) {
	dir := s.StateDir
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("locate user config directory: %w", err)
		}
		dir = filepath.Join(base, "kbrd", "reminders")
	}
	abs, err := filepath.Abs(boardPath)
	if err != nil {
		return "", fmt.Errorf("resolve board path: %w", err)
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(dir, hex.EncodeToString(sum[:12])+".json"), nil
}

func (s *Service) loadState(boardPath string) (syncState, string, error) {
	path, err := s.statePath(boardPath)
	if err != nil {
		return syncState{}, "", err
	}
	state := syncState{Pairs: make(map[string]pairState)}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, path, nil
		}
		return syncState{}, path, fmt.Errorf("read reminders state: %w", err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return syncState{}, path, fmt.Errorf("decode reminders state: %w", err)
	}
	if state.Pairs == nil {
		state.Pairs = make(map[string]pairState)
	}
	return state, path, nil
}

func saveState(path string, state syncState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create reminders state directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode reminders state: %w", err)
	}
	data = append(data, '\n')
	if err := kbrdfs.WriteFileAtomicDurable(path, data, 0o600); err != nil {
		return fmt.Errorf("write reminders state: %w", err)
	}
	return nil
}
