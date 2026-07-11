package model

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"kbrd/harpoon"
)

// HarpoonMenu is the five-slot, board-scoped quick-jump overlay. Slot paths
// live in the machine-local harpoon store, while this component owns only the
// interaction and rendering state for the open menu.
type HarpoonMenu struct {
	active    bool
	palette   Palette
	boardPath string
	slots     harpoon.Slots
	selected  int
}

func (m *HarpoonMenu) Active() bool { return m.active }

func (m *HarpoonMenu) Close() { m.active = false }

func (m *HarpoonMenu) SetPalette(p Palette) { m.palette = p }

func (m *HarpoonMenu) Open(boardPath string) error {
	store, err := harpoon.Load()
	if err != nil {
		return err
	}
	m.active = true
	m.boardPath = boardPath
	m.slots = store.ForBoard(boardPath)
	m.selected = firstEmptySlot(m.slots)
	return nil
}

func firstEmptySlot(slots harpoon.Slots) int {
	for i, path := range slots {
		if path == "" {
			return i
		}
	}
	return 0
}

func (m *HarpoonMenu) Update(msg tea.KeyPressMsg) {
	switch msg.Code {
	case tea.KeyUp:
		m.selected = (m.selected + harpoon.SlotCount - 1) % harpoon.SlotCount
	case tea.KeyDown:
		m.selected = (m.selected + 1) % harpoon.SlotCount
	default:
		switch msg.String() {
		case "k":
			m.selected = (m.selected + harpoon.SlotCount - 1) % harpoon.SlotCount
		case "j":
			m.selected = (m.selected + 1) % harpoon.SlotCount
		}
	}
}

func (m *HarpoonMenu) Select(slot int) {
	if slot >= 0 && slot < harpoon.SlotCount {
		m.selected = slot
	}
}

func (m *HarpoonMenu) SetSelected(path string) error {
	store, err := harpoon.Load()
	if err != nil {
		return err
	}
	if err := store.Set(m.boardPath, m.selected, path); err != nil {
		return err
	}
	if err := store.Save(); err != nil {
		return err
	}
	m.slots = store.ForBoard(m.boardPath)
	return nil
}

func (m *HarpoonMenu) SelectedPath() string { return m.slots[m.selected] }

func (m *HarpoonMenu) View(termWidth, _ int) string {
	if !m.active {
		return ""
	}
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(m.palette.Primary).Width(4)
	selectedStyle := lipgloss.NewStyle().Background(m.palette.BgSelectedDetail).Foreground(m.palette.FgSelectedPreview)
	pathStyle := lipgloss.NewStyle().Foreground(m.palette.FgMuted)
	emptyStyle := lipgloss.NewStyle().Foreground(m.palette.FgDim).Italic(true)
	rows := make([]string, 0, harpoon.SlotCount)
	for i, path := range m.slots {
		label := emptyStyle.Render("empty")
		if path != "" {
			label = pathStyle.Render(m.displayPath(path))
		}
		row := " " + keyStyle.Render(fmt.Sprintf("%d", i+1)) + label
		if i == m.selected {
			row = selectedStyle.Render(row)
		}
		rows = append(rows, row)
	}
	footer := RenderInlineHints([]Shortcut{{"↑/↓", "select"}, {"1-5/enter", "jump"}, {"a", "assign"}, {"d", "clear"}, {"esc/q", "close"}})
	contentW := lipgloss.Width(footer) + 2
	for _, row := range rows {
		contentW = max(contentW, lipgloss.Width(row)+1)
	}
	if termWidth > 0 {
		contentW = min(contentW, max(termWidth-8, 1))
	}
	return OverlayFrame{
		Title:   "Harpoon",
		Body:    lipgloss.JoinVertical(lipgloss.Left, rows...),
		Footer:  footer,
		Width:   overlayWidthForBody(contentW),
		Palette: m.palette,
	}.Render()
}

func (m *HarpoonMenu) displayPath(path string) string {
	if rel, err := filepath.Rel(m.boardPath, path); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

type boardHarpoonActions struct {
	board *Board
}

func (b *Board) harpoonActions() boardHarpoonActions { return boardHarpoonActions{board: b} }

func (a boardHarpoonActions) open() (tea.Model, tea.Cmd) {
	b := a.board
	if err := b.harpoon.Open(b.cfg.Path); err != nil {
		return b, b.notifier.ErrorCause("failed to load harpoon slots", err)
	}
	return b, nil
}

func (a boardHarpoonActions) update(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	b := a.board
	if key.Matches(msg, Keys.Quit) {
		b.harpoon.Close()
		return b.beginShutdown()
	}
	switch {
	case key.Matches(msg, Keys.HarpoonClose):
		b.harpoon.Close()
	case msg.String() == "a":
		return a.assign()
	case msg.String() == "d":
		return a.clear()
	case msg.Code == tea.KeyEnter:
		return a.jump()
	case len(msg.Text) == 1 && msg.Text[0] >= '1' && msg.Text[0] <= '5':
		b.harpoon.Select(int(msg.Text[0] - '1'))
		return a.jump()
	default:
		b.harpoon.Update(msg)
	}
	return b, nil
}

func (a boardHarpoonActions) assign() (tea.Model, tea.Cmd) {
	b := a.board
	if b.selectedCol < 0 || b.selectedCol >= len(b.columns) {
		return b, b.notifier.Error("no file selected")
	}
	col := b.columns[b.selectedCol]
	if col.Virtual {
		return b, b.notifier.ErrorCause("cannot assign a virtual item", errVirtualColumn)
	}
	item := col.SelectedItem()
	if item == nil || item.FullPath == "" {
		return b, b.notifier.Error("no file selected")
	}
	if err := b.harpoon.SetSelected(item.FullPath); err != nil {
		return b, b.notifier.ErrorCause("failed to save harpoon slot", err)
	}
	return b, b.notifier.Success(fmt.Sprintf("harpoon %d → %s", b.harpoon.selected+1, item.Name))
}

func (a boardHarpoonActions) clear() (tea.Model, tea.Cmd) {
	b := a.board
	if b.harpoon.SelectedPath() == "" {
		return b, nil
	}
	if err := b.harpoon.SetSelected(""); err != nil {
		return b, b.notifier.ErrorCause("failed to clear harpoon slot", err)
	}
	return b, b.notifier.Success(fmt.Sprintf("cleared harpoon %d", b.harpoon.selected+1))
}

func (a boardHarpoonActions) jump() (tea.Model, tea.Cmd) {
	b := a.board
	path := b.harpoon.SelectedPath()
	if path == "" {
		return b, b.notifier.Error(fmt.Sprintf("harpoon %d is empty", b.harpoon.selected+1))
	}
	for colIdx, col := range b.columns {
		if col.Virtual {
			continue
		}
		for _, item := range col.Items {
			if samePath(item.FullPath, path) {
				b.selectedCol = colIdx
				col.SelectByName(item.Name)
				col.Expand()
				b.harpoon.Close()
				return b, nil
			}
		}
	}
	return b, b.notifier.Error(fmt.Sprintf("harpoon %d no longer exists", b.harpoon.selected+1))
}
