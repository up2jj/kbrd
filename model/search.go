package model

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"kbrd/recents"
	searchbackend "kbrd/search"
)

// maxSearchResults bounds how many match rows we keep/render so a broad query
// against many boards can't flood the dialog or the terminal.
const (
	maxSearchResults = 200
	searchTimeout    = 10 * time.Second
)

// searchSelectMsg is emitted when the user picks a result: the board is
// activated (switching if needed) and the file is auto-selected.
type searchSelectMsg struct {
	BoardPath   string
	FilePath    string
	VirtualVID  string
	VirtualItem string
}

// searchMsg marks search-internal async messages so the host can route them
// opaquely (`case searchMsg: return b, b.search.Update(msg)`) without naming
// the concrete types — the same pattern git uses with git.Msg. Note that
// searchSelectMsg is deliberately NOT a searchMsg: it is search's output to the
// host (switch board + select file), not internal plumbing.
type searchMsg interface{ isSearchMsg() }

// searchMsgBase is embedded in every search-internal message to satisfy searchMsg.
type searchMsgBase struct{}

func (searchMsgBase) isSearchMsg() {}

// searchDebounceMsg fires after the typing debounce. Search runs its adapters
// only if Seq still matches the dialog's current generation.
type searchDebounceMsg struct {
	searchMsgBase
	Seq int
}

// searchResultsMsg carries merged adapter output back to the dialog. Stale
// results (Seq behind the dialog) are discarded.
type searchResultsMsg struct {
	searchMsgBase
	Seq     int
	Results []searchResult
	Err     string
}

type searchResult struct {
	BoardPath   string // board root the match belongs to
	BoardName   string // recents Name, for labeling
	FilePath    string // absolute path of the .md file
	Column      string // immediate parent dir name (column)
	Item        string // file basename without .md
	Line        int
	Text        string // matched line, trimmed
	matchCol    int    // rune offset of match start within Text
	matchLen    int    // rune length of match within Text
	virtualVID  string
	virtualItem string
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
	BoardPath   string
	BoardName   string
	FilePath    string
	Column      string
	Item        string
	Matches     []matchLine
	VirtualVID  string
	VirtualItem string
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
	virtuals []searchbackend.VirtualItem
	sources  []searchbackend.Source
	palette  Palette
}

func (s *Search) Open(roots []recents.Entry, virtuals []searchbackend.VirtualItem, palette Palette) {
	s.active = true
	s.filter = ""
	s.groups = nil
	s.selected = 0
	s.running = false
	s.err = ""
	s.roots = roots
	s.virtuals = virtuals
	searchRoots := make([]searchbackend.Root, len(roots))
	for i, root := range roots {
		searchRoots[i] = searchbackend.Root{Path: root.Path, Name: root.Name}
	}
	s.sources = []searchbackend.Source{
		searchbackend.NewVirtualSource(virtuals),
		searchbackend.NewFilesystemSource(searchRoots, virtuals),
	}
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
	s.virtuals = nil
	s.sources = nil
}

func (s *Search) Active() bool { return s.active }

