package board

import (
	"fmt"
	"os"
	"path/filepath"

	"kbrd/frontmatter"
)

// ScannedItem is a complete filesystem-backed board item. It is intended for
// headless features that need to inspect cards across every real column.
type ScannedItem struct {
	Path        string
	Column      string
	Name        string
	Raw         string
	Body        string
	Frontmatter frontmatter.Parsed
}

// ScanItems reads items from every filesystem column and returns those
// accepted by filter. A nil filter accepts every item. Unlike presentation
// loaders, scanning is strict: read and frontmatter errors stop the operation
// so a mutating caller cannot silently act on an incomplete view of the board.
func ScanItems(boardPath string, filter func(ScannedItem) bool) ([]ScannedItem, error) {
	columns, err := Columns(boardPath)
	if err != nil {
		return nil, fmt.Errorf("list board columns: %w", err)
	}

	var scanned []ScannedItem
	for _, column := range columns {
		columnPath := filepath.Join(boardPath, column)
		items, err := Items(columnPath)
		if err != nil {
			return nil, fmt.Errorf("list column %s: %w", column, err)
		}
		for _, name := range items {
			path := filepath.Join(columnPath, name+".md")
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read item %s: %w", path, err)
			}
			raw := string(data)
			front, body, _ := frontmatter.Split(raw)
			parsed, err := frontmatter.Parse([]byte(front))
			if err != nil {
				return nil, fmt.Errorf("parse frontmatter %s: %w", path, err)
			}
			item := ScannedItem{
				Path: path, Column: column, Name: name, Raw: raw,
				Body: body, Frontmatter: parsed,
			}
			if filter == nil || filter(item) {
				scanned = append(scanned, item)
			}
		}
	}
	return scanned, nil
}
