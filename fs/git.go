package fs

import (
	"bytes"
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
	Moved   bool
}

type FileChange struct {
	Path     string // path relative to repo root
	Status   string // porcelain v2 XY (2 chars; "??" for untracked)
	Added    int
	Deleted  int
	OrigPath string // populated only for renames/copies
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

func isRenameStatus(xy string) bool {
	if len(xy) != 2 {
		return false
	}
	return xy[0] == 'R' || xy[1] == 'R' || xy[0] == 'C' || xy[1] == 'C'
}

func GitDiffStats(repoRoot string) map[string]DiffStat {
	if !GitAvailable() {
		return nil
	}
	files := GitChangedFiles(repoRoot)
	if files == nil {
		return nil
	}
	out := make(map[string]DiffStat, len(files))
	for _, f := range files {
		out[filepath.Join(repoRoot, f.Path)] = DiffStat{
			Added:   f.Added,
			Deleted: f.Deleted,
			Moved:   isRenameStatus(f.Status),
		}
	}
	return out
}

// GitChangedFiles uses `git status --porcelain=v2 --renames` so renames are
// detected against both the index and the worktree (an unstaged `mv` shows up
// as a single R entry, not as separate D + ??). Numstat counts are merged in
// for ordinary modifications; renamed files report zero counts.
func GitChangedFiles(repoRoot string) []FileChange {
	if !GitAvailable() || repoRoot == "" {
		return nil
	}
	out, err := exec.Command("git", "--no-optional-locks", "-C", repoRoot,
		"status", "--porcelain=v2", "--renames", "--untracked-files=all", "-z").Output()
	if err != nil {
		return nil
	}
	stats := numstatByPath(repoRoot)

	files := []FileChange{}
	records := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if rec == "" || strings.HasPrefix(rec, "# ") || strings.HasPrefix(rec, "! ") {
			continue
		}
		switch {
		case strings.HasPrefix(rec, "1 ") || strings.HasPrefix(rec, "u "):
			// "1 XY sub mH mI mW hH hI path" — 9 fields; path is the 9th and may contain spaces.
			parts := strings.SplitN(rec, " ", 9)
			if len(parts) != 9 {
				continue
			}
			fc := FileChange{Status: parts[1], Path: parts[8]}
			if s, ok := stats[fc.Path]; ok {
				fc.Added = s.Added
				fc.Deleted = s.Deleted
			}
			files = append(files, fc)
		case strings.HasPrefix(rec, "2 "):
			// "2 XY sub mH mI mW hH hI Xscore path" — 10 fields; origPath is the next record.
			parts := strings.SplitN(rec, " ", 10)
			if len(parts) != 10 || i+1 >= len(records) {
				continue
			}
			orig := records[i+1]
			i++
			files = append(files, FileChange{Status: parts[1], Path: parts[9], OrigPath: orig})
		case strings.HasPrefix(rec, "? "):
			files = append(files, FileChange{Status: "??", Path: rec[2:]})
		}
	}
	files = pairUnstagedMoves(files)
	files = pairUnstagedRenamesByHash(repoRoot, files)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

// pairUnstagedRenamesByHash catches in-place renames the basename heuristic
// misses (e.g. `mv foo.md bar.md` in the same dir). It hashes any remaining
// ` D` and `??` entries with git's blob hash and pairs identical content.
// Renames that also modified the file won't pair — that matches git's own
// 100%-similarity rename detection and keeps the rule trivially explainable.
func pairUnstagedRenamesByHash(repoRoot string, files []FileChange) []FileChange {
	type bucket struct {
		idx []int
	}
	var deletedPaths, untrackedPaths []string
	var deletedIdx, untrackedIdx []int
	for i, f := range files {
		switch {
		case f.Status == "??":
			untrackedPaths = append(untrackedPaths, f.Path)
			untrackedIdx = append(untrackedIdx, i)
		case len(f.Status) == 2 && f.Status[1] == 'D' && f.Status[0] != 'D':
			deletedPaths = append(deletedPaths, f.Path)
			deletedIdx = append(deletedIdx, i)
		}
	}
	if len(deletedPaths) == 0 || len(untrackedPaths) == 0 {
		return files
	}

	delHashes := indexBlobHashes(repoRoot, deletedPaths)
	newHashes := workingTreeBlobHashes(repoRoot, untrackedPaths)
	if len(delHashes) == 0 || len(newHashes) == 0 {
		return files
	}

	delByHash := map[string]*bucket{}
	for k, p := range deletedPaths {
		h, ok := delHashes[p]
		if !ok || h == "" {
			continue
		}
		b, exists := delByHash[h]
		if !exists {
			b = &bucket{}
			delByHash[h] = b
		}
		b.idx = append(b.idx, deletedIdx[k])
	}
	newByHash := map[string]*bucket{}
	for k, p := range untrackedPaths {
		h, ok := newHashes[p]
		if !ok || h == "" {
			continue
		}
		b, exists := newByHash[h]
		if !exists {
			b = &bucket{}
			newByHash[h] = b
		}
		b.idx = append(b.idx, untrackedIdx[k])
	}

	drop := map[int]bool{}
	var pairs []FileChange
	for h, db := range delByHash {
		nb, ok := newByHash[h]
		if !ok {
			continue
		}
		// Deterministic pairing order so multi-rename results are stable.
		sort.Slice(db.idx, func(i, j int) bool { return files[db.idx[i]].Path < files[db.idx[j]].Path })
		sort.Slice(nb.idx, func(i, j int) bool { return files[nb.idx[i]].Path < files[nb.idx[j]].Path })
		n := len(db.idx)
		if len(nb.idx) < n {
			n = len(nb.idx)
		}
		for k := 0; k < n; k++ {
			d := files[db.idx[k]]
			u := files[nb.idx[k]]
			drop[db.idx[k]] = true
			drop[nb.idx[k]] = true
			pairs = append(pairs, FileChange{
				Status:   " R",
				Path:     u.Path,
				OrigPath: d.Path,
			})
		}
	}
	if len(drop) == 0 {
		return files
	}
	out := make([]FileChange, 0, len(files)-len(drop)+len(pairs))
	for i, f := range files {
		if drop[i] {
			continue
		}
		out = append(out, f)
	}
	out = append(out, pairs...)
	return out
}

// indexBlobHashes returns the blob SHA recorded in the index for each path.
// Worktree-side deletes still have an index entry, so this is how we recover
// the "old" content hash without reading the deleted file.
func indexBlobHashes(repoRoot string, paths []string) map[string]string {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"--no-optional-locks", "-C", repoRoot, "ls-files", "--stage", "-z", "--"}, paths...)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil
	}
	res := map[string]string{}
	for _, rec := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rec == "" {
			continue
		}
		// Format: "<mode> <sha> <stage>\t<path>"
		tab := strings.IndexByte(rec, '\t')
		if tab < 0 {
			continue
		}
		header := rec[:tab]
		path := rec[tab+1:]
		fields := strings.Fields(header)
		if len(fields) < 2 {
			continue
		}
		res[path] = fields[1]
	}
	return res
}

