package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Conflict is an incoming version preserved by the sync sidecar policy.
// Paths are slash-separated and relative to the repository root.
type Conflict struct {
	IncomingPath string
	OriginalPath string
	Label        string
	Sequence     int
}

var conflictCopyRE = regexp.MustCompile(`^(.+) \(conflict ([A-Za-z0-9._-]+)(?: ([0-9]+))?\)(\.md)$`)

// ParseConflictCopy identifies the filename format emitted by
// GitMergeResolveSidecar. It deliberately accepts only safe labels so ordinary
// cards with similar names are not hidden from board discovery accidentally.
func ParseConflictCopy(name string) (original, label string, sequence int, ok bool) {
	m := conflictCopyRE.FindStringSubmatch(name)
	if len(m) == 0 {
		return "", "", 0, false
	}
	if m[3] != "" {
		parsed, err := strconv.Atoi(m[3])
		if err != nil || parsed < 2 {
			return "", "", 0, false
		}
		sequence = parsed
	}
	return m[1] + m[4], m[2], sequence, true
}

// IsConflictCopy reports whether name is a sync-generated conflict sidecar.
func IsConflictCopy(name string) bool {
	_, _, _, ok := ParseConflictCopy(name)
	return ok
}

// ListConflicts finds all sync-generated conflict sidecars below repoRoot.
// .git is skipped; symlinked directories are not followed.
func ListConflicts(repoRoot string) ([]Conflict, error) {
	if repoRoot == "" {
		return nil, nil
	}
	var conflicts []Conflict
	err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != repoRoot && entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		original, label, sequence, ok := ParseConflictCopy(entry.Name())
		if !ok {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		incoming := filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(incoming))
		if dir != "." && dir != "" {
			original = dir + "/" + original
		}
		conflicts = append(conflicts, Conflict{
			IncomingPath: incoming,
			OriginalPath: original,
			Label:        label,
			Sequence:     sequence,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list conflicts: %w", err)
	}
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].OriginalPath != conflicts[j].OriginalPath {
			return conflicts[i].OriginalPath < conflicts[j].OriginalPath
		}
		return conflicts[i].IncomingPath < conflicts[j].IncomingPath
	})
	return conflicts, nil
}

// DeleteConflict removes an incoming sidecar after the user chooses to keep
// the original version.
func DeleteConflict(repoRoot string, conflict Conflict) error {
	incoming, err := conflictPath(repoRoot, conflict.IncomingPath)
	if err != nil {
		return err
	}
	if err := os.Remove(incoming); err != nil {
		return fmt.Errorf("remove conflict copy: %w", err)
	}
	return nil
}

// ReplaceConflict writes the incoming version over the original and removes
// the sidecar. If the original was deleted locally, the incoming file creates
// it with normal card permissions.
func ReplaceConflict(repoRoot string, conflict Conflict) error {
	incoming, err := conflictPath(repoRoot, conflict.IncomingPath)
	if err != nil {
		return err
	}
	original, err := conflictPath(repoRoot, conflict.OriginalPath)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(incoming)
	if err != nil {
		return fmt.Errorf("read conflict copy: %w", err)
	}
	if _, err := os.Stat(original); err == nil {
		err = WriteExistingFileAtomicDurable(original, content)
	} else if os.IsNotExist(err) {
		err = WriteFileAtomicDurable(original, content, 0o644)
	}
	if err != nil {
		return fmt.Errorf("replace original with conflict copy: %w", err)
	}
	if err := os.Remove(incoming); err != nil {
		return fmt.Errorf("remove replaced conflict copy: %w", err)
	}
	return nil
}

// RenameConflict keeps both versions by moving the incoming sidecar to a new
// card name in the same directory. newName is a card basename, with an
// optional .md suffix, and must not contain path separators.
func RenameConflict(repoRoot string, conflict Conflict, newName string) (string, error) {
	incoming, err := conflictPath(repoRoot, conflict.IncomingPath)
	if err != nil {
		return "", err
	}
	clean := strings.TrimSpace(strings.TrimSuffix(newName, ".md"))
	if clean == "" || clean == "." || clean == ".." || strings.ContainsAny(clean, `/\\`) || strings.Contains(clean, "..") {
		return "", fmt.Errorf("invalid conflict card name %q", newName)
	}
	dir := filepath.Dir(incoming)
	target := filepath.Join(dir, clean+".md")
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("card already exists: %s", clean)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("check conflict card name: %w", err)
	}
	if err := os.Rename(incoming, target); err != nil {
		return "", fmt.Errorf("rename conflict copy: %w", err)
	}
	rel, err := filepath.Rel(repoRoot, target)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func conflictPath(repoRoot, rel string) (string, error) {
	if repoRoot == "" || rel == "" {
		return "", fmt.Errorf("conflict path is empty")
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	inside, err := filepath.Rel(root, target)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("conflict path escapes repository: %q", rel)
	}
	for current := target; ; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("conflict path uses symlink: %q", rel)
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", fmt.Errorf("inspect conflict path: %w", statErr)
		}
		if current == root {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return target, nil
}
