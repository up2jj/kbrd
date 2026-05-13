package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"kbrd/config"
	kbrdfs "kbrd/fs"
	"kbrd/recents"
)

const Version = "v0.1.0"

const logoArt = `  __   __           __
 / /__/ /  ____ ___/ /
/  '_/ _ \/ __// _  /
\_,_/_.__/_/   \_,_/`

type watchMsg struct{}

type initBoardRequestMsg struct{}
type initBoardConfirmMsg struct{}
type initBoardDeclineMsg struct{}

type itemRef struct {
	ColIndex int
	Name     string
}

type Board struct {
	cfg           config.Config
	columns       []*Column
	visibleHeight int
	termWidth     int
	termHeight    int
	selectedCol     int
	firstVisibleCol int
	quitting        bool
	editor        *Editor
	notifier      *Notifier
	quickCmdMode  bool
	quickCmdInput textinput.Model
	theme         string
	watcher       *kbrdfs.Watcher
	dialog        Dialog
	helpOpen      bool
	peek          Peek
	switcher      Switcher

	// mnemonic state — rebuilt whenever the visible item set changes
	mnemonicByRef map[itemRef]string
	refByMnemonic map[string]itemRef
	mnemonicMaxLen int
}

func NewBoard(cfg config.Config) *Board {
	ti := textinput.New()
	ti.Prompt = ": "
	ti.Placeholder = "type command…"
	ti.CharLimit = 32
	ti.Width = 30
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#60a5fa")).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0"))
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#475569")).Italic(true)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#fde047"))
	return &Board{
		cfg:           cfg,
		visibleHeight: 20,
		editor:        NewEditor(),
		notifier:      NewNotifier(cfg.NotifyBackend),
		quickCmdInput: ti,
		theme:         cfg.Theme,
	}
}

func (b *Board) Init() tea.Cmd {
	return func() tea.Msg {
		if err := b.loadColumns(); err != nil {
			return notifyMsg{Message: "failed to load columns: " + err.Error(), Type: notifyError}
		}
		paths, err := kbrdfs.DiscoverPaths(b.cfg.Path)
		if err == nil {
			if w, err := kbrdfs.NewWatcher(paths); err == nil {
				b.watcher = w
			}
		}
		if len(b.columns) == 0 {
			return initBoardRequestMsg{}
		}
		return watchMsg{}
	}
}

func (b *Board) createDefaultColumns() error {
	for _, name := range []string{"1. TO DO", "2. IN PROGRESS", "3. DONE"} {
		if err := os.MkdirAll(filepath.Join(b.cfg.Path, name), 0o755); err != nil {
			return err
		}
	}
	return nil
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
			name := entry.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				continue
			}
			col := NewColumn(name, filepath.Join(b.cfg.Path, name), b.cfg.ColumnWidth, b.cfg.PreviewLines)
			if err := col.LoadItems(); err != nil {
				continue
			}
			if b.visibleHeight > 0 {
				col.SetHeight(b.visibleHeight)
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
		b.termWidth = msg.Width
		b.termHeight = msg.Height
		b.visibleHeight = msg.Height - 8
		if b.visibleHeight < 1 {
			b.visibleHeight = 1
		}
		for _, col := range b.columns {
			col.SetHeight(b.visibleHeight)
		}
		b.editor.SetTermSize(b.termWidth, b.termHeight)
		return b, nil

	case tea.KeyMsg:
		return b.handleKey(msg)

	case notifyMsg:
		return b, b.notifier.Send(msg.Message, msg.Type)

	case watchMsg:
		b.loadColumns()
		return b, b.watchCmd()

	case initBoardRequestMsg:
		b.dialog.Open(
			"Initialize kanban board?",
			"Create default columns in "+b.cfg.Path+":\n1. TO DO  •  2. IN PROGRESS  •  3. DONE",
			[]DialogButton{
				{Label: "Create", Primary: true, Msg: initBoardConfirmMsg{}},
				{Label: "Quit", Msg: initBoardDeclineMsg{}},
			},
		)
		b.dialog.selected = 0
		return b, nil

	case initBoardConfirmMsg:
		if err := b.createDefaultColumns(); err != nil {
			return b, b.notifier.Send("failed to create columns: "+err.Error(), notifyError)
		}
		if err := b.loadColumns(); err != nil {
			return b, b.notifier.Send("failed to load columns: "+err.Error(), notifyError)
		}
		return b, tea.Batch(b.notifier.Send("created default columns", notifySuccess), b.watchCmd())

	case initBoardDeclineMsg:
		b.quitting = true
		return b, tea.Quit

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

	case renameItemRequestMsg:
		return b.handleRenameItemRequest(msg)

	case renameColumnRequestMsg:
		return b.handleRenameColumnRequest(msg)

	case renameItemConfirmMsg:
		return b.handleRenameItemConfirm(msg)

	case renameColumnConfirmMsg:
		return b.handleRenameColumnConfirm(msg)

	case editorDiscardMsg:
		b.editor.Close()
		b.editor = NewEditor()
		return b, nil

	case quickCommandMsg:
		return b.handleQuickCommand(msg)

	case switchBoardMsg:
		return b.handleSwitchBoard(msg)

	default:
		// Pass list-internal messages (e.g. FilterMatchesMsg) to the active column
		if len(b.columns) > 0 {
			return b, b.columns[b.selectedCol].UpdateList(msg)
		}
	}

	return b, nil
}

