package model

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Column struct {
	Name           string
	Path           string
	Items          []Item
	SelectedIndex  int
	Offset         int
	HasFocus       bool
	SearchQuery    string
	FilteredItems  []Item
}

func NewColumn(name, path string) *Column {
	return &Column{
		Name: name,
		Path: path,
	}
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
	c.applySearch()
	return nil
}

func (c *Column) applySearch() {
	if c.SearchQuery == "" {
		c.FilteredItems = c.Items
		return
	}

	c.FilteredItems = []Item{}
	for _, item := range c.Items {
		if strings.Contains(strings.ToLower(item.Name), strings.ToLower(c.SearchQuery)) {
			c.FilteredItems = append(c.FilteredItems, item)
		}
	}

	if c.SelectedIndex >= len(c.FilteredItems) {
		c.SelectedIndex = len(c.FilteredItems) - 1
	}
	if c.SelectedIndex < 0 {
		c.SelectedIndex = 0
	}
}

func (c *Column) TotalCount() int {
	return len(c.Items)
}

func (c *Column) VisibleCount() int {
	return len(c.FilteredItems)
}

func (c *Column) VisibleItems() []Item {
	return c.FilteredItems
}

func (c *Column) SelectUp() {
	if c.SelectedIndex > 0 {
		c.SelectedIndex--
		c.ensureVisible()
	}
}

func (c *Column) SelectDown() {
	if c.SelectedIndex < len(c.FilteredItems)-1 {
		c.SelectedIndex++
		c.ensureVisible()
	}
}

func (c *Column) SelectFirst() {
	c.SelectedIndex = 0
	c.Offset = 0
}

func (c *Column) SelectLast() {
	c.SelectedIndex = len(c.FilteredItems) - 1
	c.ensureVisible()
}

func (c *Column) PageUp() {
	pageSize := 10
	c.SelectedIndex -= pageSize
	if c.SelectedIndex < 0 {
		c.SelectedIndex = 0
		c.Offset = 0
	} else {
		c.Offset -= pageSize
		if c.Offset < 0 {
			c.Offset = 0
		}
	}
}

func (c *Column) PageDown() {
	pageSize := 10
	c.SelectedIndex += pageSize
	if c.SelectedIndex >= len(c.FilteredItems) {
		c.SelectedIndex = len(c.FilteredItems) - 1
	}
	c.ensureVisible()
}

func (c *Column) ensureVisible() {
	if len(c.FilteredItems) == 0 {
		return
	}
	if c.SelectedIndex < c.Offset {
		c.Offset = c.SelectedIndex
	}
	if c.SelectedIndex >= c.Offset+10 {
		c.Offset = c.SelectedIndex - 9
	}
}

func (c *Column) HasSelectedItem() bool {
	return c.SelectedIndex >= 0 && c.SelectedIndex < len(c.FilteredItems)
}

func (c *Column) SelectedItem() *Item {
	if !c.HasSelectedItem() {
		return nil
	}
	return &c.FilteredItems[c.SelectedIndex]
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
	err := os.Rename(srcPath, destPath)
	if err != nil {
		return err
	}

	c.LoadItems()
	destCol.LoadItems()
	return nil
}

func (c *Column) DeleteItem(itemName string) error {
	fullPath := ""
	for _, item := range c.Items {
		if item.Name == itemName {
			fullPath = item.FullPath
			break
		}
	}
	if fullPath == "" {
		return os.ErrNotExist
	}
	return os.Remove(fullPath)
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
	fullPath := ""
	for _, item := range c.Items {
		if item.Name == itemName {
			fullPath = item.FullPath
			break
		}
	}
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
	fullPath := ""
	for _, item := range c.Items {
		if item.Name == itemName {
			fullPath = item.FullPath
			break
		}
	}
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
	journalEntry := timestamp + " - " + text + "\n"
	return c.AppendText(itemName, journalEntry)
}

func (c *Column) CopyContent(itemName string) ([]byte, error) {
	fullPath := ""
	for _, item := range c.Items {
		if item.Name == itemName {
			fullPath = item.FullPath
			break
		}
	}
	if fullPath == "" {
		return nil, os.ErrNotExist
	}

	return os.ReadFile(fullPath)
}

