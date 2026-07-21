package model

import (
	"bufio"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"strings"
	"time"

	"kbrd/board"
	"kbrd/frontmatter"
)

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
	// contentHash is an FNV-64a hash of the file's full bytes, used by the
	// watcher path to tell a real content change from a metadata-only touch
	// (mtime bump with identical bytes). Lets a post-save rewriting hook bound
	// to item_changed converge instead of looping. Empty for virtual items.
	contentHash uint64
	Tags        []string // frontmatter `tags`; shown as #tag chips, matched by the filter
	Render      []string // frontmatter `render`: keys surfaced on the card as key: value
	BadFM       bool     // frontmatter block present but not valid YAML; card shows a ⚠ badge

	// Presentation/payload fields shared by both item kinds: scripts set them
	// via kbrd.column.set, filesystem items populate them from YAML frontmatter.
	// Virtual/Separator stay script-only; for virtual items Name doubles as the
	// stable cursor key (set from the item id, else title).
	Virtual   bool
	Separator bool           // inert grouping row — no actions, no mnemonic
	Meta      string         // replaces the filesystem meta line (line 3)
	Icon      string         // optional glyph prefixed on line 1
	Accent    string         // color key/name for the title/icon
	Data      map[string]any // opaque payload, round-trips into command ctx
}

func NewItem(fullPath string, opts ItemOptions) (Item, error) {
	info, err := os.Stat(fullPath)
	if err != nil {
		return Item{}, err
	}

	name := strings.TrimSuffix(info.Name(), ".md")

	preview, heading, front, hash, _ := readPreviewAndHeading(fullPath, opts)
	card := board.ProjectCardParts(name, heading, preview, front, board.CardProjectionOptions{
		PreviewLines: opts.PreviewLines, TitleFromHeading: opts.TitleFromHeading,
	})

	return Item{
		BadFM:       card.BadFM,
		Name:        card.Name,
		Title:       card.Title,
		Preview:     card.Preview,
		FullPath:    fullPath,
		Pinned:      card.Pinned,
		Size:        info.Size(),
		Modified:    info.ModTime(),
		contentHash: hash,
		Tags:        card.Tags,
		Render:      card.Render,
		Meta:        card.Meta,
		Icon:        card.Icon,
		Accent:      card.Accent,
		Data:        card.Data,
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

func (i Item) FilterValue() string {
	if i.Separator {
		return "" // an empty filter value never matches a query → filtered out
	}
	if i.Virtual {
		return i.Title
	}
	if len(i.Tags) > 0 {
		return i.Name + " " + strings.Join(i.Tags, " ")
	}
	return i.Name
}

func (i Item) HumanSize() string {
	if i.Size < 1024 {
		return fmt.Sprintf("%d B", i.Size)
	}
	return fmt.Sprintf("%.1f KB", float64(i.Size)/1024)
}

// TimeAgo formats a timestamp as a compact, human-readable age for TUI views.
func TimeAgo(t time.Time) string {
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

func (i *Item) Refresh(opts ItemOptions) error {
	info, err := os.Stat(i.FullPath)
	if err != nil {
		return err
	}

	i.Size = info.Size()
	i.Modified = info.ModTime()

	if preview, heading, front, hash, err := readPreviewAndHeading(i.FullPath, opts); err == nil {
		card := board.ProjectCardParts(i.Name, heading, preview, front, board.CardProjectionOptions{
			PreviewLines: opts.PreviewLines, TitleFromHeading: opts.TitleFromHeading,
		})
		i.Preview = card.Preview
		i.contentHash = hash
		// Assign unconditionally so removing the frontmatter clears the fields.
		i.Title = card.Title
		i.BadFM = card.BadFM
		i.Pinned = card.Pinned
		i.Tags = card.Tags
		i.Render = card.Render
		i.Meta = card.Meta
		i.Icon = card.Icon
		i.Accent = card.Accent
		i.Data = card.Data
	}
	return nil
}

// readPreviewAndHeading scans only the leading prefix of the file in a single
// pass, returning the non-empty preview lines (first opts.PreviewLines of
// them), the first level-1 markdown heading (when opts.TitleFromHeading is
// set), and the raw bytes of an optional leading YAML frontmatter block.
//
// The frontmatter block ('---' fences, closed by '---' or '...') is captured
// regardless of opts and never reaches the preview; the returned bytes exclude
// the fences and are capped at frontmatter.MaxBytes (the file is still
// consumed to the closing fence so the preview stays correct). The heading
// must be the first non-blank content line after the frontmatter; if it is
// not an H1 there is no title. The heading line, when used, is omitted from
// the preview so it is not shown twice. The 64 KB scanner line cap is raised so
// pathological single-line files degrade gracefully (matching the old
// whole-file ReadFile behaviour).
//
// The returned uint64 is an FNV-64a hash of the file's full bytes. Parsing
// still only consumes the leading prefix, but the bytes are teed through the
// hash and the tail is drained after, so the full file is read once — the
// watcher path uses the hash to suppress no-op (mtime-only) change events.
func readPreviewAndHeading(path string, opts ItemOptions) ([]string, string, []byte, uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", nil, 0, err
	}
	defer f.Close()

	// Tee the file through a hash so a single pass yields both the preview
	// prefix and a full-content hash. The scanner reads ahead in chunks, so
	// every byte it pulls is already teed; draining the remainder after the
	// preview loop completes the hash over the whole file.
	h := fnv.New64a()
	sc := bufio.NewScanner(io.TeeReader(f, h))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	heading := ""
	headingDone := !opts.TitleFromHeading // once true, every line counts against the preview
	var front []byte
	frontPossible := true // until the first non-blank line settles the question
	inFrontmatter := false

	preview := []string{}
	budget := opts.PreviewLines
	for {
		if !sc.Scan() {
			break
		}
		line := sc.Text()
		trimmed := strings.TrimSpace(line)

		// Frontmatter phase — independent of TitleFromHeading: capture an
		// optional leading '---' block so it never leaks into the preview.
		if inFrontmatter {
			if trimmed == "---" || trimmed == "..." {
				inFrontmatter = false
			} else if len(front) < frontmatter.MaxBytes {
				front = append(front, line...)
				front = append(front, '\n')
			}
			continue
		}
		if frontPossible && trimmed != "" {
			frontPossible = false
			if trimmed == "---" {
				inFrontmatter = true
				continue
			}
			// Leading blanks fall through: in heading mode the heading phase
			// consumes them for free; otherwise they spend preview budget,
			// matching the pre-frontmatter behaviour.
		}

		// Heading phase (heading mode only): consume any blank lines and the
		// title line itself without spending the preview budget, so the
		// preview shows body text.
		if !headingDone {
			if trimmed == "" {
				continue
			}
			headingDone = true
			if title, ok := board.HeadingTitle(trimmed); ok {
				heading = title
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
	// Drain whatever the scanner did not pull (it stops at the preview budget,
	// not EOF) so the hash covers the file's tail too. TeeReader has already
	// fed the head — including the scanner's read-ahead — into h.
	_, _ = io.Copy(h, f)
	return preview, heading, front, h.Sum64(), nil
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