func (b *Board) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle help overlay
	if b.helpOpen {
		switch {
		case key.Matches(msg, Keys.Quit):
			b.helpOpen = false
			b.quitting = true
			return b, tea.Quit
		case key.Matches(msg, Keys.ToggleHelp), msg.String() == "esc":
			b.helpOpen = false
		}
		return b, nil
	}

	// Handle dialog
	if b.dialog.active {
		return b, b.dialog.Update(msg)
	}

	// Handle editor
	if b.editor.state != editorNone {
		if msg.String() == "esc" && b.editor.IsDirty() {
			b.dialog.Open("Discard unsaved changes?", "Your edits will be lost.", []DialogButton{
				{Label: "Cancel", Primary: true, Msg: nil},
				{Label: "Discard", Danger: true, Msg: editorDiscardMsg{}},
			})
			b.dialog.selected = 0
			return b, nil
		}
		cmd, _ := b.editor.Update(msg)
		if b.editor.state == editorNone {
			b.editor = NewEditor()
		}
		return b, cmd
	}

	// Handle peek modal
	if b.peek.Active() {
		b.peek.Update(msg)
		return b, nil
	}

	// Handle board switcher
	if b.switcher.Active() {
		return b, b.switcher.Update(msg)
	}

	// Handle quick command
	if b.quickCmdMode {
		return b.handleQuickCommandKey(msg)
	}

	if len(b.columns) == 0 {
		return b, nil
	}

	col := b.columns[b.selectedCol]

	// When list is filtering, all keys go directly to it
	if col.IsFiltering() {
		return b, col.UpdateList(msg)
	}

	switch {
	case key.Matches(msg, Keys.Quit):
		b.quitting = true
		return b, tea.Quit
	case key.Matches(msg, Keys.ToggleHelp):
		b.helpOpen = true
		return b, nil
	case key.Matches(msg, Keys.QuickCmd):
		return b, b.openQuickCommand()
	case key.Matches(msg, Keys.SwitchBoard):
		return b, b.openSwitcher()
	case key.Matches(msg, Keys.ToggleTheme):
		b.toggleTheme()
		return b, nil
	case key.Matches(msg, Keys.Refresh):
		return b, b.refresh()
	case key.Matches(msg, Keys.RenameItem):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			return b, b.editor.OpenRenameItem(b.selectedCol, item.Name)
		}
	case key.Matches(msg, Keys.RenameCol):
		return b, b.editor.OpenRenameColumn(b.selectedCol, col.Name)
	case key.Matches(msg, Keys.PrevCol):
		b.selectedCol--
		if b.selectedCol < 0 {
			b.selectedCol = len(b.columns) - 1
		}
	case key.Matches(msg, Keys.NextCol):
		b.selectedCol++
		if b.selectedCol >= len(b.columns) {
			b.selectedCol = 0
		}
	case key.Matches(msg, Keys.PanLeft):
		if b.firstVisibleCol > 0 {
			b.firstVisibleCol--
		}
	case key.Matches(msg, Keys.PanRight):
		_, count := b.visibleColRange()
		maxFirst := len(b.columns) - count
		if maxFirst < 0 {
			maxFirst = 0
		}
		if b.firstVisibleCol < maxFirst {
			b.firstVisibleCol++
		}
	case key.Matches(msg, Keys.Edit):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			return b, b.editor.OpenEdit(b.selectedCol, item.Name, item.FullPath)
		}
	case key.Matches(msg, Keys.Append):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			return b, b.editor.OpenAppend(b.selectedCol, item.Name)
		}
	case key.Matches(msg, Keys.Prepend):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			return b, b.editor.OpenPrepend(b.selectedCol, item.Name)
		}
	case key.Matches(msg, Keys.Journal):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			return b, b.editor.OpenJournal(b.selectedCol, item.Name)
		}
	case key.Matches(msg, Keys.Copy):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			content, err := col.CopyContent(item.Name)
			if err != nil {
				return b, b.notifier.Send("failed to copy: "+err.Error(), notifyError)
			}
			return b, b.copyToClipboard([]byte(content))
		}
	case key.Matches(msg, Keys.Paste):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			return b, b.pasteToItem(b.selectedCol, item.Name)
		}
	case key.Matches(msg, Keys.OpenExternal):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			err := col.OpenFile(item.Name)
			if err != nil {
				return b, b.notifier.Send("failed to open: "+err.Error(), notifyError)
			}
			return b, b.notifier.Send("opened "+item.Name, notifySuccess)
		}
	case key.Matches(msg, Keys.Pin):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			err := col.PinItem(item.Name)
			if err != nil {
				return b, b.notifier.Send("failed to pin: "+err.Error(), notifyError)
			}
			pinState := "unpinned"
			if item.Pinned {
				pinState = "pinned"
			}
			return b, b.notifier.Send(item.Name+" "+pinState, notifySuccess)
		}
	case key.Matches(msg, Keys.Delete):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			b.dialog.Open("Delete item?", item.Name+".md", []DialogButton{
				{Label: "Yes", Danger: true, Msg: deleteConfirmMsg{ColIndex: b.selectedCol, FileName: item.Name}},
				{Label: "No", Primary: true},
			})
			b.dialog.selected = 1
		}
	case key.Matches(msg, Keys.MoveNext):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			nextCol := (b.selectedCol + 1) % len(b.columns)
			if err := col.MoveItemTo(b.columns[nextCol], item.Name); err != nil {
				return b, b.notifier.Send("failed to move: "+err.Error(), notifyError)
			}
			b.selectedCol = nextCol
		}
	case key.Matches(msg, Keys.MoveFirst):
		if col.HasSelectedItem() {
			if len(b.columns) == 0 {
				return b, b.notifier.Send("no folders available", notifyError)
			}
			if b.selectedCol == 0 {
				return b, nil
			}
			item := col.SelectedItem()
			if err := col.MoveItemTo(b.columns[0], item.Name); err != nil {
				return b, b.notifier.Send("failed to move: "+err.Error(), notifyError)
			}
			b.selectedCol = 0
		}
	case key.Matches(msg, Keys.Peek):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			content, err := col.CopyContent(item.Name)
			if err != nil {
				return b, b.notifier.Send("failed to peek: "+err.Error(), notifyError)
			}
			return b, b.peek.Open(item.Name, string(content), b.termWidth)
		}
		return b, nil
	case key.Matches(msg, Keys.Filter):
		col.list.SetShowFilter(true)
		return b, col.UpdateList(msg)
	case key.Matches(msg, Keys.New):
		return b, b.editor.OpenNew(b.selectedCol, b.columns[b.selectedCol].Name)
	case key.Matches(msg, Keys.NewFirst):
		if len(b.columns) == 0 {
			return b, b.notifier.Send("no folders available", notifyError)
		}
		return b, b.editor.OpenNew(0, b.columns[0].Name)
	default:
		return b, col.UpdateList(msg)
	}

	return b, nil
}


