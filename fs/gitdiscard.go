package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitDiscardFile restores one changed file to HEAD. Paths that do not exist in
// HEAD (newly-added or untracked files) are removed from both the index and the
// worktree. For renames, change.OrigPath is restored while change.Path is
// removed when appropriate.
func GitDiscardFile(repoRoot string, change FileChange) error {
	paths, err := discardPaths(repoRoot, change)
	if err != nil {
		return err
	}

	headExists := true
	if _, err := gitOutput(repoRoot, "rev-parse", "--verify", "HEAD^{commit}"); err != nil {
		if _, repoErr := gitOutput(repoRoot, "rev-parse", "--git-dir"); repoErr != nil {
			return fmt.Errorf("discard changes: %w", err)
		}
		headExists = false
	}

	tracked := make([]string, 0, len(paths))
	newPaths := make([]string, 0, len(paths))
	for _, path := range paths {
		atHead := false
		if headExists {
			out, err := gitOutput(repoRoot, "ls-tree", "-z", "--name-only", "HEAD", "--", path)
			if err != nil {
				return fmt.Errorf("inspect %q at HEAD: %w", path, err)
			}
			atHead = strings.TrimSuffix(out, "\x00") == path
		}
		if atHead {
			tracked = append(tracked, path)
		} else {
			newPaths = append(newPaths, path)
		}
	}

	if len(tracked) > 0 {
		args := append([]string{"restore", "--source=HEAD", "--staged", "--worktree", "--"}, tracked...)
		if err := gitRun(repoRoot, args...); err != nil {
			return fmt.Errorf("restore tracked changes: %w", err)
		}
	}
	if len(newPaths) == 0 {
		return nil
	}

	args := append([]string{"rm", "-f", "--cached", "--ignore-unmatch", "--"}, newPaths...)
	if err := gitRun(repoRoot, args...); err != nil {
		return fmt.Errorf("unstage new file: %w", err)
	}
	for _, path := range newPaths {
		if err := os.Remove(filepath.Join(repoRoot, filepath.FromSlash(path))); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove untracked file %q: %w", path, err)
		}
	}
	return nil
}

func discardPaths(repoRoot string, change FileChange) ([]string, error) {
	if repoRoot == "" {
		return nil, fmt.Errorf("discard changes: repository root is empty")
	}
	paths := make([]string, 0, 2)
	for _, path := range []string{change.Path, change.OrigPath} {
		if path == "" {
			continue
		}
		clean := filepath.Clean(filepath.FromSlash(path))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("discard changes: path %q is outside the repository", path)
		}
		clean = filepath.ToSlash(clean)
		if clean == "." {
			return nil, fmt.Errorf("discard changes: path is empty")
		}
		duplicate := false
		for _, existing := range paths {
			if existing == clean {
				duplicate = true
				break
			}
		}
		if !duplicate {
			paths = append(paths, clean)
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("discard changes: file path is empty")
	}
	return paths, nil
}
