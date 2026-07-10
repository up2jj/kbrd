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
)

const previewLines = 3

// Card is the view model for one item file.
type Card struct {
	Name    string // file base name without ".md" (identifier in URLs)
	Title   string // display title: H1 heading when present, else the name
	Icon    string
	Accent  string
	Tags    []string
	Preview []string // first few body lines, frontmatter and H1 excluded
	Pinned  bool
	Changed bool // file touched by the latest commit (HEAD)

	search string // lowercased title + tags + body, for the quick filter
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
	c := Card{Name: name, Title: name}

	raw, err := board.ReadItem(columnPath, name)
	if err != nil {
		c.search = strings.ToLower(c.Title)
		return c
	}
	p := board.ProjectCardContent(name, raw, board.CardProjectionOptions{PreviewLines: previewLines, TitleFromHeading: true})
	c.Title = p.Title
	c.Icon = p.Icon
	c.Accent = p.Accent
	c.Tags = p.Tags
	c.Preview = p.Preview
	c.Pinned = p.Pinned
	c.search = strings.ToLower(c.Title + "\n" + strings.Join(c.Tags, "\n") + "\n" + p.Body)
	return c
}

// contentHash is the optimistic-concurrency token carried by the edit form.
func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