func (b *Board) handleSave(msg editorSaveMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	fullPath := ""
	for _, item := range col.Items {
		if item.Name == msg.FileName {
			fullPath = item.FullPath
			break
		}
	}
	if fullPath == "" {
		return b, b.notifier.Send("item not found: "+msg.FileName, notifyError)
	}
	err := os.WriteFile(fullPath, []byte(msg.Content), 0644)
	if err != nil {
		return b, b.notifier.Send("failed to save: "+err.Error(), notifyError)
	}
	col.LoadItems()
	return b, b.notifier.Send("saved "+msg.FileName, notifySuccess)
}

func (b *Board) handleAppend(msg editorAppendMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	err := col.AppendText(msg.FileName, msg.Text)
	if err != nil {
		return b, b.notifier.Send("failed to append: "+err.Error(), notifyError)
	}
	col.LoadItems()
	return b, b.notifier.Send("appended to "+msg.FileName, notifySuccess)
}

func (b *Board) handlePrepend(msg editorPrependMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	err := col.PrependText(msg.FileName, msg.Text)
	if err != nil {
		return b, b.notifier.Send("failed to prepend: "+err.Error(), notifyError)
	}
	col.LoadItems()
	return b, b.notifier.Send("prepended to "+msg.FileName, notifySuccess)
}

