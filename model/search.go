package model

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"kbrd/board"
	"kbrd/recents"
)

// maxSearchResults bounds how many match rows we keep/render so a broad query
// against many boards can't flood the dialog or the terminal.
const maxSearchResults = 200

// searchSelectMsg is emitted when the user picks a result: the board is
// activated (switching if needed) and the file is auto-selected.
type searchSelectMsg struct {
	BoardPath string
	FilePath  string
}

// searchDebounceMsg fires after the typing debounce. The board runs ripgrep
// only if Seq still matches the dialog's current generation.
type searchDebounceMsg struct {
	Seq int
}

// searchResultsMsg carries ripgrep output back to the dialog. Stale results
// (Seq behind the dialog) are discarded.
type searchResultsMsg struct {
	Seq     int
	Results []searchResult
	Err     string
}

type searchResult struct {
	BoardPath string // board root the match belongs to
	BoardName string // recents Name, for labeling
	FilePath  string // absolute path of the .md file
	Column    string // immediate parent dir name (column)
	Item      string // file basename without .md
	Line      int
	Text      string // matched line, trimmed
	matchCol  int    // rune offset of match start within Text
	matchLen  int    // rune length of match within Text
}

// matchLine is one matching line within a file.
type matchLine struct {
	Line     int
	Text     string
	matchCol int
	matchLen int
}

// fileGroup collects every match within a single file. It is the unit the user
// selects and opens.
type fileGroup struct {
	BoardPath string
	BoardName string
	FilePath  string
	Column    string
	Item      string
	Matches   []matchLine
}

type Search struct {
	active   bool
	filter   string
	groups   []fileGroup
	selected int
	seq      int // generation counter to discard stale async results
	running  bool
	err      string
	roots    []recents.Entry // board roots to search (recents + current)
	palette  Palette
}

func (s *Search) Open(roots []recents.Entry, palette Palette) {
	s.active = true
	s.filter = ""
	s.groups = nil
	s.selected = 0
	s.running = false
	s.err = ""
	s.roots = roots
	s.palette = palette
}

func (s *Search) Close() {
	s.active = false
	s.filter = ""
	s.groups = nil
	s.selected = 0
	s.running = false
	s.err = ""
	s.roots = nil
}

func (s *Search) Active() bool { return s.active }

// debouncedRun returns the ripgrep command for a debounce tick, or nil if the
// tick is stale (the query changed since it was scheduled).
func (s *Search) debouncedRun(seq int) tea.Cmd {
	if !s.active || seq != s.seq {
		return nil
	}
	return runRipgrep(seq, s.filter, s.roots)
}

// buildSearchRoots returns the boards to search: the active board first, then
// every recents entry, deduplicated by absolute path.
func buildSearchRoots(activeAbs, activeName string, entries []recents.Entry) []recents.Entry {
	roots := make([]recents.Entry, 0, len(entries)+1)
	seen := map[string]bool{}
	add := func(path, name string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		roots = append(roots, recents.Entry{Path: path, Name: name})
	}
	add(activeAbs, activeName)
	for _, e := range entries {
		abs, err := filepath.Abs(e.Path)
		if err != nil {
			abs = e.Path
		}
		add(abs, e.Name)
	}
	return roots
}

// locateFile finds the column and item index for filePath: the column whose
// directory contains the file, and the item matching its basename.
func locateFile(columns []*Column, filePath string) (colIdx, itemIdx int, ok bool) {
	dir := filepath.Dir(filePath)
	item := strings.TrimSuffix(filepath.Base(filePath), ".md")
	for i, col := range columns {
		if !samePath(col.Path, dir) {
			continue
		}
		for j := range col.Items {
			if col.Items[j].Name == item {
				return i, j, true
			}
		}
	}
	return 0, 0, false
}

// samePath reports whether two paths refer to the same location after
// resolving to absolute form.
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && aa == bb
}

