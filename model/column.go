package model

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// itemDelegate renders each kanban item inside a bubbles list.
type itemDelegate struct {
	isActive   bool
	mnemonicOf func(name string) string
	gutterW    int
	colWidth   int
}

func (d itemDelegate) Height() int  { return 3 }
func (d itemDelegate) Spacing() int { return 1 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(Item)
	if !ok {
		return
	}
	isSelected := index == m.Index()

	gutterW := d.gutterW
	if gutterW < 2 {
		gutterW = 2
	}
	mnemonic := ""
	if d.mnemonicOf != nil {
		mnemonic = d.mnemonicOf(item.Name)
	}

	// Row palette — every cell on the row shares the same background so the
	// mnemonic cell visually belongs to the row.
	var rowBg, mnemFg, nameFg lipgloss.Color
	hasRowBg := false
	switch {
	case isSelected && d.isActive:
		rowBg = lipgloss.Color("#3b82f6")
		mnemFg = lipgloss.Color("#fde047")
		nameFg = lipgloss.Color("#ffffff")
		hasRowBg = true
	case isSelected:
		mnemFg = lipgloss.Color("#f59e0b")
		nameFg = lipgloss.Color("#f1f5f9")
	default:
		mnemFg = lipgloss.Color("#f59e0b")
		nameFg = lipgloss.Color("#f1f5f9")
	}

	pinIcon := ""
	if item.Pinned {
		pinIcon = "📌 "
	}

	// Build line 1 as gutter + rest, each rendered with the same row background
	// so they fuse into one continuous bar.
	gutterStyle := lipgloss.NewStyle().Bold(true).Foreground(mnemFg).Width(gutterW)
	restWidth := d.colWidth - gutterW
	if restWidth < 1 {
		restWidth = 1
	}
	restStyle := lipgloss.NewStyle().Bold(true).Foreground(nameFg).Width(restWidth).MaxWidth(restWidth)
	if hasRowBg {
		gutterStyle = gutterStyle.Background(rowBg)
		restStyle = restStyle.Background(rowBg)
	}

	gutterText := mnemonic
	if gutterText == "" {
		if isSelected && d.isActive {
			gutterText = ">"
		}
	}
	fmt.Fprintln(w, gutterStyle.Render(gutterText)+restStyle.Render(pinIcon+item.Name))

	// Line 2 — preview
	preview := "—"
	if len(item.Preview) > 0 {
		preview = item.Preview[0]
	}
	var previewFg, detailBg lipgloss.Color
	switch {
	case isSelected && d.isActive:
		previewFg = lipgloss.Color("#bfdbfe")
		detailBg = lipgloss.Color("#1e3a8a")
	case isSelected:
		previewFg = lipgloss.Color("#94a3b8")
	default:
		previewFg = lipgloss.Color("#64748b")
	}
	previewStyle := lipgloss.NewStyle().Width(d.colWidth).MaxWidth(d.colWidth).PaddingLeft(gutterW).Foreground(previewFg).Italic(true)
	if isSelected && d.isActive {
		previewStyle = previewStyle.Background(detailBg)
	}
	fmt.Fprintln(w, previewStyle.Render(preview))

	// Line 3 — meta (modified + size)
	meta := timeAgo(item.Modified) + "  ·  " + item.HumanSize()
	var metaFg lipgloss.Color
	switch {
	case isSelected && d.isActive:
		metaFg = lipgloss.Color("#93c5fd")
	case isSelected:
		metaFg = lipgloss.Color("#64748b")
	default:
		metaFg = lipgloss.Color("#334155")
	}
	metaStyle := lipgloss.NewStyle().Width(d.colWidth).MaxWidth(d.colWidth).PaddingLeft(gutterW).Foreground(metaFg)
	if isSelected && d.isActive {
		metaStyle = metaStyle.Background(detailBg)
	}
	fmt.Fprint(w, metaStyle.Render(meta))
}

// Column represents one kanban column backed by a directory.
type Column struct {
	Name         string
	Path         string
	Items        []Item // unfiltered master list (used by file operations)
	list         list.Model
	colWidth     int
	previewLines int
}

