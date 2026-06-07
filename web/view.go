// Package web is the headless mobile-first web frontend: a small HTTP(S)
// server over the board filesystem (package board) that persists every
// mutation as a git commit + push. It mirrors the mcp package's role as a
// second frontend — it never imports the TUI (package model).
package web

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"

	"kbrd/board"
	"kbrd/frontmatter"
)

const previewLines = 3

// Card is the view model for one item file.
type Card struct {
	Name    string // real file base name (identifier in URLs), e.g. "p_task"
	Title   string // display title: frontmatter/H1/name with "p_" stripped
	Icon    string
	Accent  string
	Tags    []string
	Preview []string // first few body lines, frontmatter and H1 excluded
	Pinned  bool
}

// Column is the view model for one column directory.
type Column struct {
	Name  string
	Cards []Card
}

// loadBoard builds the full board view, reading fresh from disk.
func loadBoard(boardPath string) ([]Column, error) {
	names, err := board.Columns(boardPath)
	if err != nil {
		return nil, err
	}
	cols := make([]Column, 0, len(names))
	for _, name := range names {
		col, err := loadColumn(boardPath, name)
		if err != nil {
			return nil, err
		}
		cols = append(cols, col)
	}
	return cols, nil
}

// loadColumn builds one column view: pinned cards first, then alphabetical.
func loadColumn(boardPath, name string) (Column, error) {
	colPath := filepath.Join(boardPath, name)
	items, err := board.Items(colPath)
	if err != nil {
		return Column{}, err
	}
	cards := make([]Card, 0, len(items))
	for _, item := range items {
		cards = append(cards, loadCard(colPath, item))
	}
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Pinned != cards[j].Pinned {
			return cards[i].Pinned
		}
		return cards[i].Title < cards[j].Title
	})
	return Column{Name: name, Cards: cards}, nil
}

// loadCard parses a single item file into its view model. Read errors yield a
// bare card (name only) rather than failing the page — mirrors the lenient
// stance of the TUI loader.
func loadCard(columnPath, name string) Card {
	c := Card{Name: name, Pinned: board.PinnedName(name)}
	c.Title = board.UnpinnedName(name)

	raw, err := board.ReadItem(columnPath, name)
	if err != nil {
		return c
	}
	fmBlock, body := splitFrontmatter(raw)
	if fm, err := frontmatter.Parse([]byte(fmBlock)); err == nil {
		c.Icon = fm.Icon
		c.Accent = fm.Accent
		c.Tags = fm.Tags
	}

	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if h, ok := strings.CutPrefix(trimmed, "# "); ok && c.Title == board.UnpinnedName(name) {
			c.Title = strings.TrimSpace(h)
			continue
		}
		if len(c.Preview) < previewLines {
			c.Preview = append(c.Preview, trimmed)
		}
	}
	return c
}

// splitFrontmatter splits raw content into the frontmatter block (the bytes
// between leading "---" fences, without the fences) and the remaining body.
// Content without a leading fence comes back unchanged as body. The block is
// capped at frontmatter.MaxBytes, mirroring the TUI loader's bound.
func splitFrontmatter(raw string) (block, body string) {
	rest, ok := strings.CutPrefix(raw, "---\n")
	if !ok {
		if raw == "---" { // degenerate: fence-only file
			return "", raw
		}
		return "", raw
	}
	// Closing fence: a line that is exactly "---".
	for i := 0; i <= len(rest); {
		end := strings.Index(rest[i:], "\n")
		line := ""
		next := len(rest)
		if end >= 0 {
			line = rest[i : i+end]
			next = i + end + 1
		} else {
			line = rest[i:]
		}
		if strings.TrimRight(line, " \t\r") == "---" {
			b := rest[:i]
			if len(b) > frontmatter.MaxBytes {
				b = b[:frontmatter.MaxBytes]
			}
			return b, rest[next:]
		}
		if end < 0 {
			break
		}
		i = next
	}
	// No closing fence: treat the whole file as body.
	return "", raw
}

// contentHash is the optimistic-concurrency token carried by the edit form.
func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
