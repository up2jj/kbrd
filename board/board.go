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

	"github.com/sahilm/fuzzy"

	"kbrd/recents"
)

var (
	ErrBoardNotFound  = errors.New("board not found")
	ErrBoardAmbiguous = errors.New("board name is ambiguous")
	ErrNoColumns      = errors.New("board has no folders")
	ErrFolderNotFound = errors.New("folder not found")
	ErrEmptyName      = errors.New("name cannot be empty")
	ErrBadName        = errors.New("name cannot contain path separators or '..'")
)

// Ref is a board known to kbrd: a friendly name (may be empty) and its path.
type Ref struct {
	Name   string
	Path   string
	Pinned bool
}

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

// ListBoards returns the boards known from the recents store. Paths are not
// required to still exist on disk; callers may filter on existence.
func ListBoards() ([]Ref, error) {
	store, err := recents.Load()
	if err != nil {
		return nil, err
	}
	refs := make([]Ref, 0, len(store.Entries))
	for _, e := range store.Entries {
		refs = append(refs, Ref{Name: e.Name, Path: e.Path, Pinned: e.Pinned})
	}
	return refs, nil
}

// label is the string a board is matched against: its friendly name if set,
// otherwise the base name of its directory.
func (r Ref) Label() string {
	if r.Name != "" {
		return r.Name
	}
	return filepath.Base(r.Path)
}

// Resolve maps a user-supplied board name to a known board:
//
//  1. exact case-insensitive match on the friendly name (or, for boards
//     without a name, the directory base name). Exactly one match wins; more
//     than one is ErrBoardAmbiguous.
//  2. otherwise a fuzzy match over the same labels. A single result wins; more
//     than one is ErrBoardAmbiguous; none is ErrBoardNotFound.
//
// Returned errors enumerate the known board names so a caller (or an LLM) can
// correct itself.
func Resolve(name string) (Ref, error) {
	refs, err := ListBoards()
	if err != nil {
		return Ref{}, err
	}
	return resolveFrom(name, refs)
}

func resolveFrom(name string, refs []Ref) (Ref, error) {
	query := strings.TrimSpace(name)
	if len(refs) == 0 {
		return Ref{}, fmt.Errorf("%w: no boards known — open a board in kbrd first", ErrBoardNotFound)
	}
	if query == "" {
		return Ref{}, fmt.Errorf("%w: provide a board name; known boards: %s", ErrBoardNotFound, knownNames(refs))
	}

	// 1) exact case-insensitive match.
	var exact []Ref
	for _, r := range refs {
		if strings.EqualFold(r.Label(), query) {
			exact = append(exact, r)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return Ref{}, fmt.Errorf("%w: %q matches %d boards: %s", ErrBoardAmbiguous, query, len(exact), refsNames(exact))
	}

	// 2) fuzzy fallback over labels.
	labels := make([]string, len(refs))
	for i, r := range refs {
		labels[i] = r.Label()
	}
	matches := fuzzy.Find(query, labels)
	if len(matches) == 0 {
		return Ref{}, fmt.Errorf("%w: %q; known boards: %s", ErrBoardNotFound, query, knownNames(refs))
	}
	if len(matches) == 1 {
		return refs[matches[0].Index], nil
	}
	// Multiple fuzzy candidates: accept the top one only if it scores
	// decisively ahead of the runner-up; otherwise ask the caller to be
	// specific. fuzzy.Find returns results sorted best-first.
	if matches[0].Score > matches[1].Score {
		return refs[matches[0].Index], nil
	}
	cands := make([]Ref, 0, len(matches))
	for _, m := range matches {
		cands = append(cands, refs[m.Index])
	}
	return Ref{}, fmt.Errorf("%w: %q; candidates: %s", ErrBoardAmbiguous, query, refsNames(cands))
}

func knownNames(refs []Ref) string { return refsNames(refs) }

func refsNames(refs []Ref) string {
	names := make([]string, 0, len(refs))
	for _, r := range refs {
		names = append(names, r.Label())
	}
	return strings.Join(names, ", ")
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
		if e.IsDir() || Hidden(name) || !strings.HasSuffix(name, ".md") {
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
	f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if content != "" {
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if _, err := f.WriteString(content); err != nil {
			return "", err
		}
	}
	return fullPath, nil
}