func NewColumn(name, path string, colWidth, previewLines int) *Column {
	delegate := itemDelegate{colWidth: colWidth}
	l := list.New(nil, delegate, colWidth, 20)
	l.SetShowTitle(false)
	l.SetShowFilter(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()
	l.Styles.NoItems = lipgloss.NewStyle().
		PaddingLeft(2).
		Foreground(lipgloss.Color("#475569"))

	return &Column{Name: name, Path: path, list: l, colWidth: colWidth, previewLines: previewLines}
}

func (c *Column) SetHeight(h int) {
	c.list.SetHeight(h)
}

func (c *Column) UpdateList(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	c.list, cmd = c.list.Update(msg)
	return cmd
}

func (c *Column) renderHeader(isActive bool, leftPad int) string {
	var nameFg, countFg, sepColor lipgloss.Color
	if isActive {
		nameFg = lipgloss.Color("#f8fafc")
		countFg = lipgloss.Color("#60a5fa")
		sepColor = lipgloss.Color("#3b82f6")
	} else {
		nameFg = lipgloss.Color("#94a3b8")
		countFg = lipgloss.Color("#475569")
		sepColor = lipgloss.Color("#334155")
	}

	nameLabel := strings.ToUpper(c.Name)
	countLabel := strconv.Itoa(c.TotalCount())
	filtered := c.list.IsFiltered() && !c.list.SettingFilter()
	if filtered {
		countLabel = strconv.Itoa(len(c.list.VisibleItems())) + "/" + strconv.Itoa(c.TotalCount())
	}

	indicator := "  "
	if isActive {
		indicator = lipgloss.NewStyle().Foreground(sepColor).Bold(true).Render("▍ ")
	}
	if filtered {
		indicator = lipgloss.NewStyle().Foreground(countFg).Render("⌕ ")
	}

	leftPadStr := ""
	if leftPad > 0 {
		leftPadStr = strings.Repeat(" ", leftPad)
	}

	name := lipgloss.NewStyle().Bold(true).Foreground(nameFg).Render(nameLabel)
	count := lipgloss.NewStyle().Foreground(countFg).Render(countLabel)

	used := lipgloss.Width(leftPadStr) + lipgloss.Width(indicator) + lipgloss.Width(name) + lipgloss.Width(count)
	gap := c.colWidth - used
	if gap < 1 {
		gap = 1
	}
	spacer := strings.Repeat(" ", gap)

	header := leftPadStr + indicator + name + spacer + count
	sep := lipgloss.NewStyle().Foreground(sepColor).Render(strings.Repeat("─", c.colWidth))

	return lipgloss.JoinVertical(lipgloss.Left, header, sep)
}

func (c *Column) View(isActive bool, mnemonicOf func(name string) string, gutterW int) string {
	c.list.SetDelegate(itemDelegate{isActive: isActive, mnemonicOf: mnemonicOf, gutterW: gutterW, colWidth: c.colWidth})
	c.list.SetShowFilter(c.list.SettingFilter() || c.list.IsFiltered())

	var borderColor lipgloss.Color
	if isActive {
		borderColor = lipgloss.Color("#3b82f6")
	} else {
		borderColor = lipgloss.Color("#334155")
	}

	leftPad := gutterW - 2
	if leftPad < 0 {
		leftPad = 0
	}
	content := lipgloss.JoinVertical(lipgloss.Left,
		c.renderHeader(isActive, leftPad),
		c.list.View(),
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Render(content)
}

func (c *Column) IsFiltering() bool {
	return c.list.SettingFilter()
}

func (c *Column) LoadItems() error {
	entries, err := os.ReadDir(c.Path)
	if err != nil {
		return err
	}

	items := []Item{}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		if !entry.IsDir() && strings.HasSuffix(name, ".md") {
			fullPath := filepath.Join(c.Path, name)
			item, err := NewItem(fullPath, c.previewLines)
			if err == nil {
				items = append(items, item)
			}
		}
	}

	c.Items = SortItems(items)

	listItems := make([]list.Item, len(c.Items))
	for i, item := range c.Items {
		listItems[i] = item
	}
	c.list.SetItems(listItems)
	return nil
}

func (c *Column) TotalCount() int {
	return len(c.Items)
}

// VisibleItems returns the items currently rendered (post filter+sort), in
// display order.
func (c *Column) VisibleItems() []Item {
	li := c.list.VisibleItems()
	out := make([]Item, 0, len(li))
	for _, it := range li {
		if item, ok := it.(Item); ok {
			out = append(out, item)
		}
	}
	return out
}

func (c *Column) HasSelectedItem() bool {
	return len(c.Items) > 0 && c.list.SelectedItem() != nil
}

func (c *Column) SelectedItem() *Item {
	li := c.list.SelectedItem()
	if li == nil {
		return nil
	}
	item := li.(Item)
	return &item
}

func (c *Column) MoveItemTo(destCol *Column, itemName string) error {
	srcPath := ""
	for _, item := range c.Items {
		if item.Name == itemName {
			srcPath = item.FullPath
			break
		}
	}
	if srcPath == "" {
		return os.ErrNotExist
	}

	destPath := filepath.Join(destCol.Path, filepath.Base(srcPath))
	if err := os.Rename(srcPath, destPath); err != nil {
		return err
	}

	c.LoadItems()
	destCol.LoadItems()
	return nil
}

func (c *Column) DeleteItem(itemName string) error {
	for _, item := range c.Items {
		if item.Name == itemName {
			return os.Remove(item.FullPath)
		}
	}
	return os.ErrNotExist
}

func (c *Column) CreateItem(name string) (string, error) {
	filename := name + ".md"
	fullPath := filepath.Join(c.Path, filename)
	if _, err := os.Create(fullPath); err != nil {
		return "", err
	}
	c.LoadItems()
	return filename, nil
}

func (c *Column) AppendText(itemName, text string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		text = "\n" + text
	}
	return os.WriteFile(fullPath, append(content, []byte(text+"\n")...), 0644)
}

