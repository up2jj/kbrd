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

const colWidth = 32

// itemDelegate renders each kanban item inside a bubbles list.
type itemDelegate struct {
	isActive bool
}

func (d itemDelegate) Height() int  { return 1 }
func (d itemDelegate) Spacing() int { return 1 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(Item)
	if !ok {
		return
	}
	isSelected := index == m.Index()

	cursor := "  "
	if isSelected && d.isActive {
		cursor = "> "
	}
	pinIcon := ""
	if item.Pinned {
		pinIcon = "📌 "
	}

	nameStyle := lipgloss.NewStyle().Width(colWidth).MaxWidth(colWidth)
	switch {
	case isSelected && d.isActive:
		nameStyle = nameStyle.Bold(true).
			Background(lipgloss.Color("#3b82f6")).
			Foreground(lipgloss.Color("#ffffff"))
	case isSelected:
		nameStyle = nameStyle.Bold(true).Foreground(lipgloss.Color("#e2e8f0"))
	default:
		nameStyle = nameStyle.Foreground(lipgloss.Color("#64748b"))
	}
	fmt.Fprint(w, nameStyle.Render(cursor+pinIcon+item.Name))
}

// Column represents one kanban column backed by a directory.
type Column struct {
	Name  string
	Path  string
	Items []Item // unfiltered master list (used by file operations)
	list  list.Model
}

func NewColumn(name, path string) *Column {
	delegate := itemDelegate{}
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

	return &Column{Name: name, Path: path, list: l}
}

func (c *Column) SetHeight(h int) {
	c.list.SetHeight(h)
}

func (c *Column) UpdateList(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	c.list, cmd = c.list.Update(msg)
	return cmd
}

func (c *Column) renderHeader(isActive bool) string {
	var bg, fg, sepColor lipgloss.Color
	if isActive {
		bg = lipgloss.Color("#3b82f6")
		fg = lipgloss.Color("#ffffff")
		sepColor = lipgloss.Color("#60a5fa")
	} else {
		bg = lipgloss.Color("#1e293b")
		fg = lipgloss.Color("#64748b")
		sepColor = lipgloss.Color("#334155")
	}

	baseStyle := lipgloss.NewStyle().Background(bg).Foreground(fg)

	name := baseStyle.Bold(true).Padding(0, 1).Render(c.Name)
	count := baseStyle.Padding(0, 1).Render(strconv.Itoa(c.TotalCount()))

	// fill remaining space between name and count
	gap := colWidth - lipgloss.Width(name) - lipgloss.Width(count)
	if gap < 0 {
		gap = 0
	}
	spacer := baseStyle.Render(strings.Repeat(" ", gap))

	header := lipgloss.JoinHorizontal(lipgloss.Top, name, spacer, count)
	sep := lipgloss.NewStyle().Foreground(sepColor).Render(strings.Repeat("─", colWidth))

	return lipgloss.JoinVertical(lipgloss.Left, header, sep)
}

func (c *Column) View(isActive bool) string {
	c.list.SetDelegate(itemDelegate{isActive: isActive})
	c.list.SetShowFilter(c.list.SettingFilter() || c.list.IsFiltered())

	var borderColor lipgloss.Color
	if isActive {
		borderColor = lipgloss.Color("#3b82f6")
	} else {
		borderColor = lipgloss.Color("#334155")
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		c.renderHeader(isActive),
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
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			fullPath := filepath.Join(c.Path, entry.Name())
			item, err := NewItem(fullPath)
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
