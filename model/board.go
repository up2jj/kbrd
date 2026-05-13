package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"kbrd/config"
	kbrdfs "kbrd/fs"
)

type watchMsg struct{}

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
	selectedCol   int
	quitting      bool
	editor        *Editor
	notifier      *Notifier
	quickCmdMode  bool
	quickCmdInput textinput.Model
	theme         string
	watcher       *kbrdfs.Watcher
	dialog        Dialog
	helpOpen      bool
	peek          Peek

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
		notifier:      NewNotifier(),
		quickCmdInput: ti,
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
		b.termWidth = msg.Width
		b.termHeight = msg.Height
		b.visibleHeight = msg.Height - 4
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
		switch msg.String() {
		case "?", "esc", "ctrl+c":
			b.helpOpen = false
			if msg.String() == "ctrl+c" {
				b.quitting = true
				return b, tea.Quit
			}
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

	switch msg.String() {
	case "ctrl+c":
		b.quitting = true
		return b, tea.Quit
	case "?":
		b.helpOpen = true
		return b, nil
	case ".":
		return b, b.openQuickCommand()
	case "t":
		b.toggleTheme()
		return b, nil
	case "f5":
		return b, b.refresh()
	case "r":
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			return b, b.editor.OpenRenameItem(b.selectedCol, item.Name)
		}
	case "R":
		return b, b.editor.OpenRenameColumn(b.selectedCol, col.Name)
	case "[", "shift+tab":
		b.selectedCol--
		if b.selectedCol < 0 {
			b.selectedCol = len(b.columns) - 1
		}
	case "]", "tab":
		b.selectedCol++
		if b.selectedCol >= len(b.columns) {
			b.selectedCol = 0
		}
	case "e":
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			return b, b.editor.OpenEdit(b.selectedCol, item.Name, item.FullPath)
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
				return b, b.notifier.Send("failed to copy: "+err.Error(), notifyError)
			}
			return b, b.copyToClipboard([]byte(content))
		}
	case "V":
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			return b, b.pasteToItem(b.selectedCol, item.Name)
		}
	case "o":
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			err := col.OpenFile(item.Name)
			if err != nil {
				return b, b.notifier.Send("failed to open: "+err.Error(), notifyError)
			}
			return b, b.notifier.Send("opened "+item.Name, notifySuccess)
		}
	case "!":
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
				return b, b.notifier.Send("failed to move: "+err.Error(), notifyError)
			}
			b.selectedCol = nextCol
		}
	case " ":
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			content, err := col.CopyContent(item.Name)
			if err != nil {
				return b, b.notifier.Send("failed to peek: "+err.Error(), notifyError)
			}
			return b, b.peek.Open(item.Name, string(content), b.termWidth)
		}
		return b, nil
	case "/":
		col.list.SetShowFilter(true)
		return b, col.UpdateList(msg)
	case "n":
		return b, b.editor.OpenNew(b.selectedCol, b.columns[b.selectedCol].Name)
	case "N":
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
	switch msg.String() {
	case "esc":
		b.quickCmdMode = false
		b.quickCmdInput.Blur()
		b.quickCmdInput.SetValue("")
		return b, nil
	case "enter":
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

func (b *Board) View() string {
	if len(b.columns) == 0 {
		return "No columns found in " + b.cfg.Path
	}

	b.rebuildMnemonics()

	gap := lipgloss.NewStyle().MarginRight(1)
	rendered := make([]string, len(b.columns))
	gutterW := 2
	if b.mnemonicMaxLen+1 > gutterW {
		gutterW = b.mnemonicMaxLen + 1
	}
	for i, col := range b.columns {
		rendered[i] = gap.Render(col.View(i == b.selectedCol, b.mnemonicLookup(i), gutterW))
	}
	columnsView := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)

	quickCmdView := b.renderQuickCommand()

	result := columnsView
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
		colLabel := helpLabelStyle.Render("column: ") + helpKeyStyle.Render(col.Name)
		count := helpLabelStyle.Render(itemCountLabel(col.TotalCount()))
		primary = mode + sepDot + colLabel + sepDot + count
	default:
		primary = helpTitleStyle.Render("⏵ board")
	}

	secondary := RenderInlineHints(ContextShortcuts(ctx))

	lineStyle := lipgloss.NewStyle().Width(width).Align(lipgloss.Center)
	return lineStyle.Render(primary) + "\n" + lineStyle.Render(secondary)
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
