// Package extension installs the browser extension bundled with kbrd.
package extension

import (
	"embed"
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"

	kbrdfs "kbrd/fs"
)

const (
	assetRoot  = "chrome"
	markerFile = "kbrd-extension.json"
)

//go:embed all:chrome
var assets embed.FS

// DefaultDir returns the stable directory used for Chrome's "Load unpacked"
// installation. Keeping the path stable also keeps Chrome's derived extension
// ID stable across kbrd upgrades. It deliberately lives directly under the
// home directory so macOS Finder's folder chooser does not hide it.
func DefaultDir() (string, error) {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	return filepath.Join(dir, "kbrd-chrome-extension"), nil
}

// Install extracts the bundled extension into dir and stamps its manifest with
// appVersion. It updates directories previously created by kbrd, but refuses
// to take over an unrelated directory.
func Install(dir, appVersion string) ([]string, error) {
	dir = filepath.Clean(dir)
	if err := verifyDestination(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create extension directory %s: %w", dir, err)
	}

	var written []string
	err := iofs.WalkDir(assets, assetRoot, func(path string, entry iofs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel := strings.TrimPrefix(path, assetRoot)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return nil
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if entry.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create extension directory %s: %w", target, err)
			}
			return nil
		}
		data, err := assets.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded extension asset %s: %w", path, err)
		}
		if rel == "manifest.json" {
			data, err = versionedManifest(data, appVersion)
			if err != nil {
				return err
			}
		}
		if err := kbrdfs.WriteFileAtomicDurable(target, data, 0o644); err != nil {
			return fmt.Errorf("write extension asset %s: %w", target, err)
		}
		written = append(written, target)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return written, nil
}

func verifyDestination(dir string) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect extension directory %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink extension directory: %s", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("extension destination is not a directory: %s", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, markerFile)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				return fmt.Errorf("inspect extension directory %s: %w", dir, readErr)
			}
			if len(entries) == 0 {
				return nil
			}
			return fmt.Errorf("refusing to overwrite unmanaged directory: %s", dir)
		}
		return fmt.Errorf("inspect extension marker: %w", err)
	}
	return nil
}
