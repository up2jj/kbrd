package board

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kbrd/natdate"
)

// ItemPath sanitizes name and returns the absolute path of the item file
// <columnPath>/<name>.md. It does not check whether the file exists.
func ItemPath(columnPath, name string) (string, error) {
	clean, err := SanitizeName(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(columnPath, clean+".md"), nil
}

// ReadItem returns the raw content of an item file. The error wraps
// os.ErrNotExist when the item does not exist.
func ReadItem(columnPath, name string) (string, error) {
	path, err := ItemPath(columnPath, name)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteItem overwrites the full raw content of an EXISTING item, normalizing
// the trailing newline like CreateItem. It fails with os.ErrNotExist when the
// item is missing — creating items is CreateItem's job, so an edit can never
// silently create a file.
func WriteItem(columnPath, name, content string) error {
	path, err := ItemPath(columnPath, name)
	if err != nil {
		return err
	}
	return ReplaceFileContent(path, content)
}

// DeleteItem removes an item file. The error wraps os.ErrNotExist when the
// item does not exist.
func DeleteItem(columnPath, name string) error {
	path, err := ItemPath(columnPath, name)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// The path-level operations below carry the content semantics shared by every
// frontend. The TUI (model.Column) resolves an item's path through its
// in-memory list — which, for virtual columns, may point outside the column
// directory — and delegates here; the web server reaches them through the
// (columnPath, name) wrappers above.

// ReplaceFileContent overwrites an EXISTING file, ensuring a trailing newline.
// Fails with os.ErrNotExist when the file is missing so a stale editor can
// never resurrect a deleted item.
func ReplaceFileContent(path, content string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("replace content: %w", err)
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return writeExistingFileAtomicDurable(path, []byte(content))
}

// AppendLine appends text as a new line, inserting a separating newline when
// the existing content does not end with one.
func AppendLine(path, text string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		text = "\n" + text
	}
	return writeExistingFileAtomicDurable(path, append(content, []byte(text+"\n")...))
}

// PrependLine inserts text as the first line of the file (before any
// frontmatter — prepend is a raw-top operation by convention).
func PrependLine(path, text string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return writeExistingFileAtomicDurable(path, append([]byte(text+"\n"), content...))
}

// writeExistingFileAtomicDurable overwrites an existing file by writing a
// same-dir temp file and renaming it over the target. The target must exist so
// stale editors cannot silently recreate deleted files.
func writeExistingFileAtomicDurable(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return writeFileAtomicDurable(path, data, info.Mode().Perm(), true)
}

// WriteFileAtomicDurable writes path by fsyncing a unique same-directory temp
// file, renaming it into place, then best-effort syncing the parent directory.
// It may create path; callers that require existing-only semantics should stat
// first via writeExistingFileAtomicDurable.
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
		// Re-check immediately before rename so a stale editor save fails instead
		// of recreating a file deleted while the temp content was being prepared.
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

func writeNewFileNoClobberDurable(path string, data []byte, perm os.FileMode) error {
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

// JournalLine appends text prefixed with an "at" timestamp formatted as
// "2006-01-02 15:04:05" — the journal entry format shared by all frontends. The
// caller supplies the time so the stamp can reflect a date detected in the entry
// (see DetectDate), not just time.Now().
func JournalLine(path string, at time.Time, text string) error {
	return AppendLine(path, at.Format("2006-01-02 15:04:05")+" - "+text)
}

// DetectDate splits a leading natural-language date phrase off text, returning the
// resolved timestamp and the remaining entry body. It scans successively longer
// leading token-prefixes (natdate.Parse is fail-closed, so it rejects any phrase
// with a non-date token) and keeps the longest that parses. With no leading date —
// or when stripping it would leave an empty body — it returns now and text
// unchanged.
func DetectDate(text string, now time.Time) (time.Time, string) {
	fields := strings.Fields(text)
	bestN := 0
	var bestT time.Time
	for n := 1; n <= len(fields); n++ {
		if t, err := natdate.Parse(strings.Join(fields[:n], " "), now); err == nil {
			bestN, bestT = n, t
		}
	}
	if bestN == 0 {
		return now, text
	}
	remainder := strings.TrimSpace(strings.Join(fields[bestN:], " "))
	if remainder == "" {
		return now, text
	}
	return bestT, remainder
}

// RenameNoClobber renames a file or directory, failing with os.ErrExist when
// the destination already exists — os.Rename alone would silently overwrite a
// destination file. (Best effort: the check-then-rename is not atomic.)
func RenameNoClobber(oldPath, newPath string) error {
	if _, err := os.Stat(newPath); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(oldPath, newPath)
}

// MoveItem moves an item between column directories, keeping its name.
// os.ErrNotExist when the source is missing, os.ErrExist when the destination
// already has an item of that name.
func MoveItem(srcColumnPath, destColumnPath, name string) error {
	src, err := ItemPath(srcColumnPath, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("move item: %w", err)
	}
	dest, err := ItemPath(destColumnPath, name)
	if err != nil {
		return err
	}
	return RenameNoClobber(src, dest)
}

// RenameItem renames an item within its column. os.ErrNotExist when the
// source is missing, os.ErrExist when the new name is taken.
func RenameItem(columnPath, oldName, newName string) error {
	src, err := ItemPath(columnPath, oldName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("rename item: %w", err)
	}
	dest, err := ItemPath(columnPath, newName)
	if err != nil {
		return err
	}
	return RenameNoClobber(src, dest)
}
