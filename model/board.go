package model

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"kbrd/config"
	kbrdfs "kbrd/fs"
)

type watchMsg struct{}

type Board struct {
	cfg              config.Config
	columns          []*Column
	colStyle         ColumnStyle
	visibleHeight    int
	selectedCol      int
	quitting         bool
	editor           *Editor
	toastMgr         *ToastManager
	searchMode       bool
	searchCol        *Column
	quickCmdMode     bool
	quickCmdInput    string
	statusMsg        string
	statusColor      string
	statusTimer      int
	theme            string
	watcher          *kbrdfs.Watcher
	dialog           Dialog
}

func NewBoard(cfg config.Config) *Board {
	return &Board{
		cfg:        cfg,
		colStyle:   DefaultColumnStyle(),
		visibleHeight: 10,
		editor:     NewEditor(),
		toastMgr:   NewToastManager(),
	}
}

func (b *Board) Init() tea.Cmd {
	return func() tea.Msg {
		if err := b.loadColumns(); err != nil {
			return toastMsg{Message: "failed to load columns: " + err.Error(), Type: toastError}
		}
		paths, err := kbrdfs.DiscoverPaths(b.cfg.Path)
		if err == nil {
			if w, err := kbrdfs.NewWatcher(paths); err == nil {
				b.watcher = w
			}
		}
		return watchMsg{}
	}
}

func (b *Board) watchCmd() tea.Cmd {
	if b.watcher == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case _, ok := <-b.watcher.Events():
			if !ok {
				return nil
			}
		case _, ok := <-b.watcher.Errors():
			if !ok {
				return nil
			}
		}
		return watchMsg{}
	}
}

func (b *Board) loadColumns() error {
	entries, err := os.ReadDir(b.cfg.Path)
	if err != nil {
		return err
	}

	b.columns = []*Column{}
	for _, entry := range entries {
		if entry.IsDir() {
			col := NewColumn(entry.Name(), filepath.Join(b.cfg.Path, entry.Name()))
			if err := col.LoadItems(); err != nil {
				continue
			}
			b.columns = append(b.columns, col)
		}
	}

	if len(b.columns) > 0 && b.selectedCol >= len(b.columns) {
		b.selectedCol = len(b.columns) - 1
	}
	return nil
}

func (b *Board) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if b.quitting {
		return b, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.visibleHeight = msg.Height - 6
		return b, nil

	case tea.KeyMsg:
		return b.handleKey(msg)

	case toastMsg:
		return b, b.toastMgr.Add(msg.Message, msg.Type)

	case watchMsg:
		b.loadColumns()
		return b, b.watchCmd()

	case toastTickMsg:
		tm, cmd := b.toastMgr.Update(msg)
		b.toastMgr = tm
		return b, cmd

	case editorSaveMsg:
		return b.handleSave(msg)

	case editorAppendMsg:
		return b.handleAppend(msg)

	case editorPrependMsg:
		return b.handlePrepend(msg)

	case editorJournalMsg:
		return b.handleJournal(msg)

	case editorNewMsg:
		return b.handleNew(msg)

	case deleteConfirmMsg:
		return b.handleDelete(msg)

	case quickCommandMsg:
		return b.handleQuickCommand(msg)
	}

	return b, nil
}