func (b *Board) handleJournal(msg editorJournalMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	err := col.JournalText(msg.FileName, msg.Text)
	if err != nil {
		return b, b.notifier.Send("failed to journal: "+err.Error(), notifyError)
	}
	col.LoadItems()
	return b, b.notifier.Send("journal entry added to "+msg.FileName, notifySuccess)
}

func (b *Board) handleNew(msg editorNewMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	if msg.FileName == "" {
		return b, b.notifier.Send("filename cannot be empty", notifyError)
	}
	_, err := col.CreateItem(msg.FileName)
	if err != nil {
		return b, b.notifier.Send("failed to create: "+err.Error(), notifyError)
	}
	return b, b.notifier.Send("created "+msg.FileName+".md", notifySuccess)
}

func validateRenameName(name string) error {
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("name cannot contain path separators")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid name")
	}
	return nil
}

func (b *Board) handleRenameItemRequest(msg renameItemRequestMsg) (tea.Model, tea.Cmd) {
	newName := strings.TrimSpace(msg.NewName)
	if err := validateRenameName(newName); err != nil {
		return b, b.notifier.Send("invalid name: "+err.Error(), notifyError)
	}
	if newName == msg.OldName {
		return b, nil
	}
	if msg.ColIndex < 0 || msg.ColIndex >= len(b.columns) {
		return b, b.notifier.Send("invalid column", notifyError)
	}
	col := b.columns[msg.ColIndex]
	target := filepath.Join(col.Path, newName+".md")
	if _, err := os.Stat(target); err == nil {
		return b, b.notifier.Send("file already exists: "+newName+".md", notifyError)
	}
	b.dialog.Open("Rename item?", msg.OldName+".md → "+newName+".md", []DialogButton{
		{Label: "Yes", Primary: true, Msg: renameItemConfirmMsg{ColIndex: msg.ColIndex, OldName: msg.OldName, NewName: newName}},
		{Label: "No", Msg: nil},
	})
	b.dialog.selected = 0
	return b, nil
}

func (b *Board) handleRenameColumnRequest(msg renameColumnRequestMsg) (tea.Model, tea.Cmd) {
	newName := strings.TrimSpace(msg.NewName)
	if err := validateRenameName(newName); err != nil {
		return b, b.notifier.Send("invalid name: "+err.Error(), notifyError)
	}
	if newName == msg.OldName {
		return b, nil
	}
	if msg.ColIndex < 0 || msg.ColIndex >= len(b.columns) {
		return b, b.notifier.Send("invalid column", notifyError)
	}
	parent := filepath.Dir(b.columns[msg.ColIndex].Path)
	target := filepath.Join(parent, newName)
	if _, err := os.Stat(target); err == nil {
		return b, b.notifier.Send("folder already exists: "+newName, notifyError)
	}
	b.dialog.Open("Rename column?", msg.OldName+" → "+newName, []DialogButton{
		{Label: "Yes", Primary: true, Msg: renameColumnConfirmMsg{ColIndex: msg.ColIndex, OldName: msg.OldName, NewName: newName}},
		{Label: "No", Msg: nil},
	})
	b.dialog.selected = 0
	return b, nil
}

