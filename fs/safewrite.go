package fs

import (
	"os"
	"path/filepath"
)

// WriteExistingFileAtomicDurable overwrites an existing file by writing a
// same-directory temp file, syncing it, renaming it over the target, and
// best-effort syncing the parent directory. The target must exist.
func WriteExistingFileAtomicDurable(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return writeFileAtomicDurable(path, data, info.Mode().Perm(), true)
}

// WriteFileAtomicDurable writes path by fsyncing a unique same-directory temp
// file, renaming it into place, then best-effort syncing the parent directory.
// It may create path.
func WriteFileAtomicDurable(path string, data []byte, perm os.FileMode) error {
	return writeFileAtomicDurable(path, data, perm, false)
}

func writeFileAtomicDurable(path string, data []byte, perm os.FileMode, requireExisting bool) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if requireExisting {
		if _, err := os.Stat(path); err != nil {
			return err
		}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	syncParentDir(path)
	return nil
}

// WriteNewFileNoClobberDurable creates path with O_EXCL, syncs it, and
// best-effort syncs the parent directory. It fails with os.ErrExist when path
// already exists.
func WriteNewFileNoClobberDurable(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(path)
		}
	}()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	cleanup = false
	syncParentDir(path)
	return nil
}

func syncParentDir(path string) {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return
	}
	defer dir.Close()
	_ = dir.Sync()
}