// setResults applies async ripgrep output if it belongs to the current query.
func (s *Search) setResults(msg searchResultsMsg) {
	if msg.Seq != s.seq {
		return // stale
	}
	s.running = false
	s.err = msg.Err
	s.groups = groupByFile(msg.Results)
	if s.selected >= len(s.groups) {
		s.selected = len(s.groups) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
}

// groupByFile collapses per-line results into one group per file, preserving
// the order files first appear in the ripgrep output.
func groupByFile(results []searchResult) []fileGroup {
	groups := make([]fileGroup, 0, len(results))
	index := map[string]int{}
	for _, r := range results {
		gi, ok := index[r.FilePath]
		if !ok {
			gi = len(groups)
			index[r.FilePath] = gi
			groups = append(groups, fileGroup{
				BoardPath: r.BoardPath,
				BoardName: r.BoardName,
				FilePath:  r.FilePath,
				Column:    r.Column,
				Item:      r.Item,
			})
		}
		groups[gi].Matches = append(groups[gi].Matches, matchLine{
			Line:     r.Line,
			Text:     r.Text,
			matchCol: r.matchCol,
			matchLen: r.matchLen,
		})
	}
	return groups
}

func (s *Search) Update(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, Keys.SearchClose):
		s.Close()
		return nil
	case key.Matches(msg, Keys.SearchPrev):
		if s.selected > 0 {
			s.selected--
		}
		return nil
	case key.Matches(msg, Keys.SearchNext):
		if s.selected < len(s.groups)-1 {
			s.selected++
		}
		return nil
	case key.Matches(msg, Keys.SearchConfirm):
		if len(s.groups) == 0 {
			return nil
		}
		g := s.groups[s.selected]
		s.Close()
		return func() tea.Msg {
			return searchSelectMsg{BoardPath: g.BoardPath, FilePath: g.FilePath}
		}
	}

	switch msg.Type {
	case tea.KeyBackspace:
		if r := []rune(s.filter); len(r) > 0 {
			s.filter = string(r[:len(r)-1])
			return s.queryChanged()
		}
		return nil
	case tea.KeyRunes, tea.KeySpace:
		if str := msg.String(); str != "" {
			s.filter += str
			s.selected = 0
			return s.queryChanged()
		}
		return nil
	}
	return nil
}

// queryChanged bumps the generation and schedules a debounced ripgrep run. An
// empty query clears results without running anything.
func (s *Search) queryChanged() tea.Cmd {
	s.seq++
	if strings.TrimSpace(s.filter) == "" {
		s.running = false
		s.groups = nil
		s.err = ""
		return nil
	}
	s.running = true
	s.err = ""
	seq := s.seq
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		return searchDebounceMsg{Seq: seq}
	})
}

// runRipgrep executes ripgrep across all roots for query and returns the parsed
// results as a searchResultsMsg tagged with seq.
func runRipgrep(seq int, query string, roots []recents.Entry) tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(query) == "" {
			return searchResultsMsg{Seq: seq}
		}
		paths := columnPaths(roots)
		if len(paths) == 0 {
			return searchResultsMsg{Seq: seq}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// --max-depth 1 + the column dirs as roots restricts matches to items
		// living directly inside a board's columns (board/column/file.md).
		args := []string{"--json", "--fixed-strings", "--ignore-case", "-g", "*.md", "--max-depth", "1", "-m", "20", "--", query}
		args = append(args, paths...)
		out, err := exec.CommandContext(ctx, "rg", args...).Output()
		if err != nil {
			// rg exits 1 when there are no matches — that is not an error.
			var ee *exec.ExitError
			if errors.As(err, &ee) && ee.ExitCode() == 1 {
				return searchResultsMsg{Seq: seq}
			}
			if errors.Is(err, exec.ErrNotFound) {
				return searchResultsMsg{Seq: seq, Err: "ripgrep (rg) not installed"}
			}
			return searchResultsMsg{Seq: seq, Err: "search failed: " + err.Error()}
		}
		return searchResultsMsg{Seq: seq, Results: parseRipgrep(out, roots)}
	}
}

// columnPaths returns the immediate subdirectories (columns) of every board
// root, applying the same skip rules as the board loader: hidden (.) and
// private (_) directories are excluded. Search is scoped to these so only
// column-level items (board/column/*.md) can match.
func columnPaths(roots []recents.Entry) []string {
	var dirs []string
	for _, e := range roots {
		cols, err := board.Columns(e.Path)
		if err != nil {
			continue
		}
		for _, name := range cols {
			dirs = append(dirs, filepath.Join(e.Path, name))
		}
	}
	return dirs
}

// rgEvent mirrors the subset of ripgrep --json "match" events we consume.
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

func parseRipgrep(out []byte, roots []recents.Entry) []searchResult {
	results := make([]searchResult, 0, 32)
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		var ev rgEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type != "match" {
			continue
		}
		fp := ev.Data.Path.Text
		text, col, length := matchSpan(ev.Data.Lines.Text, ev.Data.Submatches)
		board, name := boardForPath(fp, roots)
		results = append(results, searchResult{
			BoardPath: board,
			BoardName: name,
			FilePath:  fp,
			Column:    filepath.Base(filepath.Dir(fp)),
			Item:      strings.TrimSuffix(filepath.Base(fp), ".md"),
			Line:      ev.Data.LineNumber,
			Text:      text,
			matchCol:  col,
			matchLen:  length,
		})
		if len(results) >= maxSearchResults {
			break
		}
	}
	return results
}

// matchSpan trims the raw matched line and translates the first submatch's byte
// offsets into rune offsets within the trimmed text (for highlighting).
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
	bs := submatches[0].Start - trimmedLeft
	be := submatches[0].End - trimmedLeft
	if bs < 0 {
		bs = 0
	}
	if be > len(text) {
		be = len(text)
	}
	if be < bs {
		be = bs
	}
	runeCol = utf8.RuneCountInString(text[:bs])
	runeLen = utf8.RuneCountInString(text[bs:be])
	return text, runeCol, runeLen
}