func (b *Board) handleRenameItemConfirm(msg renameItemConfirmMsg) (tea.Model, tea.Cmd) {
	if msg.ColIndex < 0 || msg.ColIndex >= len(b.columns) {
		return b, b.notifier.Send("invalid column", notifyError)
	}
	col := b.columns[msg.ColIndex]
	if err := col.RenameItem(msg.OldName, msg.NewName); err != nil {
		return b, b.notifier.Send("failed to rename: "+err.Error(), notifyError)
	}
	for i, it := range col.Items {
		if it.Name == msg.NewName {
			col.list.Select(i)
			break
		}
	}
	return b, b.notifier.Send("renamed "+msg.OldName+" → "+msg.NewName, notifySuccess)
}

func (b *Board) handleRenameColumnConfirm(msg renameColumnConfirmMsg) (tea.Model, tea.Cmd) {
	if msg.ColIndex < 0 || msg.ColIndex >= len(b.columns) {
		return b, b.notifier.Send("invalid column", notifyError)
	}
	col := b.columns[msg.ColIndex]
	if err := col.Rename(msg.NewName); err != nil {
		return b, b.notifier.Send("failed to rename: "+err.Error(), notifyError)
	}
	return b, b.notifier.Send("renamed column "+msg.OldName+" → "+msg.NewName, notifySuccess)
}

func (b *Board) handleDelete(msg deleteConfirmMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	err := col.DeleteItem(msg.FileName)
	if err != nil {
		return b, b.notifier.Send("failed to delete: "+err.Error(), notifyError)
	}
	col.LoadItems()
	return b, b.notifier.Send("deleted "+msg.FileName, notifySuccess)
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
		return b, b.notifier.Send("unknown command: "+string(action), notifyError)
	}
}

func (b *Board) handleQuickCommandKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.QuickCmdCancel):
		b.quickCmdMode = false
		b.quickCmdInput.Blur()
		b.quickCmdInput.SetValue("")
		return b, nil
	case key.Matches(msg, Keys.QuickCmdConfirm):
		b.quickCmdMode = false
		b.quickCmdInput.Blur()
		cmd := strings.TrimSpace(b.quickCmdInput.Value())
		b.quickCmdInput.SetValue("")
		if cmd == "" {
			return b, nil
		}
		return b, func() tea.Msg {
			return quickCommandMsg{Command: cmd}
		}
	}

	ti, cmd := b.quickCmdInput.Update(msg)
	b.quickCmdInput = ti

	buf := b.quickCmdInput.Value()
	// Item-command fast path: first char is an item action, the rest must match
	// a mnemonic. Dispatch on unique resolution.
	if len(buf) >= 1 && isItemCommandAction(buf[0]) {
		action := buf[0]
		suffix := buf[1:]
		if suffix == "" {
			return b, cmd
		}
		if ref, ok := b.refByMnemonic[suffix]; ok {
			b.quickCmdMode = false
			b.quickCmdInput.Blur()
			b.quickCmdInput.SetValue("")
			return b, b.dispatchItemCommand(action, ref)
		}
		for tag := range b.refByMnemonic {
			if strings.HasPrefix(tag, suffix) {
				return b, cmd
			}
		}
		b.quickCmdMode = false
		b.quickCmdInput.Blur()
		b.quickCmdInput.SetValue("")
		return b, b.notifier.Send("no item: "+suffix, notifyError)
	}

	return b, cmd
}

func isItemCommandAction(c byte) bool {
	switch c {
	case 'e', 'a', 'p', 'J', 'c', 'V', 'o', '!', 'd', 'm':
		return true
	}
	return false
}

func (b *Board) openSwitcher() tea.Cmd {
	store, err := recents.Load()
	if err != nil {
		return b.notifier.Send("failed to load recents: "+err.Error(), notifyError)
	}
	removed := store.Prune()
	if removed > 0 {
		_ = store.Save()
	}
	activeAbs, _ := filepath.Abs(b.cfg.Path)
	b.switcher.Open(store.Entries, activeAbs)
	return nil
}

