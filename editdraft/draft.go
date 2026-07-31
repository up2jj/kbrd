// Package editdraft manages crash-recovery sidecars shared by kbrd's editors.
package editdraft

import (
	"errors"
	"os"
	"path/filepath"

	kbrdfs "kbrd/fs"
)

// Path returns the hidden recovery sidecar for documentPath.
func Path(documentPath string) string {
	dir := filepath.Dir(documentPath)
	return filepath.Join(dir, "."+filepath.Base(documentPath)+".kbrd-swap")
}

// Read reads the complete recovery sidecar for documentPath.
func Read(documentPath string) ([]byte, error) {
	return os.ReadFile(Path(documentPath))
}

// Write atomically and durably replaces the recovery sidecar for documentPath.
func Write(documentPath string, content []byte) error {
	return kbrdfs.WriteFileAtomicDurable(Path(documentPath), content, 0o644)
}

// Clear removes the recovery sidecar. A missing sidecar is already clear.
func Clear(documentPath string) error {
	err := os.Remove(Path(documentPath))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
