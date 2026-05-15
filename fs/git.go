package fs

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type DiffStat struct {
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

func GitDiffStats(repoRoot string) map[string]DiffStat {
	if !GitAvailable() || repoRoot == "" {
		return nil
	}
	out, err := exec.Command("git", "--no-optional-locks", "-C", repoRoot, "diff", "--numstat", "HEAD").Output()
	if err != nil {
		return nil
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
			continue
		}
		added, err1 := strconv.Atoi(parts[0])
		deleted, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			continue
		}
		abs := filepath.Join(repoRoot, parts[2])
		stats[abs] = DiffStat{Added: added, Deleted: deleted}
	}
	return stats
}