func (c *Column) PrependText(itemName, text string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}
	return os.WriteFile(fullPath, append([]byte(text+"\n"), content...), 0644)
}

func (c *Column) JournalText(itemName, text string) error {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	return c.AppendText(itemName, timestamp+" - "+text)
}

func (c *Column) CopyContent(itemName string) ([]byte, error) {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(fullPath)
}

func (c *Column) OpenFile(itemName string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	return openFile(fullPath)
}

func (c *Column) RenameItem(oldName, newName string) error {
	for i := range c.Items {
		if c.Items[i].Name == oldName {
			newPath := filepath.Join(c.Path, newName+".md")
			if _, err := os.Stat(newPath); err == nil {
				return os.ErrExist
			}
			if err := os.Rename(c.Items[i].FullPath, newPath); err != nil {
				return err
			}
			return c.LoadItems()
		}
	}
	return os.ErrNotExist
}

func (c *Column) Rename(newName string) error {
	parent := filepath.Dir(c.Path)
	newPath := filepath.Join(parent, newName)
	if _, err := os.Stat(newPath); err == nil {
		return os.ErrExist
	}
	if err := os.Rename(c.Path, newPath); err != nil {
		return err
	}
	c.Name = newName
	c.Path = newPath
	return c.LoadItems()
}

func (c *Column) PinItem(itemName string) error {
	for i := range c.Items {
		if c.Items[i].Name == itemName {
			c.Items[i].TogglePin()
			newName := c.Items[i].Name
			newPath := filepath.Join(c.Path, newName+".md")
			if err := os.Rename(c.Items[i].FullPath, newPath); err != nil {
				return err
			}
			return c.LoadItems()
		}
	}
	return os.ErrNotExist
}

func (c *Column) fullPathFor(itemName string) string {
	for _, item := range c.Items {
		if item.Name == itemName {
			return item.FullPath
		}
	}
	return ""
}
