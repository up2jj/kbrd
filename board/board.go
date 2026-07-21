// Package board holds the headless filesystem semantics shared by the TUI
// (package model) and the MCP server (package mcp): how boards are discovered
// by friendly name, how columns/folders and items map to directories and .md
// files, and how items are created.
//
// It is the single source of truth for the "skip names prefixed with . or _"
// rule and for board-name resolution. The dependency points one way only:
// model and mcp import board; board never imports either, so it stays usable
// without a running Bubble Tea program.
package board

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	kbrdfs "kbrd/fs"
)

var (
	ErrNoColumns      = errors.New("board has no folders")
	ErrFolderNotFound = errors.New("folder not found")
	ErrEmptyName      = errors.New("name cannot be empty")
	ErrBadName        = errors.New("name cannot contain path separators or '..'")
)

// Hidden reports whether a directory entry name is skipped during board and
// item discovery. Mirrors the rule in model.loadColumns / Column.LoadItems:
// names prefixed with "." (dotfiles) or "_" (e.g. _archive) are ignored.
func Hidden(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// VarContext holds the resolved pieces a custom-command template can reference.
// It is the single source of truth for the variable names shared by the TUI
// (package model) and the headless MCP server (package mcp); both build their
// template map through VarContext.Vars so the names cannot drift apart.
//
// Optional groups are omitted when empty so a template that needs filePath
// fails cleanly (missingkey=error) when no item is in context.
type VarContext struct {
	BoardPath, BoardName   string
	ColumnPath, ColumnName string // omitted when ColumnPath == ""
	FilePath, FileName     string // omitted when FilePath == ""
}

// Vars renders the canonical template variable map: boardPath/boardName always;
// columnPath/columnName when a column is in context; filePath/fileName/fileDir
// when an item is. fileDir is derived from filePath (always its directory).
func (v VarContext) Vars() map[string]string {
	m := map[string]string{
		"boardPath": v.BoardPath,
		"boardName": v.BoardName,
	}
	if v.ColumnPath != "" {
		m["columnPath"] = v.ColumnPath
		m["columnName"] = v.ColumnName
	}
	if v.FilePath != "" {
		m["filePath"] = v.FilePath
		m["fileName"] = v.FileName
		m["fileDir"] = filepath.Dir(v.FilePath)
	}
	return m
}

// Columns returns the column/folder names of a board: immediate subdirectories
// of boardPath, alphabetically sorted, skipping Hidden entries.
func Columns(boardPath string) ([]string, error) {
	entries, err := os.ReadDir(boardPath)
	if err != nil {
		return nil, err
	}
	var cols []string
	for _, e := range entries {
		if !e.IsDir() || Hidden(e.Name()) {
			continue
		}
		cols = append(cols, e.Name())
	}
	sort.Strings(cols)
	return cols, nil
}

// ResolveColumn returns the absolute path of a column within a board. When
// folder is empty, the first column (alphabetical) is used, and ErrNoColumns
// is returned if the board has none. When folder is named, it is matched
// case-insensitively; a missing folder is created when autoCreate is true,
// otherwise ErrFolderNotFound is returned (listing the existing folders).
func ResolveColumn(boardPath, folder string, autoCreate bool) (string, error) {
	cols, err := Columns(boardPath)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(folder) == "" {
		if len(cols) == 0 {
			return "", fmt.Errorf("%w: %s", ErrNoColumns, boardPath)
		}
		return filepath.Join(boardPath, cols[0]), nil
	}

	for _, c := range cols {
		if strings.EqualFold(c, folder) {
			return filepath.Join(boardPath, c), nil
		}
	}

	if autoCreate {
		name, err := SanitizeFolder(folder)
		if err != nil {
			return "", err
		}
		dir := filepath.Join(boardPath, name)
		if err := os.Mkdir(dir, 0o755); err != nil {
			return "", err
		}
		return dir, nil
	}

	if len(cols) == 0 {
		return "", fmt.Errorf("%w: %q; board has no folders", ErrFolderNotFound, folder)
	}
	return "", fmt.Errorf("%w: %q; existing folders: %s", ErrFolderNotFound, folder, strings.Join(cols, ", "))
}

// Items returns the item names (base name without the .md extension) in a
// column directory, sorted alphabetically, skipping Hidden entries.
func Items(columnPath string) ([]string, error) {
	entries, err := os.ReadDir(columnPath)
	if err != nil {
		return nil, err
	}
	var items []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || Hidden(name) || kbrdfs.IsConflictCopy(name) || !strings.HasSuffix(name, ".md") {
			continue
		}
		items = append(items, strings.TrimSuffix(name, ".md"))
	}
	sort.Strings(items)
	return items, nil
}

// SanitizeName normalizes an item name: trims surrounding space, strips a
// single trailing ".md", and rejects anything containing a path separator or
// ".." (so it cannot escape its column directory). Returns the clean name.
func SanitizeName(name string) (string, error) {
	n := strings.TrimSpace(name)
	n = strings.TrimSuffix(n, ".md")
	n = strings.TrimSpace(n)
	if n == "" {
		return "", ErrEmptyName
	}
	if n == "." || n == ".." || strings.ContainsAny(n, `/\`) || filepath.Base(n) != n {
		return "", fmt.Errorf("%w: %q", ErrBadName, name)
	}
	return n, nil
}

// SanitizeGeneratedName turns arbitrary external text into a stable, safe
// card filename. It lowercases letters, preserves Unicode letters and digits,
// and collapses every other run of characters into one dash. An optional .md
// suffix is removed before normalization.
func SanitizeGeneratedName(name string) (string, error) {
	n := strings.TrimSpace(name)
	if ext := filepath.Ext(n); strings.EqualFold(ext, ".md") {
		n = strings.TrimSuffix(n, ext)
	}

	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(n) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}

	return SanitizeName(strings.TrimSuffix(b.String(), "-"))
}

// SanitizeFolder validates a folder name with the same rules as SanitizeName
// but without stripping a .md suffix.
func SanitizeFolder(name string) (string, error) {
	n := strings.TrimSpace(name)
	if n == "" {
		return "", ErrEmptyName
	}
	if n == "." || n == ".." || strings.ContainsAny(n, `/\`) || filepath.Base(n) != n {
		return "", fmt.Errorf("%w: %q", ErrBadName, name)
	}
	return n, nil
}

// CreateItem creates <columnPath>/<name>.md and writes content to it. The name
// is sanitized first. Creation is atomic and fails with os.ErrExist if the
// file already exists (items are never silently overwritten). A trailing
// newline is appended when content is non-empty and lacks one. Returns the
// absolute path of the created file.
func CreateItem(columnPath, name, content string) (string, error) {
	clean, err := SanitizeName(name)
	if err != nil {
		return "", err
	}
	fullPath := filepath.Join(columnPath, clean+".md")
	if content != "" {
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
	}
	if err := kbrdfs.WriteNewFileNoClobberDurable(fullPath, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fullPath, nil
}
