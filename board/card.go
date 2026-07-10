package board

import (
	"regexp"
	"strings"

	"kbrd/frontmatter"
)

// CardProjectionOptions controls frontend presentation policy without coupling
// a frontend to the filesystem parser.
type CardProjectionOptions struct {
	PreviewLines     int
	TitleFromHeading bool
}

// CardProjection is the shared, filesystem-backed card presentation. Each
// frontend can add its own state around it (selection, git badges, virtual
// cards), but title/frontmatter/preview parsing stays consistent.
type CardProjection struct {
	Name    string
	Title   string
	Preview []string
	Pinned  bool
	Tags    []string
	Render  []string
	Meta    string
	Icon    string
	Accent  string
	Data    map[string]any
	BadFM   bool
	Body    string // populated by ProjectCardContent for full-body web search
}

// HeadingTitle recognizes an H1 title in the same form understood by the TUI.
func HeadingTitle(line string) (string, bool) {
	m := h1Heading.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return "", false
	}
	return m[1], true
}

var h1Heading = regexp.MustCompile(`^#[ \t]+(.+?)[ \t]*#*$`)

// ProjectCardParts builds a presentation from the leading data already scanned
// by a frontend. It lets the TUI retain its bounded streaming reader while
// sharing frontmatter and title policy with the web frontend.
func ProjectCardParts(name, heading string, preview []string, front []byte, opts CardProjectionOptions) CardProjection {
	fm, err := frontmatter.Parse(front)
	title := name
	if opts.TitleFromHeading && heading != "" {
		title = heading
	}
	return CardProjection{
		Name: name, Title: title, Preview: preview,
		Pinned: frontmatter.Bool(fm.Data["pinned"]),
		Tags:   fm.Tags, Render: fm.Render, Meta: fm.Meta, Icon: fm.Icon, Accent: fm.Accent,
		Data: fm.Data, BadFM: err != nil,
	}
}

// ProjectCardContent parses a complete item body for frontends that already
// need the raw content (the web quick filter). It follows the same title and
// preview rules as ProjectCardParts.
func ProjectCardContent(name, raw string, opts CardProjectionOptions) CardProjection {
	front, body, _ := frontmatter.Split(raw)
	preview := make([]string, 0, max(opts.PreviewLines, 0))
	heading := ""
	headingDone := !opts.TitleFromHeading
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !headingDone {
			if trimmed == "" {
				continue
			}
			headingDone = true
			if title, ok := HeadingTitle(trimmed); ok {
				heading = title
				continue
			}
		}
		if len(preview) < opts.PreviewLines && line != "" {
			preview = append(preview, line)
		}
	}
	card := ProjectCardParts(name, heading, preview, []byte(front), opts)
	card.Body = body
	return card
}