func (b *Board) handleSwitchBoard(msg switchBoardMsg) (tea.Model, tea.Cmd) {
	newCfg, err := config.Load(msg.Path)
	if err != nil {
		return b, b.notifier.Send("failed to load board: "+err.Error(), notifyError)
	}

	if b.watcher != nil {
		_ = b.watcher.Close()
		b.watcher = nil
	}

	b.cfg = newCfg
	b.theme = newCfg.Theme
	b.selectedCol = 0

	if err := b.loadColumns(); err != nil {
		return b, b.notifier.Send("failed to load columns: "+err.Error(), notifyError)
	}

	if paths, err := kbrdfs.DiscoverPaths(b.cfg.Path); err == nil {
		if w, err := kbrdfs.NewWatcher(paths); err == nil {
			b.watcher = w
		}
	}

	store, _ := recents.Load()
	store.Touch(b.cfg.Path, b.cfg.BoardName)
	_ = store.Save()

	label := b.cfg.Path
	if b.cfg.BoardName != "" {
		label = "[" + b.cfg.BoardName + "] " + b.cfg.Path
	}
	return b, tea.Batch(b.watchCmd(), b.notifier.Send("switched to "+label, notifySuccess))
}

func (b *Board) openQuickCommand() tea.Cmd {
	b.quickCmdMode = true
	b.quickCmdInput.SetValue("")
	return b.quickCmdInput.Focus()
}


// dispatchItemCommand runs a single-character item command against an arbitrary
// item identified by ref, regardless of which column is currently selected.
// Used by the mnemonic-driven quick-command path. Returns the tea.Cmd produced
// by the action (or nil) — never changes b.selectedCol so cross-column targeting
// is non-disruptive.
func (b *Board) dispatchItemCommand(action byte, ref itemRef) tea.Cmd {
	if ref.ColIndex < 0 || ref.ColIndex >= len(b.columns) {
		return b.notifier.Send("invalid column", notifyError)
	}
	col := b.columns[ref.ColIndex]
	var item *Item
	for i := range col.Items {
		if col.Items[i].Name == ref.Name {
			it := col.Items[i]
			item = &it
			break
		}
	}
	if item == nil {
		return b.notifier.Send("item not found: "+ref.Name, notifyError)
	}

	switch action {
	case 'e':
		return b.editor.OpenEdit(ref.ColIndex, item.Name, item.FullPath)
	case 'a':
		return b.editor.OpenAppend(ref.ColIndex, item.Name)
	case 'p':
		return b.editor.OpenPrepend(ref.ColIndex, item.Name)
	case 'J':
		return b.editor.OpenJournal(ref.ColIndex, item.Name)
	case 'c':
		content, err := col.CopyContent(item.Name)
		if err != nil {
			return b.notifier.Send("failed to copy: "+err.Error(), notifyError)
		}
		return b.copyToClipboard(content)
	case 'V':
		return b.pasteToItem(ref.ColIndex, item.Name)
	case 'o':
		if err := col.OpenFile(item.Name); err != nil {
			return b.notifier.Send("failed to open: "+err.Error(), notifyError)
		}
		return b.notifier.Send("opened "+item.Name, notifySuccess)
	case '!':
		if err := col.PinItem(item.Name); err != nil {
			return b.notifier.Send("failed to pin: "+err.Error(), notifyError)
		}
		state := "unpinned"
		if !item.Pinned {
			state = "pinned"
		}
		return b.notifier.Send(item.Name+" "+state, notifySuccess)
	case 'd':
		b.dialog.Open("Delete item?", item.Name+".md", []DialogButton{
			{Label: "Yes", Danger: true, Msg: deleteConfirmMsg{ColIndex: ref.ColIndex, FileName: item.Name}},
			{Label: "No", Primary: true},
		})
		b.dialog.selected = 1
		return nil
	case 'm':
		nextCol := (ref.ColIndex + 1) % len(b.columns)
		if err := col.MoveItemTo(b.columns[nextCol], item.Name); err != nil {
			return b.notifier.Send("failed to move: "+err.Error(), notifyError)
		}
		return b.notifier.Send("moved "+item.Name+" → "+b.columns[nextCol].Name, notifySuccess)
	}
	return b.notifier.Send("unknown command: "+string(action), notifyError)
}

