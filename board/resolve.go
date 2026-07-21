package board

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sahilm/fuzzy"

	"kbrd/recents"
)

var (
	ErrBoardNotFound  = errors.New("board not found")
	ErrBoardAmbiguous = errors.New("board name is ambiguous")
)

// Ref is a board known to kbrd: a friendly name (may be empty) and its path.
type Ref struct {
	Name   string
	Path   string
	Pinned bool
}

// AmbiguousBoardError reports the concrete boards that matched a query. It
// unwraps to ErrBoardAmbiguous so existing callers can continue to use
// errors.Is, while interactive callers can use errors.As to offer a choice.
type AmbiguousBoardError struct {
	Query      string
	Candidates []Ref
}

func (e *AmbiguousBoardError) Error() string {
	return fmt.Sprintf("%s: %q matches %d boards: %s", ErrBoardAmbiguous, e.Query, len(e.Candidates), refsNames(e.Candidates))
}

func (e *AmbiguousBoardError) Unwrap() error { return ErrBoardAmbiguous }

// ListBoards returns the boards known from the recents store. Paths are not
// required to still exist on disk; callers may filter on existence.
func ListBoards() ([]Ref, error) {
	store, err := recents.Load()
	if err != nil {
		return nil, err
	}
	refs := make([]Ref, 0, len(store.Entries))
	for _, entry := range store.Entries {
		refs = append(refs, Ref{Name: entry.Name, Path: entry.Path, Pinned: entry.Pinned})
	}
	return refs, nil
}

// Label is the string a board is matched against: its friendly name if set,
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
// Returned ambiguity errors carry the matching board references so an
// interactive caller can ask the user to choose one.
func Resolve(name string) (Ref, error) {
	refs, err := ListBoards()
	if err != nil {
		return Ref{}, err
	}
	return resolveFrom(name, refs)
}

// ResolveExisting resolves a board from the recent-board registry first, then
// falls back to interpreting input as a filesystem path. Unlike Resolve, its
// successful result always points at an existing directory.
func ResolveExisting(input string) (Ref, error) {
	query := strings.TrimSpace(input)
	ref, recentErr := Resolve(query)
	if recentErr == nil {
		abs, err := existingDir(ref.Path)
		if err == nil {
			ref.Path = abs
			return ref, nil
		}
		recentErr = fmt.Errorf("recent board %q at %s is unavailable: %w", ref.Label(), ref.Path, err)
	}

	if query != "" {
		if abs, err := existingDir(query); err == nil {
			return Ref{Path: abs}, nil
		}
	}
	if recentErr != nil {
		return Ref{}, recentErr
	}
	return Ref{}, fmt.Errorf("%w: %q is neither a known board nor an existing directory", ErrBoardNotFound, query)
}

func existingDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return abs, nil
}

func resolveFrom(name string, refs []Ref) (Ref, error) {
	query := strings.TrimSpace(name)
	if len(refs) == 0 {
		return Ref{}, fmt.Errorf("%w: no boards known — open a board in kbrd first", ErrBoardNotFound)
	}
	if query == "" {
		return Ref{}, fmt.Errorf("%w: provide a board name; known boards: %s", ErrBoardNotFound, knownNames(refs))
	}

	var exact []Ref
	for _, ref := range refs {
		if strings.EqualFold(ref.Label(), query) {
			exact = append(exact, ref)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return Ref{}, &AmbiguousBoardError{Query: query, Candidates: exact}
	}

	labels := make([]string, len(refs))
	for i, ref := range refs {
		labels[i] = ref.Label()
	}
	matches := fuzzy.Find(query, labels)
	if len(matches) == 0 {
		return Ref{}, fmt.Errorf("%w: %q; known boards: %s", ErrBoardNotFound, query, knownNames(refs))
	}
	if len(matches) == 1 {
		return refs[matches[0].Index], nil
	}
	if matches[0].Score > matches[1].Score {
		return refs[matches[0].Index], nil
	}
	candidates := make([]Ref, 0, len(matches))
	for _, match := range matches {
		candidates = append(candidates, refs[match.Index])
	}
	return Ref{}, &AmbiguousBoardError{Query: query, Candidates: candidates}
}

func knownNames(refs []Ref) string { return refsNames(refs) }

func refsNames(refs []Ref) string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Label())
	}
	return strings.Join(names, ", ")
}
