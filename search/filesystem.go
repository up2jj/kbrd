package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"kbrd/board"
)

const maxFilesystemMatches = 200

// FilesystemSource adapts ripgrep output into Match values. Normal roots are
// board column directories; path-backed virtual items outside those directories
// are added as explicit Markdown files.
type FilesystemSource struct {
	roots    []Root
	virtuals []VirtualItem
}

// NewFilesystemSource creates an immutable filesystem search adapter.
func NewFilesystemSource(roots []Root, virtuals []VirtualItem) FilesystemSource {
	return FilesystemSource{
		roots:    append([]Root(nil), roots...),
		virtuals: cloneVirtualItems(virtuals),
	}
}

// Search runs one case-insensitive fixed-string ripgrep query.
func (s FilesystemSource) Search(ctx context.Context, query string) ([]Match, error) {
	paths := s.paths()
	if len(paths) == 0 {
		return nil, nil
	}

	args := []string{"--json", "--fixed-strings", "--ignore-case", "-g", "*.md", "--max-depth", "1", "-m", "20", "--", query}
	args = append(args, paths...)
	out, err := exec.CommandContext(ctx, "rg", args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("ripgrep (rg) not installed")
		}
		return nil, fmt.Errorf("search failed: %w", err)
	}

	matches := parseRipgrep(out, s.roots)
	for i := range matches {
		if matches[i].BoardPath != "" {
			continue
		}
		if owner, ok := virtualOwner(matches[i].FilePath, s.virtuals); ok {
			matches[i].BoardPath = owner.BoardPath
			matches[i].BoardName = owner.BoardName
		}
	}
	return matches, nil
}

func (s FilesystemSource) paths() []string {
	dirs := columnPaths(s.roots)
	paths := append([]string(nil), dirs...)
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		seen[canonicalPath(path)] = struct{}{}
	}
	for _, item := range s.virtuals {
		path := item.FilePath
		if path == "" || !strings.EqualFold(filepath.Ext(path), ".md") || coveredBySearchDir(path, dirs) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		key := canonicalPath(path)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

func columnPaths(roots []Root) []string {
	var dirs []string
	for _, root := range roots {
		columns, err := board.Columns(root.Path)
		if err != nil {
			continue
		}
		for _, name := range columns {
			dirs = append(dirs, filepath.Join(root.Path, name))
		}
	}
	return dirs
}

func coveredBySearchDir(path string, dirs []string) bool {
	path = canonicalPath(path)
	for _, dir := range dirs {
		rel, err := filepath.Rel(canonicalPath(dir), path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func virtualOwner(path string, items []VirtualItem) (VirtualItem, bool) {
	for _, item := range items {
		if item.FilePath != "" && samePath(item.FilePath, path) {
			return item, true
		}
	}
	return VirtualItem{}, false
}

func canonicalPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

func samePath(a, b string) bool {
	return a == b || canonicalPath(a) == canonicalPath(b)
}

type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

func parseRipgrep(out []byte, roots []Root) []Match {
	matches := make([]Match, 0, 32)
	for line := range strings.SplitSeq(string(out), "\n") {
		if line == "" {
			continue
		}
		var event rgEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil || event.Type != "match" {
			continue
		}
		path := event.Data.Path.Text
		text, col, length := matchSpan(event.Data.Lines.Text, event.Data.Submatches)
		boardPath, boardName := boardForPath(path, roots)
		matches = append(matches, Match{
			BoardPath: boardPath,
			BoardName: boardName,
			FilePath:  path,
			Column:    filepath.Base(filepath.Dir(path)),
			Item:      strings.TrimSuffix(filepath.Base(path), ".md"),
			Line:      event.Data.LineNumber,
			Text:      text,
			MatchCol:  col,
			MatchLen:  length,
		})
		if len(matches) >= maxFilesystemMatches {
			break
		}
	}
	return matches
}

func matchSpan(raw string, submatches []struct {
	Start int `json:"start"`
	End   int `json:"end"`
}) (text string, runeCol, runeLen int) {
	raw = strings.TrimRight(raw, "\r\n")
	trimmedLeft := len(raw) - len(strings.TrimLeft(raw, " \t"))
	text = strings.TrimSpace(raw)
	if len(submatches) == 0 {
		return text, 0, 0
	}
	start := submatches[0].Start - trimmedLeft
	end := submatches[0].End - trimmedLeft
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	if end < start {
		end = start
	}
	runeCol = utf8.RuneCountInString(text[:start])
	runeLen = utf8.RuneCountInString(text[start:end])
	return text, runeCol, runeLen
}

func boardForPath(path string, roots []Root) (rootPath, name string) {
	best := -1
	for _, root := range roots {
		if strings.HasPrefix(path, root.Path+string(filepath.Separator)) || path == root.Path {
			if len(root.Path) > best {
				best = len(root.Path)
				rootPath = root.Path
				name = root.Name
			}
		}
	}
	return rootPath, name
}