func (b *Board) rebuildMnemonics() {
	type cell struct {
		ref itemRef
	}
	var cells []cell
	for ci, col := range b.columns {
		for _, item := range col.VisibleItems() {
			cells = append(cells, cell{ref: itemRef{ColIndex: ci, Name: item.Name}})
		}
	}
	tags := GenerateMnemonics(len(cells))
	b.mnemonicByRef = make(map[itemRef]string, len(cells))
	b.refByMnemonic = make(map[string]itemRef, len(cells))
	max := 0
	for i, c := range cells {
		tag := tags[i]
		b.mnemonicByRef[c.ref] = tag
		b.refByMnemonic[tag] = c.ref
		if len(tag) > max {
			max = len(tag)
		}
	}
	b.mnemonicMaxLen = max
}

func (b *Board) mnemonicLookup(colIdx int) func(name string) string {
	return func(name string) string {
		return b.mnemonicByRef[itemRef{ColIndex: colIdx, Name: name}]
	}
}

func (b *Board) refresh() tea.Cmd {
	return func() tea.Msg {
		if err := b.loadColumns(); err != nil {
			return notifyMsg{Message: "failed to refresh: " + err.Error(), Type: notifyError}
		}
		return notifyMsg{Message: "refreshed", Type: notifySuccess}
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
	return func() tea.Msg {
		if err := clipboard.WriteAll(string(content)); err != nil {
			return notifyMsg{Message: "clipboard not available", Type: notifyError}
		}
		return notifyMsg{Message: "copied to clipboard", Type: notifySuccess}
	}
}

func (b *Board) pasteToItem(colIdx int, fileName string) tea.Cmd {
	return func() tea.Msg {
		text, err := clipboard.ReadAll()
		if err != nil || text == "" {
			return notifyMsg{Message: "clipboard empty or unavailable", Type: notifyError}
		}
		col := b.columns[colIdx]
		if err := col.AppendText(fileName, text); err != nil {
			return notifyMsg{Message: "failed to paste: " + err.Error(), Type: notifyError}
		}
		col.LoadItems()
		return notifyMsg{Message: "pasted to " + fileName, Type: notifySuccess}
	}
}

func (b *Board) renderLogo() string {
	logoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#60a5fa")).
		Italic(true).
		Bold(true)
	versionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#64748b")).
		Italic(true)
	return logoStyle.Render(logoArt) + "  " + versionStyle.Render(Version)
}

// slotWidth is the rendered width of one column cell on the row:
// configured colWidth + 2 for the rounded border + 1 for the right margin.
func (b *Board) slotWidth() int { return b.cfg.ColumnWidth + 3 }

// visibleColRange returns the index of the first column to render and the
// number of columns that fit horizontally. It also adjusts firstVisibleCol so
// the active column is always within the visible window.
func (b *Board) visibleColRange() (first, count int) {
	if len(b.columns) == 0 {
		return 0, 0
	}
	w := b.termWidth
	if w == 0 {
		w = 80
	}
	const indicatorReserve = 6 // room for "◀ N " / " N ▶" chips on either side
	count = (w - indicatorReserve) / b.slotWidth()
	if count < 1 {
		count = 1
	}
	if count > len(b.columns) {
		count = len(b.columns)
	}

	if b.selectedCol < b.firstVisibleCol {
		b.firstVisibleCol = b.selectedCol
	}
	if b.selectedCol >= b.firstVisibleCol+count {
		b.firstVisibleCol = b.selectedCol - count + 1
	}
	maxFirst := len(b.columns) - count
	if maxFirst < 0 {
		maxFirst = 0
	}
	if b.firstVisibleCol > maxFirst {
		b.firstVisibleCol = maxFirst
	}
	if b.firstVisibleCol < 0 {
		b.firstVisibleCol = 0
	}
	return b.firstVisibleCol, count
}

func (b *Board) View() string {
	if len(b.columns) == 0 {
		w, h := b.termWidth, b.termHeight
		if w == 0 {
			w = 80
		}
		if h == 0 {
			h = 24
		}
		if dialogView := b.dialog.View(); dialogView != "" {
			return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, dialogView)
		}
		return "No columns found in " + b.cfg.Path
	}

	// Bail out cleanly on tiny terminals rather than draw a broken layout.
	if b.termWidth > 0 && b.termWidth < b.cfg.ColumnWidth+4 {
		w, h := b.termWidth, b.termHeight
		if h == 0 {
			h = 24
		}
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8")).Render("terminal too small"))
	}
	if b.termHeight > 0 && b.termHeight < 10 {
		w := b.termWidth
		if w == 0 {
			w = 80
		}
		return lipgloss.Place(w, b.termHeight, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8")).Render("terminal too small"))
	}

	b.rebuildMnemonics()

	gap := lipgloss.NewStyle().MarginRight(1)
	gutterW := 2
	if b.mnemonicMaxLen+1 > gutterW {
		gutterW = b.mnemonicMaxLen + 1
	}

	first, count := b.visibleColRange()
	end := first + count
	rendered := make([]string, 0, count+2)

	indicatorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#64748b")).
		Bold(true).
		PaddingTop(1).
		MarginRight(1)
	if first > 0 {
		rendered = append(rendered, indicatorStyle.Render(fmt.Sprintf("◀ %d", first)))
	}
	for i := first; i < end; i++ {
		col := b.columns[i]
		rendered = append(rendered, gap.Render(col.View(i == b.selectedCol, b.mnemonicLookup(i), gutterW)))
	}
	if end < len(b.columns) {
		rendered = append(rendered, indicatorStyle.Render(fmt.Sprintf("%d ▶", len(b.columns)-end)))
	}
	columnsView := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)

	quickCmdView := b.renderQuickCommand()

	result := b.renderLogo() + "\n" + columnsView
	if quickCmdView != "" {
		result += "\n" + quickCmdView
	}
	w, h := b.termWidth, b.termHeight
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}
	if b.helpOpen {
		overlay := RenderHelpOverlay(w, h, GlobalShortcuts(ShortcutContext{
			HasSelectedItem: b.selectedCol < len(b.columns) && b.columns[b.selectedCol].HasSelectedItem(),
		}))
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, overlay)
	}
	dialogView := b.dialog.View()
	if dialogView != "" {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, dialogView)
	}
	editorView := b.renderEditor()
	if editorView != "" {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, editorView)
	}
	if b.peek.Active() {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, b.peek.View(w, h))
	}
	if b.switcher.Active() {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, b.switcher.View())
	}
	result += "\n" + b.renderStatusBar()

	return result
}

