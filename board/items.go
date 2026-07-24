package board

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	kbrdfs "kbrd/fs"
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
	clean, err := SanitizeName(name)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(columnPath)
	if err != nil {
		return "", err
	}
	defer root.Close()

	f, err := root.Open(clean + ".md")
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
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
	return kbrdfs.WriteExistingFileAtomicDurable(path, []byte(content))
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
	return kbrdfs.WriteExistingFileAtomicDurable(path, append(content, []byte(text+"\n")...))
}

// PrependLine inserts text as the first line of the file (before any
// frontmatter — prepend is a raw-top operation by convention).
func PrependLine(path, text string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return kbrdfs.WriteExistingFileAtomicDurable(path, append([]byte(text+"\n"), content...))
}

// JournalEntry is one timestamped line to append to a card.
type JournalEntry struct {
	At   time.Time
	Text string
}

// JournalLine appends one timestamped journal entry to a card.
func JournalLine(path string, at time.Time, text string) error {
	return JournalLines(path, []JournalEntry{{At: at, Text: text}})
}

// JournalLines appends timestamped journal entries in one durable write. The
// caller supplies each timestamp so entries can use independently detected
// natural-language dates (see DetectDate).
func JournalLines(path string, entries []JournalEntry) error {
	if len(entries) == 0 {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var added strings.Builder
	if len(content) > 0 && content[len(content)-1] != '\n' {
		added.WriteByte('\n')
	}
	for _, entry := range entries {
		added.WriteString(entry.At.Format("2006-01-02 15:04:05"))
		added.WriteString(" - ")
		added.WriteString(entry.Text)
		added.WriteByte('\n')
	}
	return kbrdfs.WriteExistingFileAtomicDurable(path, append(content, []byte(added.String())...))
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
