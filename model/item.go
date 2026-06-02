package model

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

const pinPrefix = "p_"

type Item struct {
	Name     string
	Preview  []string
	FullPath string
	Pinned   bool
	Size     int64
	Modified time.Time
}

func NewItem(fullPath string, previewLines int) (Item, error) {
	info, err := os.Stat(fullPath)
	if err != nil {
		return Item{}, err
	}

	name := info.Name()
	name = strings.TrimSuffix(name, ".md")
	pinned := strings.HasPrefix(name, pinPrefix)
	if pinned {
		name = strings.TrimPrefix(name, pinPrefix)
	}

	preview, _ := readPreview(fullPath, previewLines)

	return Item{
		Name:     name,
		Preview:  preview,
		FullPath: fullPath,
		Pinned:   pinned,
		Size:     info.Size(),
		Modified: info.ModTime(),
	}, nil
}

// itemCache holds previously loaded items keyed by absolute path. A reload
// consults it via reuse so files whose size and mtime are unchanged skip the
// re-read entirely (see Column.loadItems).
type itemCache map[string]Item

// reuse returns the cached item for path when its size and mtime match info,
// reporting false when the file is absent from the cache or has changed and so
// must be re-read. The (mtime, size) key is the standard make/rsync heuristic:
// any write bumps mtime, so a stale hit would need a same-size, same-mtime edit,
// which writes do not produce.
func (c itemCache) reuse(path string, info os.FileInfo) (Item, bool) {
	old, ok := c[path]
	if !ok || old.Size != info.Size() || !old.Modified.Equal(info.ModTime()) {
		return Item{}, false
	}
	return old, true
}

func (i Item) FilterValue() string { return i.Name }

func (i Item) HumanSize() string {
	if i.Size < 1024 {
		return fmt.Sprintf("%d B", i.Size)
	}
	return fmt.Sprintf("%.1f KB", float64(i.Size)/1024)
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		if t.Year() == time.Now().Year() {
			return t.Format("Jan 2")
		}
		return t.Format("Jan 2 2006")
	}
}

func (i *Item) TogglePin() {
	i.Pinned = !i.Pinned
	if i.Pinned {
		i.Name = pinPrefix + i.Name
	} else {
		i.Name = strings.TrimPrefix(i.Name, pinPrefix)
	}
}

func (i *Item) Refresh(previewLines int) error {
	info, err := os.Stat(i.FullPath)
	if err != nil {
		return err
	}

	i.Size = info.Size()
	i.Modified = info.ModTime()

	if preview, err := readPreview(i.FullPath, previewLines); err == nil {
		i.Preview = preview
	}
	return nil
}

// readPreview returns the non-empty lines among the first previewLines lines of
// the file. It reads only that prefix — never the file's tail — so preview cost
// is bounded regardless of file size. The default 64 KB scanner line cap is
// raised so a pathological single-line file degrades gracefully instead of
// erroring (matching the old whole-file ReadFile behaviour).
func readPreview(path string, previewLines int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	preview := []string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for i := 0; i < previewLines && sc.Scan(); i++ {
		if line := sc.Text(); line != "" {
			preview = append(preview, line)
		}
	}
	return preview, nil
}

func SortItems(items []Item) []Item {
	result := make([]Item, len(items))
	copy(result, items)
	pinned := []Item{}
	unpinned := []Item{}
	for _, item := range result {
		if item.Pinned {
			pinned = append(pinned, item)
		} else {
			unpinned = append(unpinned, item)
		}
	}
	unpinned = sortByName(unpinned)
	return append(pinned, unpinned...)
}

func sortByName(items []Item) []Item {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Name < items[j-1].Name; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	return items
}
