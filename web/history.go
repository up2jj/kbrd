package web

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"kbrd/fs"
)

const historyLimit = 50

// commitView is one row of history.html.
type commitView struct {
	Short   string
	Subject string
	Author  string
	When    string // relative time, e.g. "3 hours ago"
}

// handleHistory renders the board-wide commit history. Without git (nil
// Syncer) the page doesn't exist — the topbar link is hidden too.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if s.sync == nil {
		http.NotFound(w, r)
		return
	}
	commits, err := fs.GitLog(s.sync.root, historyLimit)
	if err != nil {
		http.Error(w, "failed to read history", http.StatusInternalServerError)
		return
	}
	views := make([]commitView, 0, len(commits))
	now := time.Now()
	for _, c := range commits {
		views = append(views, commitView{
			Short:   c.Short,
			Subject: c.Subject,
			Author:  c.Author,
			When:    relTime(c.Time, now),
		})
	}
	s.render(w, "history.html", s.page(map[string]any{"Commits": views}))
}

// relTime renders t relative to now ("3 hours ago"); anything older than four
// weeks falls back to the date.
func relTime(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < 2*time.Minute:
		return "1 minute ago"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", d/time.Minute)
	case d < 2*time.Hour:
		return "1 hour ago"
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", d/time.Hour)
	case d < 48*time.Hour:
		return "1 day ago"
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%d days ago", d/(24*time.Hour))
	case d < 28*24*time.Hour:
		return fmt.Sprintf("%d weeks ago", d/(7*24*time.Hour))
	default:
		return t.Format("2006-01-02")
	}
}

// headChangedSet returns the item files touched by the HEAD commit, keyed by
// board-relative slash path ("col/name.md"). nil when git is unavailable, the
// repo is empty, or paths can't be resolved — callers just skip the badges.
func (s *Server) headChangedSet() map[string]bool {
	if s.sync == nil {
		return nil
	}
	files, err := fs.GitHeadChangedFiles(s.sync.root)
	if err != nil || len(files) == 0 {
		return nil
	}
	// The board may live in a subdirectory of the repo; translate repo-relative
	// paths to board-relative ones.
	boardAbs, err := filepath.Abs(s.opts.BoardPath)
	if err != nil {
		return nil
	}
	// The Syncer root comes from `git rev-parse` with symlinks resolved (macOS
	// tempdirs are symlinked); resolve the board path the same way.
	if resolved, err := filepath.EvalSymlinks(boardAbs); err == nil {
		boardAbs = resolved
	}
	rel, err := filepath.Rel(s.sync.root, boardAbs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil
	}
	prefix := ""
	if rel != "." {
		prefix = filepath.ToSlash(rel) + "/"
	}
	set := make(map[string]bool, len(files))
	for _, f := range files {
		if p, ok := strings.CutPrefix(f, prefix); ok {
			set[p] = true
		}
	}
	return set
}

// markChanged flags cards whose file was touched by the HEAD commit.
func markChanged(cols []Column, changed map[string]bool) []Column {
	if len(changed) == 0 {
		return cols
	}
	for ci := range cols {
		for i := range cols[ci].Cards {
			key := cols[ci].Name + "/" + cols[ci].Cards[i].Name + ".md"
			cols[ci].Cards[i].Changed = changed[key]
		}
	}
	return cols
}