func (b *Board) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle dialog
	if b.dialog.active {
		return b, b.dialog.Update(msg)
	}

	// Handle editor open
	if b.editor.state != editorNone {
		cmd, _ := b.editor.Update(msg)
		if b.editor.state == editorNone {
			b.editor = NewEditor()
		}
		return b, cmd
	}

	// Handle search
	if b.searchMode {
		return b.handleSearchKey(msg)
	}

	// Handle quick command
	if b.quickCmdMode {
		return b.handleQuickCommandKey(msg)
	}

	// Handle status message timeout
	if b.statusTimer > 0 {
		b.statusTimer--
		if b.statusTimer == 0 {
			b.statusMsg = ""
			b.statusColor = ""
		}
	}

	switch msg.String() {
	case "q":
		return b, b.openQuickCommand()
	case "t":
		b.toggleTheme()
		return b, nil
	case "R":
		return b, b.refresh()
	}

	if len(b.columns) == 0 {
		return b, nil
	}

	col := b.columns[b.selectedCol]

	switch msg.String() {
	case "ctrl+c":
		b.quitting = true
		return b, tea.Quit
	case "left", "h":
		b.selectedCol--
		if b.selectedCol < 0 {
			b.selectedCol = len(b.columns) - 1
		}
	case "right", "l":
		b.selectedCol++
		if b.selectedCol >= len(b.columns) {
			b.selectedCol = 0
		}
	case "Tab":
		b.selectedCol++
		if b.selectedCol >= len(b.columns) {
			b.selectedCol = 0
		}
	case "shift+tab":
		b.selectedCol--
		if b.selectedCol < 0 {
			b.selectedCol = len(b.columns) - 1
		}
	case "up", "k":
		col.SelectUp()
	case "down", "j":
		col.SelectDown()
	case "home":
		col.SelectFirst()
	case "end":
		col.SelectLast()
	case "pgup":
		col.PageUp()
	case "pgdown":
		col.PageDown()
	case "/":
		return b, b.openSearch()
	case "e":
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			return b, b.editor.OpenEdit(b.selectedCol, item.Name)
		}
	case "a":
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			return b, b.editor.OpenAppend(b.selectedCol, item.Name)
		}
	case "p":
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			return b, b.editor.OpenPrepend(b.selectedCol, item.Name)
		}
	case "J":
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			return b, b.editor.OpenJournal(b.selectedCol, item.Name)
		}
	case "c":
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			content, err := col.CopyContent(item.Name)
			if err != nil {
				return b, b.toastMgr.Add("failed to copy: "+err.Error(), toastError)
			}
			// Use system clipboard
			return b, b.copyToClipboard([]byte(content))
		}
	case "o":
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			err := col.OpenFile(item.Name)
			if err != nil {
				return b, b.toastMgr.Add("failed to open: "+err.Error(), toastError)
			}
			return b, b.toastMgr.Add("opened "+item.Name, toastSuccess)
		}
	case "!":
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			err := col.PinItem(item.Name)
			if err != nil {
				return b, b.toastMgr.Add("failed to pin: "+err.Error(), toastError)
			}
			pinState := "unpinned"
			if item.Pinned {
				pinState = "pinned"
			}
			return b, b.toastMgr.Add(item.Name+" "+pinState, toastSuccess)
		}
	case "d":
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			b.dialog.Open("Delete item?", item.Name+".md", []DialogButton{
				{Label: "Yes", Danger: true, Msg: deleteConfirmMsg{ColIndex: b.selectedCol, FileName: item.Name}},
				{Label: "No", Primary: true},
			})
			b.dialog.selected = 1
		}
	case "m":
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			nextCol := (b.selectedCol + 1) % len(b.columns)
			if err := col.MoveItemTo(b.columns[nextCol], item.Name); err != nil {
				return b, b.toastMgr.Add("failed to move: "+err.Error(), toastError)
			}
			b.selectedCol = nextCol
		}
	case "n":
		return b, b.editor.OpenNew(b.selectedCol)
	case "N":
		return b, b.editor.OpenNew(b.selectedCol)
	}

	return b, nil
}

func (b *Board) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		b.searchMode = false
		b.searchCol.SearchQuery = ""
		return b, nil
	case tea.KeyEnter:
		query := strings.TrimSpace(b.searchCol.SearchQuery)
		if query != "" {
			b.searchCol.SearchQuery = query
		}
		b.searchMode = false
		return b, nil
	}

	cmd := b.searchCol.UpdateSearch(msg)
	return b, cmd
}

func (b *Board) handleSave(msg editorSaveMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	err := os.WriteFile(col.Path+"/"+msg.FileName, []byte(msg.Content), 0644)
	if err != nil {
		return b, b.toastMgr.Add("failed to save: "+err.Error(), toastError)
	}
	col.LoadItems()
	b.statusMsg = "saved " + msg.FileName
	b.statusColor = "#4ade80"
	b.statusTimer = 60
	return b, nil
}

