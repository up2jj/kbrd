// Package capture prepares content received from external capture surfaces for
// interactive review. It converts rich HTML to Markdown but does not resolve a
// board or write cards; package ingest owns persistence and metadata.
package capture

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// MaxInputBytes bounds data accepted from an external application.
const MaxInputBytes = 4 << 20

// Input is the richest set of representations supplied by a capture adapter.
type Input struct {
	Title     string `json:"title,omitempty"`
	HTML      string `json:"html,omitempty"`
	PlainText string `json:"plain_text,omitempty"`
	URL       string `json:"url,omitempty"`
	SourceApp string `json:"source_app,omitempty"`
}

// Prepared is ready to prefill an interactive capture form.
type Prepared struct {
	Title     string `json:"title"`
	Markdown  string `json:"markdown"`
	URL       string `json:"url,omitempty"`
	SourceApp string `json:"source_app,omitempty"`
}

// Prepare validates and normalizes a capture. HTML is preferred over plain
// text; a URL becomes a Markdown link only when no textual content exists.
func Prepare(input Input) (Prepared, error) {
	if len(input.Title)+len(input.HTML)+len(input.PlainText)+len(input.URL)+len(input.SourceApp) > MaxInputBytes {
		return Prepared{}, fmt.Errorf("capture exceeds %d bytes", MaxInputBytes)
	}
	input.URL = strings.TrimSpace(input.URL)
	markdown := ""
	if strings.TrimSpace(input.HTML) != "" {
		var err error
		markdown, err = HTMLToMarkdown(input.HTML)
		if err != nil {
			return Prepared{}, fmt.Errorf("convert HTML: %w", err)
		}
	}
	if strings.TrimSpace(markdown) == "" {
		markdown = normalizePlainText(input.PlainText)
	}

	title := singleLine(input.Title)
	if title == "" {
		title = titleFromMarkdown(markdown)
	}
	if title == "" {
		title = titleFromURL(input.URL)
	}
	if title == "" {
		title = "Captured item"
	}
	if markdown == "" && input.URL != "" {
		if title == input.URL {
			markdown = "<" + input.URL + ">"
		} else {
			markdown = "[" + escapeLabel(title) + "](" + input.URL + ")"
		}
	}
	if markdown == "" {
		return Prepared{}, fmt.Errorf("capture has no text, HTML, or URL")
	}

	return Prepared{
		Title: title, Markdown: markdown, URL: input.URL,
		SourceApp: singleLine(input.SourceApp),
	}, nil
}

func normalizePlainText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimSpace(text)
}

func singleLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func titleFromMarkdown(markdown string) string {
	for line := range strings.SplitSeq(markdown, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimLeft(line, "#>*-+ `~")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > 100 {
			runes := []rune(line)
			line = string(runes[:100]) + "…"
		}
		return line
	}
	return ""
}

func titleFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return raw
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `[`, `\[`)
	return strings.ReplaceAll(s, `]`, `\]`)
}

var (
	spaceRE      = regexp.MustCompile(`[\t\n\f\r ]+`)
	blankLinesRE = regexp.MustCompile(`\n[ \t]*\n(?:[ \t]*\n)+`)
)

// HTMLToMarkdown converts common semantic HTML into portable Markdown. It is
// intentionally local and deterministic: URLs and images remain references;
// no network resources are fetched.
func HTMLToMarkdown(input string) (string, error) {
	doc, err := html.Parse(strings.NewReader(input))
	if err != nil {
		return "", err
	}
	r := htmlRenderer{}
	out := r.renderChildren(doc)
	out = strings.ReplaceAll(out, "\u00a0", " ")
	lines := strings.Split(out, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	out = strings.Join(lines, "\n")
	out = blankLinesRE.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out), nil
}

type htmlRenderer struct {
	listDepth int
}

func (r *htmlRenderer) renderChildren(n *html.Node) string {
	var b strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(r.render(child))
	}
	return b.String()
}