func (c *Column) OpenFile(itemName string) error {
	fullPath := ""
	for _, item := range c.Items {
		if item.Name == itemName {
			fullPath = item.FullPath
			break
		}
	}
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
			err := os.Rename(c.Items[i].FullPath, newPath)
			if err != nil {
				return err
			}
			c.Items[i].FullPath = newPath
			c.LoadItems()
			return nil
		}
	}
	return os.ErrNotExist
}

func (c *Column) UpdateSearch(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyEsc {
		c.SearchQuery = ""
		return nil
	}
	if msg.Type == tea.KeyRunes {
		for _, r := range msg.Runes {
			c.SearchQuery += string(r)
		}
		c.applySearch()
		return nil
	}
	return nil
}

const colWidth = 30

func (c *Column) Render(style ColumnStyle, visibleHeight int, isActive bool) string {
	var result strings.Builder

	headerStyle := lipgloss.NewStyle().
		Width(colWidth).
		Align(lipgloss.Left).
		Bold(true).
		Padding(0, 1)

	if isActive {
		headerStyle = headerStyle.
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#3b82f6"))
	} else {
		headerStyle = headerStyle.
			Foreground(lipgloss.Color("#94a3b8")).
			Background(lipgloss.Color("#1e293b"))
	}

	header := headerStyle.Render(" " + c.Name + " [" + strconv.Itoa(c.TotalCount()) + "] ")
	result.WriteString(header)
	result.WriteString("\n")

	separatorColor := lipgloss.Color("#3b82f6")
	if !isActive {
		separatorColor = lipgloss.Color("#334155")
	}
	separator := strings.Repeat("─", colWidth)
	result.WriteString(lipgloss.NewStyle().Foreground(separatorColor).Render(separator))
	result.WriteString("\n")

	items := c.FilteredItems
	if len(items) == 0 {
		result.WriteString(lipgloss.NewStyle().Width(colWidth).PaddingLeft(2).Foreground(lipgloss.Color("#64748b")).Render("(empty)"))
		result.WriteString("\n")
		return result.String()
	}

	start := c.Offset
	end := start + visibleHeight
	if end > len(items) {
		end = len(items)
	}

	for i := start; i < end; i++ {
		item := items[i]
		isSelected := i == c.SelectedIndex

		cursor := "  "
		if isSelected && isActive {
			cursor = "> "
		}

		pinIcon := ""
		if item.Pinned {
			pinIcon = "📌 "
		}

		itemStyle := lipgloss.NewStyle().Width(colWidth).MaxWidth(colWidth)

		if isSelected && isActive {
			itemStyle = itemStyle.
				Bold(true).
				Background(lipgloss.Color("#3b82f6")).
				Foreground(lipgloss.Color("#ffffff"))
		} else if isSelected {
			itemStyle = itemStyle.
				Bold(true).
				Foreground(lipgloss.Color("#e2e8f0"))
		} else {
			itemStyle = itemStyle.Foreground(lipgloss.Color("#64748b"))
		}

		result.WriteString(itemStyle.Render(cursor + pinIcon + item.Name))
		result.WriteString("\n")

		for _, line := range item.Preview {
			var previewColor lipgloss.Color
			if isSelected && isActive {
				previewColor = lipgloss.Color("#bfdbfe")
			} else if isSelected {
				previewColor = lipgloss.Color("#94a3b8")
			} else {
				previewColor = lipgloss.Color("#475569")
			}
			previewStyle := lipgloss.NewStyle().
				Width(colWidth).
				MaxWidth(colWidth).
				PaddingLeft(4).
				Foreground(previewColor)
			result.WriteString(previewStyle.Render(line))
			result.WriteString("\n")
		}

		if i < end-1 {
			result.WriteString("\n")
		}
	}

	return result.String()
}

type ColumnStyle struct {
	BorderColor lipgloss.Color
	HeaderColor lipgloss.Color
}

func DefaultColumnStyle() ColumnStyle {
	return ColumnStyle{
		BorderColor: lipgloss.Color("#334155"),
		HeaderColor: lipgloss.Color("#94a3b8"),
	}
}