func (b *Board) handleAppend(msg editorAppendMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	err := col.AppendText(msg.FileName, msg.Text)
	if err != nil {
		return b, b.toastMgr.Add("failed to append: "+err.Error(), toastError)
	}
	col.LoadItems()
	b.statusMsg = "appended to " + msg.FileName
	b.statusColor = "#4ade80"
	b.statusTimer = 60
	return b, nil
}

func (b *Board) handlePrepend(msg editorPrependMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	err := col.PrependText(msg.FileName, msg.Text)
	if err != nil {
		return b, b.toastMgr.Add("failed to prepend: "+err.Error(), toastError)
	}
	col.LoadItems()
	b.statusMsg = "prepended to " + msg.FileName
	b.statusColor = "#4ade80"
	b.statusTimer = 60
	return b, nil
}

func (b *Board) handleJournal(msg editorJournalMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	err := col.JournalText(msg.FileName, msg.Text)
	if err != nil {
		return b, b.toastMgr.Add("failed to journal: "+err.Error(), toastError)
	}
	col.LoadItems()
	b.statusMsg = "journal entry added to " + msg.FileName
	b.statusColor = "#4ade80"
	b.statusTimer = 60
	return b, nil
}

func (b *Board) handleNew(msg editorNewMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	if msg.FileName == "" {
		return b, b.toastMgr.Add("filename cannot be empty", toastError)
	}
	_, err := col.CreateItem(msg.FileName)
	if err != nil {
		return b, b.toastMgr.Add("failed to create: "+err.Error(), toastError)
	}
	b.statusMsg = "created " + msg.FileName + ".md"
	b.statusColor = "#4ade80"
	b.statusTimer = 60
	return b, nil
}

func (b *Board) handleDelete(msg deleteConfirmMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	err := col.DeleteItem(msg.FileName)
	if err != nil {
		return b, b.toastMgr.Add("failed to delete: "+err.Error(), toastError)
	}
	col.LoadItems()
	b.statusMsg = "deleted " + msg.FileName
	b.statusColor = "#4ade80"
	b.statusTimer = 60
	return b, nil
}

func (b *Board) handleQuickCommand(msg quickCommandMsg) (tea.Model, tea.Cmd) {
	cmd := strings.TrimPrefix(msg.Command, ":")
	if cmd == "" {
		return b, nil
	}

	action := cmd[0]
	args := cmd[1:]
	_ = args

	switch action {
	case 'r':
		return b, b.refresh()
	case 't':
		b.toggleTheme()
		return b, nil
	case 'q':
		return b, b.openQuickCommand()
	default:
		return b, b.toastMgr.Add("unknown command: "+string(action), toastError)
	}
}

func (b *Board) handleQuickCommandKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		b.quickCmdMode = false
		b.quickCmdInput = ""
		return b, nil
	case "enter":
		b.quickCmdMode = false
		cmd := strings.TrimSpace(b.quickCmdInput)
		if cmd == "" {
			return b, nil
		}
		return b, func() tea.Msg {
			return quickCommandMsg{Command: cmd}
		}
	case "backspace":
		if len(b.quickCmdInput) > 0 {
			b.quickCmdInput = b.quickCmdInput[:len(b.quickCmdInput)-1]
		}
	case "left":
		// handled by tea
	case "right":
		// handled by tea
	default:
		if msg.Type == tea.KeyRunes {
			for _, r := range msg.Runes {
				b.quickCmdInput += string(r)
			}
		} else if len(key) == 1 {
			b.quickCmdInput += key
		}
	}

	return b, nil
}

func (b *Board) openQuickCommand() tea.Cmd {
	return func() tea.Msg {
		b.quickCmdMode = true
		b.quickCmdInput = ":"
		b.quickCmdInput = ""
		return nil
	}
}

func (b *Board) openSearch() tea.Cmd {
	if len(b.columns) == 0 {
		return nil
	}
	b.searchMode = true
	b.searchCol = b.columns[b.selectedCol]
	b.searchCol.SearchQuery = ""
	return nil
}

func (b *Board) refresh() tea.Cmd {
	return func() tea.Msg {
		if err := b.loadColumns(); err != nil {
			return toastMsg{Message: "failed to refresh: " + err.Error(), Type: toastError}
		}
		return toastMsg{Message: "refreshed", Type: toastSuccess}
	}
}

