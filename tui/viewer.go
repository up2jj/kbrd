package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"kbrd/theme"
)

type ViewerOptions struct {
	Title       string
	Content     string
	Format      string
	Wrap        bool
	LineNumbers bool
	Actions     []Action
}

type ViewerResult struct {
	Action    string
	Submitted bool
	Cancelled bool
}

// Viewer presents a read-only, scrollable document with optional actions.
type Viewer struct {
	opts    ViewerOptions
	lines   []viewerLine
	offset  int
	left    int
	result  *ViewerResult
	active  bool
	size    Size
	palette theme.Palette
	status  string
}

type viewerLine struct {
	text   string
	source int
}

func (v *Viewer) Open(opts ViewerOptions) {
	v.opts = opts
	v.result = nil
	v.active = true
	v.offset = 0
	v.left = 0
	v.status = ""
	v.reflow()
}

func (v *Viewer) Active() bool { return v.active }

func (v *Viewer) SetSize(width, height int) {
	v.size.Set(width, height)
	v.reflow()
	v.clampOffset()
	v.clampLeft()
}

func (v *Viewer) SetPalette(p theme.Palette) { v.palette = p }

func (v *Viewer) Update(msg tea.Msg) tea.Cmd {
	if !v.active {
		return nil
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		switch mouse.Mouse().Button {
		case tea.MouseWheelUp:
			v.scroll(-3)
		case tea.MouseWheelDown:
			v.scroll(3)
		}
		return nil
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	raw := keyMsg.String()
	pressed := strings.ToLower(raw)
	if pressed == "esc" {
		v.finish(ViewerResult{Cancelled: true})
		return nil
	}
	for _, action := range v.opts.Actions {
		if action.Key == "" || !strings.EqualFold(action.Key, pressed) {
			continue
		}
		if action.Disabled {
			v.status = action.DisabledReason
			if v.status == "" {
				v.status = "This action is disabled"
			}
			return nil
		}
		v.finish(ViewerResult{Action: action.ID, Submitted: true})
		return nil
	}
	switch {
	case raw == "G":
		v.offset = v.maxOffset()
	case !v.opts.Wrap && (pressed == "left" || pressed == "h"):
		v.scrollHorizontal(-1)
	case !v.opts.Wrap && (pressed == "right" || pressed == "l"):
		v.scrollHorizontal(1)
	case pressed == "up" || pressed == "k":
		v.scroll(-1)
	case pressed == "down" || pressed == "j":
		v.scroll(1)
	case pressed == "pgup":
		v.scroll(-v.pageSize())
	case pressed == "pgdown" || pressed == "space":
		v.scroll(v.pageSize())
	case pressed == "home" || pressed == "g":
		v.offset = 0
	case pressed == "end":
		v.offset = v.maxOffset()
	}
	return nil
}

func (v *Viewer) View() string {
	if !v.active {
		return ""
	}
	pageSize := v.pageSize()
	end := min(v.offset+pageSize, len(v.lines))
	rows := make([]string, 0, pageSize)
	for _, line := range v.lines[v.offset:end] {
		visible := v.visibleText(line.text)
		text := visible
		if v.opts.LineNumbers {
			number := ""
			if line.source > 0 {
				number = strconv.Itoa(line.source)
			}
			gutter := fmt.Sprintf("%*s ", v.gutterWidth()-1, number)
			text = lipgloss.NewStyle().Foreground(v.palette.FgDim).Render(gutter) + v.styleLine(line.text, visible)
		} else {
			text = v.styleLine(line.text, visible)
		}
		rows = append(rows, text)
	}
	blank := strings.Repeat(" ", max(v.contentWidth(), 1))
	for len(rows) < pageSize {
		rows = append(rows, blank)
	}
	body := strings.Join(rows, "\n")
	hints := []theme.Hint{{Keys: "j/k", Label: "scroll"}, {Keys: "g/G", Label: "top/bottom"}}
	if !v.opts.Wrap {
		hints = append(hints, theme.Hint{Keys: "h/l", Label: "pan"})
	}
	for _, action := range v.opts.Actions {
		hints = append(hints, theme.Hint{Keys: action.Key, Label: action.Label})
	}
	hints = append(hints, theme.Hint{Keys: "esc", Label: "cancel"})
	page := fmt.Sprintf("%d/%d", min(v.offset/pageSize+1, v.pageCount()), v.pageCount())
	footer := theme.RenderHints(v.palette, hints) + "  " + lipgloss.NewStyle().Foreground(v.palette.FgMuted).Render(page)
	if v.status != "" {
		body += "\n" + lipgloss.NewStyle().Foreground(v.palette.Warning).Render(v.status)
	}
	title := v.opts.Title
	if title == "" {
		title = "Viewer"
	}
	return theme.OverlayFrame{Title: title, Body: body, Footer: footer, Palette: v.palette, Width: v.frameWidth()}.Render()
}

func (v *Viewer) TakeResult() (ViewerResult, bool) {
	if v.result == nil {
		return ViewerResult{}, false
	}
	result := *v.result
	v.result = nil
	return result, true
}

func (v *Viewer) Close() {
	v.active = false
	v.result = nil
	v.lines = nil
	v.opts = ViewerOptions{}
	v.status = ""
	v.left = 0
}

func (v *Viewer) Offset() int { return v.offset }

func (v *Viewer) finish(result ViewerResult) {
	v.result = &result
	v.active = false
}

func (v *Viewer) scroll(delta int) {
	v.offset = min(max(v.offset+delta, 0), v.maxOffset())
}

func (v *Viewer) clampOffset() { v.offset = min(max(v.offset, 0), v.maxOffset()) }

func (v *Viewer) scrollHorizontal(delta int) {
	v.left = min(max(v.left+delta, 0), v.maxLeft())
}

func (v *Viewer) clampLeft() { v.left = min(max(v.left, 0), v.maxLeft()) }

func (v *Viewer) pageSize() int { return max(v.size.Height-9, 3) }

func (v *Viewer) maxOffset() int { return max(len(v.lines)-v.pageSize(), 0) }

func (v *Viewer) pageCount() int { return max((len(v.lines)+v.pageSize()-1)/v.pageSize(), 1) }

func (v *Viewer) frameWidth() int { return max(v.size.Fit(110, 0).Width-2, 20) }

func (v *Viewer) contentWidth() int { return max(v.frameWidth()-8-v.gutterWidth(), 1) }

func (v *Viewer) gutterWidth() int {
	if !v.opts.LineNumbers {
		return 0
	}
	content := formattedViewerContent(v.opts.Content, v.opts.Format)
	return max(len(strconv.Itoa(sourceLineCount(content)))+1, 3)
}

func (v *Viewer) reflow() {
	content := formattedViewerContent(v.opts.Content, v.opts.Format)
	source := strings.Split(content, "\n")
	v.lines = v.lines[:0]
	width := v.contentWidth()
	for index, line := range source {
		parts := []string{line}
		if v.opts.Wrap {
			parts = wrapRunes(line, width)
		}
		for partIndex, part := range parts {
			sourceLine := 0
			if partIndex == 0 {
				sourceLine = index + 1
			}
			v.lines = append(v.lines, viewerLine{text: part, source: sourceLine})
		}
	}
	if len(v.lines) == 0 {
		v.lines = []viewerLine{{source: 1}}
	}
}

func (v *Viewer) maxLeft() int {
	longest := 0
	for _, line := range v.lines {
		longest = max(longest, utf8.RuneCountInString(line.text))
	}
	return max(longest-v.contentWidth(), 0)
}

func (v *Viewer) visibleText(line string) string {
	if v.opts.Wrap {
		return line
	}
	runes := []rune(line)
	start := min(v.left, len(runes))
	end := min(start+v.contentWidth(), len(runes))
	return string(runes[start:end])
}

func (v *Viewer) styleLine(line, visible string) string {
	style := lipgloss.NewStyle().Foreground(v.palette.FgBase)
	switch v.opts.Format {
	case "markdown":
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			style = style.Foreground(v.palette.Primary).Bold(true)
		} else if strings.HasPrefix(trimmed, ">") {
			style = style.Foreground(v.palette.FgMuted).Italic(true)
		} else if strings.HasPrefix(trimmed, "```") {
			style = style.Foreground(v.palette.AccentAlt)
		}
	case "diff":
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			style = style.Foreground(v.palette.Success)
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			style = style.Foreground(v.palette.Danger)
		case strings.HasPrefix(line, "@@"):
			style = style.Foreground(v.palette.Primary).Bold(true)
		}
	case "json", "yaml":
		if strings.Contains(line, ":") {
			style = style.Foreground(v.palette.AccentAlt)
		}
	case "log":
		upper := strings.ToUpper(line)
		switch {
		case strings.Contains(upper, "ERROR") || strings.Contains(upper, "FATAL"):
			style = style.Foreground(v.palette.Danger)
		case strings.Contains(upper, "WARN"):
			style = style.Foreground(v.palette.Warning)
		case strings.Contains(upper, "DEBUG"):
			style = style.Foreground(v.palette.FgMuted)
		}
	}
	return style.Render(visible)
}

func formattedViewerContent(content, format string) string {
	if format != "json" || strings.TrimSpace(content) == "" {
		return content
	}
	var out bytes.Buffer
	if json.Indent(&out, []byte(content), "", "  ") == nil {
		return out.String()
	}
	return content
}

func wrapRunes(line string, width int) []string {
	runes := []rune(line)
	if len(runes) == 0 {
		return []string{""}
	}
	out := make([]string, 0, (len(runes)+width-1)/width)
	for len(runes) > 0 {
		n := min(width, len(runes))
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}

func sourceLineCount(content string) int { return strings.Count(content, "\n") + 1 }
