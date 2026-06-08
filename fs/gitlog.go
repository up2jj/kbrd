package fs

import (
	"strconv"
	"strings"
	"time"
)

// Commit is one entry of a repo's history as read by GitLog.
type Commit struct {
	Hash    string // full sha
	Short   string // abbreviated sha (%h)
	Author  string
	Time    time.Time
	Subject string
}

// gitHasHead reports whether HEAD resolves to a commit — false for a freshly
// initialized repo with no commits yet.
func gitHasHead(repoRoot string) bool {
	_, err := gitOutput(repoRoot, "rev-parse", "--verify", "--quiet", "HEAD")
	return err == nil
}

// GitLog returns up to limit commits, newest first. An empty repo (no HEAD)
// yields (nil, nil) so callers render an empty state rather than an error.
func GitLog(repoRoot string, limit int) ([]Commit, error) {
	if !gitHasHead(repoRoot) {
		return nil, nil
	}
	// Unit separator between fields, record separator between commits — both
	// are impossible in subjects/names, unlike newlines.
	out, err := gitOutput(repoRoot,
		"log", "-n", strconv.Itoa(limit), "--format=%H%x1f%h%x1f%an%x1f%at%x1f%s%x1e")
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for rec := range strings.SplitSeq(out, "\x1e") {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, "\x1f")
		if len(fields) != 5 {
			continue
		}
		sec, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			continue
		}
		commits = append(commits, Commit{
			Hash:    fields[0],
			Short:   fields[1],
			Author:  fields[2],
			Time:    time.Unix(sec, 0),
			Subject: fields[4],
		})
	}
	return commits, nil
}

// GitHeadChangedFiles returns the repo-root-relative paths (forward slashes,
// as git emits them) touched by the HEAD commit. A root commit lists all its
// files; an empty repo yields (nil, nil).
func GitHeadChangedFiles(repoRoot string) ([]string, error) {
	if !gitHasHead(repoRoot) {
		return nil, nil
	}
	out, err := gitOutput(repoRoot,
		"diff-tree", "--no-commit-id", "--name-only", "-r", "-z", "--root", "HEAD")
	if err != nil {
		return nil, err
	}
	var files []string
	for p := range strings.SplitSeq(out, "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	return files, nil
}