// debouncedRun returns the adapter command for a debounce tick, or nil if the
// tick is stale (the query changed since it was scheduled).
func (s *Search) debouncedRun(seq int) tea.Cmd {
	if !s.active || seq != s.seq {
		return nil
	}
	return runSearch(seq, s.filter, s.sources)
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
		if idx, ok := col.IndexByName(item); ok {
			return i, idx, true
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

// setResults applies async adapter output if it belongs to the current query.
func (s *Search) setResults(msg searchResultsMsg) {
	if msg.Seq != s.seq {
		return // stale
	}
	s.running = false
	s.err = msg.Err
	s.groups = groupSearchResults(msg.Results, s.virtuals)
	if s.selected >= len(s.groups) {
		s.selected = len(s.groups) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
}

// groupByFile is the filesystem-only compatibility wrapper used by focused
// grouping tests. Production search also supplies active virtual items.
func groupByFile(results []searchResult) []fileGroup {
	return groupSearchResults(results, nil)
}

// groupSearchResults merges filesystem and virtual-source hits. Path-backed
// hits share one canonical-path identity; fileless virtual items use their
// board/column/item identity and therefore never collide merely by title.
func groupSearchResults(results []searchResult, virtuals []searchbackend.VirtualItem) []fileGroup {
	groups := make([]fileGroup, 0, len(results))
	index := map[string]int{}
	for _, r := range results {
		key := searchResultKey(r)
		gi, ok := index[key]
		if !ok {
			gi = len(groups)
			index[key] = gi
			groups = append(groups, fileGroup{
				BoardPath:   r.BoardPath,
				BoardName:   r.BoardName,
				FilePath:    r.FilePath,
				Column:      r.Column,
				Item:        r.Item,
				VirtualVID:  r.virtualVID,
				VirtualItem: r.virtualItem,
			})
		}
		match := matchLine{
			Line:     r.Line,
			Text:     r.Text,
			matchCol: r.matchCol,
			matchLen: r.matchLen,
		}
		if !containsSearchMatch(groups[gi].Matches, match) {
			groups[gi].Matches = append(groups[gi].Matches, match)
		}
	}
	decorateSearchGroups(groups, virtuals)
	if len(groups) > maxSearchResults {
		groups = groups[:maxSearchResults]
	}
	return groups
}

func searchResultKey(r searchResult) string {
	if r.FilePath != "" {
		return "file:" + canonicalSearchPath(r.FilePath)
	}
	return "virtual:" + canonicalSearchPath(r.BoardPath) + "\x00" + r.virtualVID + "\x00" + r.virtualItem
}

func canonicalSearchPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

func containsSearchMatch(matches []matchLine, candidate matchLine) bool {
	for _, match := range matches {
		if match.Line == candidate.Line && match.Text == candidate.Text &&
			match.matchCol == candidate.matchCol && match.matchLen == candidate.matchLen {
			return true
		}
	}
	return false
}

// decorateSearchGroups makes a path-backed result look and activate like its
// active-layer representation even when only the underlying file body matched.
func decorateSearchGroups(groups []fileGroup, virtuals []searchbackend.VirtualItem) {
	byPath := make(map[string]searchbackend.VirtualItem, len(virtuals))
	for _, item := range virtuals {
		if item.FilePath == "" {
			continue
		}
		key := canonicalSearchPath(item.FilePath)
		if _, exists := byPath[key]; !exists {
			byPath[key] = item
		}
	}
	for i := range groups {
		if groups[i].FilePath == "" {
			continue
		}
		item, ok := byPath[canonicalSearchPath(groups[i].FilePath)]
		if !ok {
			continue
		}
		groups[i].Column = item.Column
		groups[i].Item = item.Title
		if groups[i].Item == "" {
			groups[i].Item = item.ID
		}
		groups[i].VirtualVID = item.VID
		groups[i].VirtualItem = item.ID
	}
}

// Update routes a search-internal async message to its handler. Keystrokes go
// through HandleKey instead.
func (s *Search) Update(m searchMsg) tea.Cmd {
	switch msg := m.(type) {
	case searchDebounceMsg:
		return s.debouncedRun(msg.Seq)
	case searchResultsMsg:
		s.setResults(msg)
	}
	return nil
}

func (s *Search) HandleKey(msg tea.KeyPressMsg) tea.Cmd {
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
			return searchSelectMsg{
				BoardPath:   g.BoardPath,
				FilePath:    g.FilePath,
				VirtualVID:  g.VirtualVID,
				VirtualItem: g.VirtualItem,
			}
		}
	}

	switch msg.Code {
	case tea.KeyBackspace:
		if r := []rune(s.filter); len(r) > 0 {
			s.filter = string(r[:len(r)-1])
			return s.queryChanged()
		}
		return nil
	default:
		if msg.Text != "" {
			s.filter += msg.Text
			s.selected = 0
			return s.queryChanged()
		}
		return nil
	}
}

// queryChanged bumps the generation and schedules a debounced adapter run. An
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

// searchBoxWidth picks a stable dialog width from the terminal width so the box
// doesn't grow/shrink with the length of the query or the matched lines.
func searchBoxWidth(termWidth int) int {
	const max = 100
	w := min(termWidth-8, max)
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
	body = lipgloss.JoinVertical(lipgloss.Left, filterLine, "", body)

	return OverlayFrame{
		Title:   "Search in boards",
		Body:    body,
		Footer:  footer,
		Width:   boxWidth,
		Palette: p,
	}.Render()
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
			lineNo := ""
			if m.Line > 0 {
				lineNo = lineStyle.Render(strconv.Itoa(m.Line) + ": ")
			}
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
	for i := range length {
		out = append(out, col+i)
	}
	return out
}