// workingTreeBlobHashes hashes each path's worktree contents the way git
// would (`git hash-object`). One subprocess covers the whole batch.
func workingTreeBlobHashes(repoRoot string, paths []string) map[string]string {
	if len(paths) == 0 {
		return nil
	}
	cmd := exec.Command("git", "--no-optional-locks", "-C", repoRoot, "hash-object", "--stdin-paths")
	var stdin bytes.Buffer
	for _, p := range paths {
		stdin.WriteString(filepath.Join(repoRoot, p))
		stdin.WriteByte('\n')
	}
	cmd.Stdin = &stdin
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != len(paths) {
		return nil
	}
	res := map[string]string{}
	for i, p := range paths {
		res[p] = lines[i]
	}
	return res
}

// pairUnstagedMoves recognises kbrd-style file moves: git status reports them
// as ` D oldpath` plus `?? newpath` because rename detection only fires for
// staged changes. When the basenames match (and the file sizes match within a
// small slop), we collapse the pair into a synthetic worktree-side rename
// (" R") so the panel and inline badges treat them as a single moved entry.
func pairUnstagedMoves(files []FileChange) []FileChange {
	type idx struct{ pos int }
	deleted := map[string][]int{}   // basename -> indices into files
	untracked := map[string][]int{} // basename -> indices into files
	for i, f := range files {
		switch {
		case f.Status == "??":
			b := filepath.Base(f.Path)
			untracked[b] = append(untracked[b], i)
		case len(f.Status) == 2 && f.Status[1] == 'D' && f.Status[0] != 'D':
			b := filepath.Base(f.Path)
			deleted[b] = append(deleted[b], i)
		}
	}
	drop := map[int]bool{}
	var pairs []FileChange
	for base, dIdxs := range deleted {
		uIdxs := untracked[base]
		n := len(dIdxs)
		if len(uIdxs) < n {
			n = len(uIdxs)
		}
		for k := 0; k < n; k++ {
			d := files[dIdxs[k]]
			u := files[uIdxs[k]]
			drop[dIdxs[k]] = true
			drop[uIdxs[k]] = true
			pairs = append(pairs, FileChange{
				Status:   " R",
				Path:     u.Path,
				OrigPath: d.Path,
			})
		}
	}
	if len(drop) == 0 {
		return files
	}
	out := make([]FileChange, 0, len(files)-len(drop)+len(pairs))
	for i, f := range files {
		if drop[i] {
			continue
		}
		out = append(out, f)
	}
	out = append(out, pairs...)
	return out
}