// boardForPath returns the root whose path is the longest prefix of fp.
func boardForPath(fp string, roots []recents.Entry) (path, name string) {
	best := -1
	for _, e := range roots {
		root := e.Path
		if strings.HasPrefix(fp, root+string(filepath.Separator)) || fp == root {
			if len(root) > best {
				best = len(root)
				path = root
				name = e.Name
			}
		}
	}
	return path, name
}

// searchBoxWidth picks a stable dialog width from the terminal width so the box
// doesn't grow/shrink with the length of the query or the matched lines.
func searchBoxWidth(termWidth int) int {
	const max = 100
	w := termWidth - 8
	if w > max {
		w = max
	}
	if w < 40 {
		w = 40
	}
	return w
}

func (s *Search) View(termWidth, termHeight int) string {
	p := s.palette
	boxWidth := searchBoxWidth(termWidth)
	textWidth := boxWidth - 6 // inside Padding(1, 3)
	clip := func(str string) string { return lipgloss.NewStyle().MaxWidth(textWidth).Render(str) }

	title := helpTitleStyle.Render("Search in boards")

	descStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	nameStyle := lipgloss.NewStyle().Foreground(p.Highlight)
	keyStyle := lipgloss.NewStyle().Foreground(p.Highlight).Bold(true)

	cursor := keyStyle.Render("> ")
	filterText := s.filter
	if filterText == "" {
		filterText = descStyle.Render("type a phrase to search…")
	} else {
		filterText = nameStyle.Render(filterText)
	}
	filterLine := clip(cursor + filterText)

	var body string
	switch {
	case s.err != "":
		body = lipgloss.NewStyle().Foreground(p.Danger).Render(s.err)
	case strings.TrimSpace(s.filter) == "":
		body = helpDimStyle.Render("results appear as you type")
	case s.running:
		body = helpDimStyle.Render("searching…")
	case len(s.groups) == 0:
		body = helpDimStyle.Render("no matches")
	default:
		body = s.renderResults(textWidth)
	}

	footer := RenderInlineHints([]Shortcut{
		{Keys: "type", Label: "search"},
		{Keys: "↑/↓", Label: "select"},
		{Keys: "enter", Label: "open"},
		{Keys: "esc", Label: "cancel"},
	})
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", filterLine, "", body, "", footer)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.BorderActive).
		Padding(1, 3).
		Width(boxWidth).
		Render(content)
}

func (s *Search) renderResults(textWidth int) string {
	clip := func(str string) string { return lipgloss.NewStyle().MaxWidth(textWidth).Render(str) }
	p := s.palette
	mutedStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	nameStyle := lipgloss.NewStyle().Foreground(p.Highlight).Bold(true)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	selCountStyle := lipgloss.NewStyle().Foreground(p.FgInverse).Background(p.Primary)
	countStyle := lipgloss.NewStyle().Foreground(p.FgDim)
	lineStyle := lipgloss.NewStyle().Foreground(p.FgDim)
	textStyle := lipgloss.NewStyle().Foreground(p.FgBase)
	hiStyle := lipgloss.NewStyle().Foreground(p.Highlight).Bold(true)
	gutterSel := lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("▌")

	rows := make([]string, 0, len(s.groups)*3)
	for i, g := range s.groups {
		selected := i == s.selected

		label := g.BoardName
		if label == "" {
			label = filepath.Base(g.BoardPath)
		}
		prefix := label + " · " + g.Column + "/"
		countText := ""
		if len(g.Matches) > 1 {
			countText = "  (" + strconv.Itoa(len(g.Matches)) + ")"
		}

		var header string
		gutter := " "
		if selected {
			// The whole header (board · column/file) gets the inverse highlight.
			gutter = gutterSel
			header = selStyle.Render(prefix+g.Item) + selCountStyle.Render(countText)
		} else {
			count := ""
			if countText != "" {
				count = countStyle.Render(countText)
			}
			header = mutedStyle.Render(prefix) + nameStyle.Render(g.Item) + count
		}
		rows = append(rows, clip(gutter+" "+header))

		for _, m := range g.Matches {
			lineNo := lineStyle.Render(strconv.Itoa(m.Line) + ": ")
			text := renderHighlighted(m.Text, matchIndexes(m.matchCol, m.matchLen), textStyle, hiStyle)
			rows = append(rows, clip("     "+lineNo+text))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func matchIndexes(col, length int) []int {
	if length <= 0 {
		return nil
	}
	out := make([]int, 0, length)
	for i := 0; i < length; i++ {
		out = append(out, col+i)
	}
	return out
}