func (r *htmlRenderer) render(n *html.Node) string {
	if n.Type == html.TextNode {
		if parentElement(n, "pre") {
			return n.Data
		}
		return spaceRE.ReplaceAllString(n.Data, " ")
	}
	if n.Type != html.ElementNode && n.Type != html.DocumentNode {
		return ""
	}
	tag := strings.ToLower(n.Data)
	switch tag {
	case "script", "style", "noscript", "template", "svg", "head", "meta", "link", "title":
		return ""
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level := int(tag[1] - '0')
		return "\n\n" + strings.Repeat("#", level) + " " + strings.TrimSpace(r.renderChildren(n)) + "\n\n"
	case "p", "div", "section", "article", "header", "footer", "main", "aside":
		content := strings.TrimSpace(r.renderChildren(n))
		if content == "" {
			return ""
		}
		return "\n\n" + content + "\n\n"
	case "br":
		return "\n"
	case "strong", "b":
		return wrapInline("**", r.renderChildren(n))
	case "em", "i":
		return wrapInline("*", r.renderChildren(n))
	case "del", "s", "strike":
		return wrapInline("~~", r.renderChildren(n))
	case "code":
		if parentElement(n, "pre") {
			return rawText(n)
		}
		content := strings.TrimSpace(r.renderChildren(n))
		if content == "" {
			return ""
		}
		fence := "`"
		if strings.Contains(content, "`") {
			fence = "``"
		}
		return fence + content + fence
	case "pre":
		content := strings.Trim(rawText(n), "\n")
		return "\n\n```\n" + content + "\n```\n\n"
	case "a":
		label := strings.TrimSpace(r.renderChildren(n))
		href := attr(n, "href")
		if href == "" {
			return label
		}
		if label == "" {
			label = href
		}
		return "[" + label + "](" + href + ")"
	case "img":
		src := attr(n, "src")
		if src == "" {
			return ""
		}
		return "![" + escapeLabel(attr(n, "alt")) + "](" + src + ")"
	case "blockquote":
		content := strings.TrimSpace(r.renderChildren(n))
		if content == "" {
			return ""
		}
		lines := strings.Split(content, "\n")
		for i := range lines {
			lines[i] = "> " + lines[i]
		}
		return "\n\n" + strings.Join(lines, "\n") + "\n\n"
	case "ul":
		return r.renderList(n, false)
	case "ol":
		return r.renderList(n, true)
	case "li":
		return strings.TrimSpace(r.renderChildren(n))
	case "hr":
		return "\n\n---\n\n"
	case "table":
		if table := r.renderTable(n); table != "" {
			return "\n\n" + table + "\n\n"
		}
	}
	return r.renderChildren(n)
}

func (r *htmlRenderer) renderList(n *html.Node, ordered bool) string {
	r.listDepth++
	defer func() { r.listDepth-- }()
	var lines []string
	index := 1
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || child.Data != "li" {
			continue
		}
		var inline strings.Builder
		var nested strings.Builder
		for part := child.FirstChild; part != nil; part = part.NextSibling {
			if part.Type == html.ElementNode && (part.Data == "ul" || part.Data == "ol") {
				nested.WriteString(r.render(part))
			} else {
				inline.WriteString(r.render(part))
			}
		}
		marker := "- "
		if ordered {
			marker = fmt.Sprintf("%d. ", index)
			index++
		}
		indent := strings.Repeat("  ", r.listDepth-1)
		line := indent + marker + strings.TrimSpace(inline.String())
		if nested.Len() > 0 {
			line += "\n" + strings.Trim(nested.String(), "\n")
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(lines, "\n") + "\n"
}

func (r *htmlRenderer) renderTable(table *html.Node) string {
	var rows [][]string
	var header bool
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			var cells []string
			rowHeader := false
			for cell := n.FirstChild; cell != nil; cell = cell.NextSibling {
				if cell.Type != html.ElementNode || (cell.Data != "td" && cell.Data != "th") {
					continue
				}
				rowHeader = rowHeader || cell.Data == "th"
				value := strings.ReplaceAll(strings.TrimSpace(r.renderChildren(cell)), "|", `\|`)
				cells = append(cells, value)
			}
			if len(cells) > 0 {
				if len(rows) == 0 {
					header = rowHeader
				}
				rows = append(rows, cells)
			}
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(table)
	if len(rows) == 0 {
		return ""
	}
	width := 0
	for _, row := range rows {
		width = max(width, len(row))
	}
	if !header {
		rows = append([][]string{make([]string, width)}, rows...)
	}
	var lines []string
	for i, row := range rows {
		row = append(row, make([]string, width-len(row))...)
		lines = append(lines, "| "+strings.Join(row, " | ")+" |")
		if i == 0 {
			lines = append(lines, "| "+strings.TrimSuffix(strings.Repeat("--- | ", width), " ")+"")
		}
	}
	return strings.Join(lines, "\n")
}

func wrapInline(marker, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	return marker + content + marker
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

func parentElement(n *html.Node, tag string) bool {
	for parent := n.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == html.ElementNode {
			return parent.Data == tag
		}
	}
	return false
}

func rawText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return b.String()
}
