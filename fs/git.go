package fs

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type DiffStat struct {
	Added   int
	Deleted int
}

type FileChange struct {
	Path    string // path relative to repo root
	Status  string // porcelain XY (2 chars)
	Added   int
	Deleted int
}

var (
	gitAvailableOnce sync.Once
	gitAvailable     bool
)

func GitAvailable() bool {
	gitAvailableOnce.Do(func() {
		_, err := exec.LookPath("git")
		gitAvailable = err == nil
	})
	return gitAvailable
}

func GitRepoRoot(dir string) string {
	if !GitAvailable() {
		return ""
	}
	out, err := exec.Command("git", "--no-optional-locks", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func GitInit(dir string) error {
	return exec.Command("git", "-C", dir, "init").Run()
}

func GitCurrentBranch(repoRoot string) string {
	if !GitAvailable() || repoRoot == "" {
		return ""
	}
	out, err := exec.Command("git", "--no-optional-locks", "-C", repoRoot, "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// numstatByPath returns added/deleted line counts keyed by repo-relative path.
func numstatByPath(repoRoot string) map[string]DiffStat {
	if repoRoot == "" {
		return nil
	}
	out, err := exec.Command("git", "--no-optional-locks", "-C", repoRoot, "diff", "--numstat", "HEAD").Output()
	if err != nil {
		return map[string]DiffStat{}
	}
	stats := map[string]DiffStat{}
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == "-" || parts[1] == "-" {
			stats[parts[2]] = DiffStat{}
			continue
		}
		added, err1 := strconv.Atoi(parts[0])
		deleted, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			continue
		}
		stats[parts[2]] = DiffStat{Added: added, Deleted: deleted}
	}
	return stats
}

func GitDiffStats(repoRoot string) map[string]DiffStat {
	if !GitAvailable() {
		return nil
	}
	by := numstatByPath(repoRoot)
	if by == nil {
		return nil
	}
	out := make(map[string]DiffStat, len(by))
	for p, s := range by {
		out[filepath.Join(repoRoot, p)] = s
	}
	return out
}

// GitChangedFiles merges `git status --porcelain -z` (which entries exist)
// with `git diff --numstat HEAD` (line counts for tracked changes). Untracked
// files appear with Status="??" and zero counts.
func GitChangedFiles(repoRoot string) []FileChange {
	if !GitAvailable() || repoRoot == "" {
		return nil
	}
	out, err := exec.Command("git", "--no-optional-locks", "-C", repoRoot, "status", "--porcelain=v1", "--untracked-files=all", "-z").Output()
	if err != nil {
		return nil
	}
	stats := numstatByPath(repoRoot)

	files := []FileChange{}
	records := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if len(rec) < 4 {
			continue
		}
		status := rec[:2]
		path := rec[3:]
		// Renames/copies in porcelain -z: "R " / "C " is followed by a second
		// NUL-separated record with the original path. Consume and ignore it.
		if status[0] == 'R' || status[0] == 'C' {
			i++
		}
		fc := FileChange{Path: path, Status: status}
		if s, ok := stats[path]; ok {
			fc.Added = s.Added
			fc.Deleted = s.Deleted
		}
		files = append(files, fc)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}
