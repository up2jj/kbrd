package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const maxPluginBytes = 64 << 20

func contentDigest(root string) (string, error) {
	type fileEntry struct {
		rel  string
		path string
	}
	var files []fileEntry
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin contains symlink %q", rel)
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return fmt.Errorf("plugin contains nested .git directory %q", rel)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("plugin contains non-regular file %q", rel)
		}
		total += info.Size()
		if total > maxPluginBytes {
			return fmt.Errorf("plugin exceeds %d bytes", maxPluginBytes)
		}
		files = append(files, fileEntry{rel: filepath.ToSlash(rel), path: path})
		return nil
	})
	if err != nil {
		return "", err
	}
	slices.SortFunc(files, func(a, b fileEntry) int { return cmpString(a.rel, b.rel) })
	h := sha256.New()
	for _, file := range files {
		_, _ = io.WriteString(h, file.rel)
		_, _ = h.Write([]byte{0})
		f, err := os.Open(file.path)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(h, f)
		closeErr := f.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		_, _ = h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func digestHex(digest string) (string, error) {
	raw, ok := strings.CutPrefix(digest, "sha256:")
	if !ok || len(raw) != sha256.Size*2 {
		return "", fmt.Errorf("invalid SHA-256 digest %q", digest)
	}
	if _, err := hex.DecodeString(raw); err != nil {
		return "", fmt.Errorf("invalid SHA-256 digest %q", digest)
	}
	return raw, nil
}

func copyPluginTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(target, 0o700)
		}
		dst := filepath.Join(target, rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin contains symlink %q", rel)
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return fmt.Errorf("plugin contains nested .git directory %q", rel)
			}
			return os.MkdirAll(dst, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("plugin contains non-regular file %q", rel)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		inErr := in.Close()
		outErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inErr != nil {
			return inErr
		}
		return outErr
	})
}
