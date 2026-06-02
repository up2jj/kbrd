package model

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

const pinPrefix = "p_"

// ItemOptions bundles the per-board display settings that influence how an item
// is loaded and labelled. It is owned by the model layer so Item stays
// decoupled from config.Config; buildColumn maps the config into it.
type ItemOptions struct {
	PreviewLines     int
	TitleFromHeading bool
}

type Item struct {
	Name     string
	Title    string
	Preview  []string
	FullPath string
	Pinned   bool
	Size     int64
	Modified time.Time
}

func NewItem(fullPath string, opts ItemOptions) (Item, error) {
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

	preview, heading, _ := readPreviewAndHeading(fullPath, opts)

	return Item{
		Name:     name,
		Title:    resolveTitle(name, heading, opts),
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

func (i *Item) Refresh(opts ItemOptions) error {
	info, err := os.Stat(i.FullPath)
	if err != nil {
		return err
	}

	i.Size = info.Size()
	i.Modified = info.ModTime()

	if preview, heading, err := readPreviewAndHeading(i.FullPath, opts); err == nil {
		i.Preview = preview
		i.Title = resolveTitle(i.Name, heading, opts)
	}
	return nil
}

// resolveTitle returns the display title: the heading when heading titles are
// enabled and one was found, otherwise the filename-derived name.
func resolveTitle(name, heading string, opts ItemOptions) string {
	if opts.TitleFromHeading && heading != "" {
		return heading
	}
	return name
}

// reH1 matches an ATX level-1 heading: a single leading '#' followed by space
// and the title text. Levels 2+ (## …) and '#' without a space are ignored.
var reH1 = regexp.MustCompile(`^#[ \t]+(.+?)[ \t]*#*$`)

// readPreviewAndHeading scans only the leading prefix of the file in a single
// pass, returning the non-empty preview lines (first opts.PreviewLines of them)
// and, when opts.TitleFromHeading is set, the first level-1 markdown heading.
//
// The heading must be the file's first non-blank content line (an optional
// leading YAML frontmatter block delimited by '---' is skipped first); if the
// first content line is not an H1 there is no title. The heading line, when
// used, is omitted from the preview so it is not shown twice. The read never
// touches the file's tail — cost stays bounded regardless of file size — and
// the 64 KB scanner line cap is raised so pathological single-line files
// degrade gracefully (matching the old whole-file ReadFile behaviour).
func readPreviewAndHeading(path string, opts ItemOptions) ([]string, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	heading := ""
	prologueDone := !opts.TitleFromHeading // once true, every line counts against the preview
	inFrontmatter := false

	preview := []string{}
	budget := opts.PreviewLines
	for {
		if !sc.Scan() {
			break
		}
		line := sc.Text()

		// Prologue phase (heading mode only): consume an optional leading YAML
		// frontmatter block, any leading blank lines, and the title line itself
		// without spending the preview budget, so the preview shows body text.
		if !prologueDone {
			trimmed := strings.TrimSpace(line)
			if inFrontmatter {
				if trimmed == "---" || trimmed == "..." {
					inFrontmatter = false
				}
				continue
			}
			if len(preview) == 0 && trimmed == "---" {
				inFrontmatter = true
				continue
			}
			if trimmed == "" {
				continue // leading blanks before the first content line
			}
			prologueDone = true
			if m := reH1.FindStringSubmatch(trimmed); m != nil {
				heading = m[1]
				continue // omit the title line from the preview
			}
			// First content line is not an H1: no title; it belongs to preview.
		}

		if budget <= 0 {
			break
		}
		budget--
		if line != "" {
			preview = append(preview, line)
		}
	}
	return preview, heading, nil
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
