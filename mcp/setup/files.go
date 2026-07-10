package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	kbrdfs "kbrd/fs"
)

func readOptional(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("read %s: %w", path, err)
}

func writeFile(path string, data []byte, exists bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if exists {
		return kbrdfs.WriteExistingFileAtomicDurable(path, data)
	}
	return kbrdfs.WriteFileAtomicDurable(path, data, 0o644)
}