func (b *Board) renderStatusBar() string {
	width := b.termWidth
	if width == 0 {
		width = 80
	}

	ctx := ShortcutContext{QuickCmdMode: b.quickCmdMode}
	var primary string
	hasSelected := b.selectedCol < len(b.columns) && b.columns[b.selectedCol].HasSelectedItem()
	ctx.HasSelectedItem = hasSelected

	sepDot := helpSepStyle.Render(" · ")

	switch {
	case b.quickCmdMode:
		primary = helpTitleStyle.Render("⏵ command")
	case b.selectedCol < len(b.columns):
		col := b.columns[b.selectedCol]
		mode := helpTitleStyle.Render("⏵ board")
		boardLabel := helpLabelStyle.Render("board: ") + helpKeyStyle.Render(b.boardLabel())
		colLabel := helpLabelStyle.Render("column: ") + helpKeyStyle.Render(col.Name)
		colPos := helpLabelStyle.Render("col ") + helpKeyStyle.Render(fmt.Sprintf("%d/%d", b.selectedCol+1, len(b.columns)))
		count := helpLabelStyle.Render(itemCountLabel(col.TotalCount()))
		primary = mode + sepDot + boardLabel + sepDot + colLabel + sepDot + colPos + sepDot + count
	default:
		primary = helpTitleStyle.Render("⏵ board") + sepDot + helpLabelStyle.Render("board: ") + helpKeyStyle.Render(b.boardLabel())
	}

	secondary := RenderInlineHints(ContextShortcuts(ctx))

	lineStyle := lipgloss.NewStyle().Width(width).Align(lipgloss.Center)
	return lineStyle.Render(primary) + "\n" + lineStyle.Render(secondary)
}

func (b *Board) boardLabel() string {
	if b.cfg.BoardName != "" {
		return "[" + b.cfg.BoardName + "] " + filepath.Base(b.cfg.Path)
	}
	return filepath.Base(b.cfg.Path)
}

func itemCountLabel(n int) string {
	if n == 1 {
		return "1 item"
	}
	return strconv.Itoa(n) + " items"
}

func (b *Board) renderQuickCommand() string {
	if !b.quickCmdMode {
		return ""
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Padding(0, 1)
	return box.Render(b.quickCmdInput.View())
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
