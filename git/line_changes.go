package git

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	kbrdfs "kbrd/fs"
)

type LineChangeKind int

const (
	LineAdded LineChangeKind = iota
	LineModified
	LineDeleted
)

type LineChange struct {
	Line int
	Kind LineChangeKind
}

func lineChangesFor(repoRoot, absPath string) []LineChange {
	if repoRoot == "" || absPath == "" || !kbrdfs.GitAvailable() {
		return nil
	}
	rel, ok := repoRelativePath(repoRoot, absPath)
	if !ok {
		return nil
	}
	for _, f := range kbrdfs.GitChangedFiles(repoRoot) {
		if f.Path != rel {
			continue
		}
		if f.Status == "??" {
			return allAddedLines(absPath)
		}
		out, err := kbrdfs.GitOutput(repoRoot, "diff", "--unified=0", "HEAD", "--", rel)
		if err != nil || strings.TrimSpace(out) == "" {
			return nil
		}
		return parseLineChanges(out, currentLineCount(absPath))
	}
	return nil
}

func repoRelativePath(repoRoot, absPath string) (string, bool) {
	repoForRel := repoRoot
	pathForRel := absPath
	if resolved, err := filepath.EvalSymlinks(repoRoot); err == nil {
		repoForRel = resolved
	}
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		pathForRel = resolved
	}
	rel, err := filepath.Rel(repoForRel, pathForRel)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func allAddedLines(path string) []LineChange {
	total := currentLineCount(path)
	if total <= 0 {
		return nil
	}
	changes := make([]LineChange, total)
	for i := 1; i <= total; i++ {
		changes[i-1] = LineChange{Line: i, Kind: LineAdded}
	}
	return changes
}

func currentLineCount(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return 0
	}
	lines := bytes.Count(raw, []byte{'\n'})
	if raw[len(raw)-1] != '\n' {
		lines++
	}
	return lines
}

func parseLineChanges(diff string, currentLines int) []LineChange {
	type hunk struct {
		newStart int
		newCount int
		added    []int
		deleted  int
	}
	changes := map[int]LineChangeKind{}
	flush := func(h hunk) {
		switch {
		case h.deleted > 0 && len(h.added) > 0:
			modified := min(h.deleted, len(h.added))
			for i, line := range h.added {
				if i < modified {
					changes[line] = LineModified
				} else {
					changes[line] = LineAdded
				}
			}
		case len(h.added) > 0:
			for _, line := range h.added {
				changes[line] = LineAdded
			}
		case h.deleted > 0:
			line := deletionAnchorLine(h.newStart, currentLines)
			if line > 0 {
				changes[line] = LineDeleted
			}
		}
	}

	var cur hunk
	inHunk := false
	newLine := 0
	for line := range strings.SplitSeq(diff, "\n") {
		if strings.HasPrefix(line, "@@ ") {
			if inHunk {
				flush(cur)
			}
			fields := strings.Fields(line)
			if len(fields) < 3 {
				inHunk = false
				continue
			}
			newStart, newCount, ok := parseDiffRange(fields[2])
			if !ok {
				inHunk = false
				continue
			}
			cur = hunk{newStart: newStart, newCount: newCount}
			newLine = newStart
			inHunk = true
			continue
		}
		if !inHunk || line == `\ No newline at end of file` {
			continue
		}
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			if cur.newCount > 0 {
				cur.added = append(cur.added, newLine)
			}
			newLine++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			cur.deleted++
		default:
			if cur.newCount > 0 {
				newLine++
			}
		}
	}
	if inHunk {
		flush(cur)
	}

	if len(changes) == 0 {
		return nil
	}
	lines := make([]int, 0, len(changes))
	for line := range changes {
		lines = append(lines, line)
	}
	sort.Ints(lines)
	out := make([]LineChange, 0, len(lines))
	for _, line := range lines {
		out = append(out, LineChange{Line: line, Kind: changes[line]})
	}
	return out
}

func parseDiffRange(s string) (int, int, bool) {
	if s == "" {
		return 0, 0, false
	}
	s = strings.TrimLeft(s, "+-")
	parts := strings.SplitN(s, ",", 2)
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	count := 1
	if len(parts) == 2 {
		count, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, false
		}
	}
	return start, count, true
}

func deletionAnchorLine(newStart, currentLines int) int {
	if currentLines <= 0 {
		return 0
	}
	if newStart <= 0 {
		return 1
	}
	if newStart >= currentLines {
		return currentLines
	}
	return newStart + 1
}