func (b *Board) toggleTheme() {
	if b.theme == "dark" {
		b.theme = "light"
	} else {
		b.theme = "dark"
	}
}

func (b *Board) copyToClipboard(content []byte) tea.Cmd {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(string(content))
	err := cmd.Run()
	if err != nil {
		return func() tea.Msg {
			return toastMsg{Message: "clipboard not available", Type: toastError}
		}
	}
	return func() tea.Msg {
		return toastMsg{Message: "copied to clipboard", Type: toastSuccess}
	}
}

func (b *Board) View() string {
	if len(b.columns) == 0 {
		return "No columns found in " + b.cfg.Path
	}

	colStyle := lipgloss.NewStyle().MarginRight(2)
	rendered := make([]string, len(b.columns))
	for i, col := range b.columns {
		rendered[i] = colStyle.Render(col.Render(b.colStyle, b.visibleHeight, i == b.selectedCol))
	}
	columnsView := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)

	statusBar := b.renderStatusBar()
	toastView := b.toastMgr.Render()
	quickCmdView := b.renderQuickCommand()

	result := columnsView
	if quickCmdView != "" {
		result += "\n" + quickCmdView
	}
	if toastView != "" {
		result += "\n\n" + toastView
	}
	editorView := b.renderEditor()
	if editorView != "" {
		result += "\n\n" + editorView
	}
	dialogView := b.dialog.View()
	if dialogView != "" {
		result += "\n\n" + dialogView
	}
	result += "\n" + statusBar

	return result
}

func (b *Board) renderStatusBar() string {
	var parts []string

	if b.searchMode {
		parts = append(parts, "/"+b.searchCol.SearchQuery)
		parts = append(parts, "Enter confirm")
		parts = append(parts, "Esc cancel")
		return lipgloss.NewStyle().
			Width(80).
			Align(lipgloss.Center).
			Foreground(lipgloss.Color("#94a3b8")).
			Render(strings.Join(parts, "  "))
	}

	if b.quickCmdMode {
		parts = append(parts, "Esc cancel")
		return lipgloss.NewStyle().
			Width(80).
			Align(lipgloss.Center).
			Foreground(lipgloss.Color("#94a3b8")).
			Render(strings.Join(parts, "  "))
	}

	if b.editor.state != editorNone {
		return lipgloss.NewStyle().
			Width(80).
			Align(lipgloss.Center).
			Foreground(lipgloss.Color("#94a3b8")).
			Render("Editor open - Esc to cancel")
	}

	if b.selectedCol < len(b.columns) && b.columns[b.selectedCol].HasSelectedItem() {
		parts = append(parts, "↑↓ navigate")
		parts = append(parts, "e edit")
		parts = append(parts, "a append")
		parts = append(parts, "p prepend")
		parts = append(parts, "j journal")
		parts = append(parts, "c copy")
		parts = append(parts, "o open")
		parts = append(parts, "! pin")
		parts = append(parts, "N new")
		parts = append(parts, "d delete")
		parts = append(parts, "q quick cmd")
	} else {
		parts = append(parts, "←→ columns")
		parts = append(parts, "N new")
		parts = append(parts, "R refresh")
		parts = append(parts, "q quit")
	}

	if b.statusMsg != "" {
		parts = append([]string{b.statusMsg}, parts...)
	}

	return lipgloss.NewStyle().
		Width(80).
		Align(lipgloss.Center).
		Foreground(lipgloss.Color(b.getStatusColor())).
		Render(strings.Join(parts, "  "))
}

func (b *Board) getStatusColor() string {
	if b.statusColor != "" {
		return b.statusColor
	}
	return "#94a3b8"
}

func (b *Board) renderQuickCommand() string {
	if !b.quickCmdMode {
		return ""
	}

	cursor := b.quickCmdInput + "█"
	if cursor == "█" {
		cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b")).Render(": type command...")
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#e2e8f0")).
		Render(cursor)
}

func (b *Board) renderEditor() string {
	if b.editor.state == editorNone {
		return ""
	}
	return b.editor.View()
}

type quickCommandOpenMsg struct{}
type quickCommandMsg struct {
	Command string
}
