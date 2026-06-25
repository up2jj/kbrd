package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"

	"kbrd/board"
	"kbrd/config"
	"kbrd/events"
	kbrdfs "kbrd/fs"
	"kbrd/git"
	"kbrd/script"
)

var Version = "dev"

// watchStartMsg is emitted once after Init's startup load completes. Columns
// and git stats are already populated; this just publishes the initial refresh
// and arms the watcher loop on the UI goroutine.
type watchStartMsg struct{}

// watchEventMsg carries a single fsnotify event out of the watcher loop. Path
// is ev.Name (empty for watcher errors, which force a full reload). Like
// search's typing events, these are debounced before any disk work runs.
type watchEventMsg struct{ Path string }

// watchDebounceMsg fires after the watcher debounce window. The board runs the
// reload only if Seq still matches b.watchSeq (no newer event has arrived).
type watchDebounceMsg struct{ Seq int }

// refreshedMsg fires after a manual refresh's off-goroutine loadColumns; its
// handler applies the column_items transform on the UI goroutine and notifies.
type refreshedMsg struct{}

// boardReloadedMsg carries the result of an off-goroutine full board rescan.
// Stale results (Seq != b.watchSeq) are discarded.
type boardReloadedMsg struct {
	Seq     int
	columns []*Column
}

// columnReloadedMsg carries the result of an off-goroutine single-column
// rescan, used when a change is local to one column. Stale results are
// discarded.
type columnReloadedMsg struct {
	Seq  int
	path string
	col  *Column
}

type initBoardRequestMsg struct{}
type initBoardConfirmMsg struct{}
type initBoardDeclineMsg struct{}

type Board struct {
	cfg             config.Config
	columns         []*Column
	visibleHeight   int
	termWidth       int
	termHeight      int
	selectedCol     int
	firstVisibleCol int
	quitting        bool
	shuttingDown    bool // waiting for an in-flight git sync before quitting
	editor          *Editor
	notifier        *Notifier
	quickCmdMode    bool
	quickCmdInput   textinput.Model
	theme           string
	palette         Palette
	watcher         *kbrdfs.Watcher
	dialog          Dialog
	helpMenu        HelpMenu
	templateMenu    TemplateMenu
	configMenuOpen  bool
	peek            Peek
	zoom            Zoom
	switcher        Switcher
	search          Search
	git             git.Controller
	zellij          Zellij
	customCmds      CustomCommandMenu
	commands        []config.Command
	commandWarnings []config.CommandLoadWarning
	presenter       boardPresenter
	cells           CellBar
	indicators      colIndicators // script-set per-column header labels (kbrd.column.indicator), keyed by column name
	mcpStatus       MCPStatus     // drives the header MCP chip (off / running / failed-to-bind)

	asyncInflight int // count of kbrd.async.run jobs currently running

	// lineApplyPending marks a Lua line command in flight: when its coroutine
	// completes (possibly after kbrd.ui.* yields), handleScriptResult drains the
	// return value and splices it into the editor. lineApplyRow is the row the
	// command was dispatched from, captured so the result lands there even if the
	// cursor moved while the command (which may have opened a UI prompt) ran.
	lineApplyPending bool
	lineApplyRow     int

	scriptStatus    string // transient kbrd.status message shown in the status bar
	scriptStatusSeq int    // bumped per kbrd.status; guards stale expiry ticks

	bus      events.Bus
	scripts  *script.Host
	scriptUI ScriptUI
	// transformPending marks a column_items transform that was skipped because
	// a script was mid-run; drainColumnTransform re-applies it once idle.
	transformPending bool
	templateFlow     TemplateFlow
	templateExec     templateExec
	frontmatterEdit  FrontmatterEditor
	hooks            *hookRunner // declarative YAML event hooks; nil when disabled

	// virtualCols are script-supplied columns (kbrd.column.set), kept in a
	// registry separate from the filesystem columns so they survive disk
	// reloads. They are appended to the tail of b.columns after every
	// filesystem (re)build. Keyed implicitly by Column.VID.
	virtualCols []*Column

	// watch debounce state — every raw fsnotify event bumps watchSeq and
	// records its path in watchDirty; a debounce tick that still matches
	// watchSeq triggers one coalesced reload (mirrors search's Seq guard).
	watchSeq   int
	watchDirty map[string]struct{}
	// changes detects per-item content changes across a watcher reload and is
	// the source of the item_changed event — see item_changes.go.
	changes changeTracker

	// mnemonic state — rebuilt whenever the visible item set changes
	mnemonicByRef  map[itemRefStable]string
	refByMnemonic  map[string]itemRefStable
	mnemonicMaxLen int
}

func NewBoard(cfg config.Config) *Board {
	palette := PaletteFor(cfg.Theme)
	ti := textinput.New()
	ti.Prompt = ": "
	ti.Placeholder = "e.g. e<tag> edit, d<tag> delete, r refresh — enter to run"
	ti.CharLimit = 64
	ti.Width = 60
	applyInputPalette(&ti, palette)
	b := &Board{
		cfg:           cfg,
		visibleHeight: 20,
		editor:        NewEditor(cfg.Editor.Vim),
		notifier:      NewNotifier(cfg.NotifyBackend),
		quickCmdInput: ti,
		theme:         cfg.Theme,
		palette:       palette,
		zellij:        NewZellij(),
	}
	b.cells = CellBar{cells: make(map[int]*Cell), palette: &b.palette}
	b.templateExec.notifier = b.notifier
	b.initGit()
	b.applyPalette()
	b.initScripting()
	b.loadCommands()
	boardHooks{board: b}.init()
	b.editorEval().wireCompletions()
	return b
}

// MCPStatus is the built-in MCP server's state as reflected in the header chip.
type MCPStatus int

const (
	MCPOff     MCPStatus = iota // not requested (no --mcp and config disabled)
	MCPRunning                  // listener bound and serving
	MCPFailed                   // requested but the listener could not bind (e.g. port in use)
)

// SetMCPStatus records the MCP server's outcome so the header strip can show a
// truthful chip. Called from main once startMCP has reported back, before the
// program loop starts.
func (b *Board) SetMCPStatus(s MCPStatus) { b.mcpStatus = s }

// initGit (re)builds the git controller for the current b.cfg. Called from
// NewBoard and on every board switch (loadBoard), mirroring initScripting, so
// the controller never holds a stale board's config/repo. The injected closures
// capture b (not cfg), so they read the live config at call time. &b.bus is
// stable across initScripting's bus reset (same field address). BeforeCommit
// lets git regenerate the README from board content without git knowing what a
// board is; EditorActive lets automatic sync pause while an editor is open;
// OnSyncSettled lets a deferred quit complete once a sync finishes.
func (b *Board) initGit() {
	b.git = git.New(git.Deps{
		Cfg:      b.cfg,
		Notifier: gitNotifier{b.notifier},
		Bus:      &b.bus,
		BeforeCommit: func() error {
			if b.cfg.GitGenerateReadme {
				return b.writeBoardReadme()
			}
			return nil
		},
		EditorActive: func() bool {
			return b.editor != nil && b.editor.state != editorNone
		},
		OnSyncSettled: func() tea.Cmd { b.quitting = true; return tea.Quit },
	})
}

// gitNotifier adapts the board's Notifier to the git package's narrow interface.
type gitNotifier struct{ n *Notifier }

func (g gitNotifier) Success(msg string) tea.Cmd { return g.n.Success(msg) }
func (g gitNotifier) Error(msg string) tea.Cmd   { return g.n.Error(msg) }

// applyInputPalette restyles a bubbles textinput using the palette colors.
// Reused by Board, GitPanel, and ScriptUI which all share the same look.
func applyInputPalette(ti *textinput.Model, p Palette) {
	ti.PromptStyle = lipgloss.NewStyle().Foreground(p.Primary).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(p.FgBase)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(p.FgDim).Italic(true)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(p.Highlight)
}

// applyPalette propagates the current palette to all sub-models and restyles
// any pre-built input widgets. Call after b.palette or b.theme changes.
func (b *Board) applyPalette() {
	b.palette = PaletteFor(b.theme)
	applyPackageStyles(b.palette)
	applyInputPalette(&b.quickCmdInput, b.palette)
	b.dialog.palette = b.palette
	b.peek.palette = b.palette
	b.switcher.palette = b.palette
	b.search.palette = b.palette
	b.git.SetPalette(b.palette)
	b.zellij.SetPalette(b.palette)
	b.customCmds.palette = b.palette
	b.scriptUI.SetPalette(b.palette)
	b.templateFlow.SetPalette(b.palette)
	b.templateMenu.SetPalette(b.palette)
	b.frontmatterEdit.SetPalette(b.palette)
	b.helpMenu.SetPalette(b.palette)
	if b.editor != nil {
		b.editor.palette = b.palette
	}
	for _, c := range b.columns {
		c.palette = b.palette
	}
}

func (b *Board) Init() tea.Cmd {
	startup := func() tea.Msg {
		// Repair any {{shell}} markers left pending by a prior interrupted
		// session before reading cards, so they load as the interrupted note.
		b.templateExec.recover(b.cfg.Path)
		if err := b.loadColumns(); err != nil {
			return notifyMsg{Message: "failed to load columns: " + err.Error(), Type: notifyError}
		}
		b.git.Detect()
		paths, err := board.DiscoverPaths(b.cfg.Path)
		if err == nil {
			if w, err := kbrdfs.NewWatcher(paths); err == nil {
				b.watcher = w
			}
		}
		if len(b.columns) == 0 {
			return initBoardRequestMsg{}
		}
		return watchStartMsg{}
	}
	cmds := []tea.Cmd{startup}
	if c := b.zellij.StartCmd(b.cfg.BoardName, b.cfg.Path); c != nil {
		cmds = append(cmds, c)
	}
	if len(b.commandWarnings) > 0 {
		first := b.commandWarnings[0]
		extra := ""
		if n := len(b.commandWarnings) - 1; n > 0 {
			extra = fmt.Sprintf(" (+%d more — press x for details)", n)
		}
		cmds = append(cmds, b.notifier.Error("commands: "+first.Message+extra))
	}
	if c := b.git.StartAutoSync(); c != nil {
		cmds = append(cmds, c)
	}
	// Pick up any timers and async work scheduled at the top level of
	// init.lua, so the first tick / first goroutine fires without needing
	// a user command to drive it.
	if c := b.collectTimerCmds(); c != nil {
		cmds = append(cmds, c)
	}
	if c := b.collectAsyncCmds(); c != nil {
		cmds = append(cmds, c)
	}
	if len(cmds) == 1 {
		return startup
	}
	return tea.Batch(cmds...)
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
		for {
			select {
			case ev, ok := <-b.watcher.Events():
				if !ok {
					return nil
				}
				if ignoreWatchEvent(ev) {
					continue
				}
				return watchEventMsg{Path: ev.Name}
			case _, ok := <-b.watcher.Errors():
				if !ok {
					return nil
				}
				return watchEventMsg{Path: ""}
			}
		}
	}
}

func ignoreWatchEvent(ev fsnotify.Event) bool {
	// Chmod-only events fire on atime updates (e.g. when git diff reads tracked
	// files). Ignore them — we only care about real content changes.
	if ev.Op == fsnotify.Chmod {
		return true
	}
	// Vim-mode crash-recovery sidecars are written while typing. They are not
	// board content, and treating them as changes causes board_refresh hooks
	// (notably async virtual-column scans) to run on every editor keystroke.
	return strings.HasSuffix(filepath.Base(ev.Name), ".kbrd-swap")
}

// buildColumns reads the board's columns and items from disk and returns fresh
// Column values. It takes everything by value and writes no Board state, so it
// is safe to run inside a tea.Cmd goroutine. Height and selection are applied
// by the caller on the UI goroutine.
func buildColumns(cfg config.Config, palette Palette, cache itemCache) ([]*Column, error) {
	names, err := board.Columns(cfg.Path)
	if err != nil {
		return nil, err
	}

	columns := make([]*Column, 0, len(names))
	for _, name := range names {
		col := buildColumn(filepath.Join(cfg.Path, name), name, cfg, palette, cache)
		if col == nil {
			continue
		}
		columns = append(columns, col)
	}
	return columns, nil
}

// buildColumn loads a single column's items from disk, returning nil if the
// directory cannot be read. Safe to call off the UI goroutine. cache lets
// unchanged files skip re-reads (see Column.loadItems); it may be nil for a
// cold load.
func buildColumn(path, name string, cfg config.Config, palette Palette, cache itemCache) *Column {
	// Read up to the layout ceiling so zoomed cards have their extra preview
	// lines in memory without re-reading files on mode changes (bounded prefix
	// read — the extra lines are nearly free).
	col := NewColumn(name, path, ItemOptions{
		PreviewLines:     max(cfg.PreviewLines, maxPreviewLines()),
		TitleFromHeading: cfg.TitleFromHeading,
	})
	col.palette = palette
	if err := col.loadItems(cache); err != nil {
		return nil
	}
	col.RestoreCollapsed()
	return col
}

// loadColumns rebuilds b.columns synchronously. Used during startup and board
// init, where it runs in Init's own goroutine before the Update loop starts.
// Watcher-driven reloads use the async tea.Cmd path instead.
func (b *Board) loadColumns() error {
	columns, err := buildColumns(b.cfg, b.palette, b.itemsByPath())
	if err != nil {
		return err
	}
	for _, col := range columns {
		if b.visibleHeight > 0 {
			col.SetHeight(b.visibleHeight)
		}
	}
	b.columns = columns
	b.appendVirtualColumns()
	if len(b.columns) > 0 && b.selectedCol >= len(b.columns) {
		b.selectedCol = len(b.columns) - 1
	}
	return nil
}

// appendVirtualColumns re-attaches the registered virtual columns to the tail of
// b.columns after a filesystem (re)build, applying current height and palette.
// Virtual columns are script state, not filesystem state, so they persist across
// reloads.
func (b *Board) appendVirtualColumns() {
	for _, vc := range b.virtualCols {
		if b.visibleHeight > 0 {
			vc.SetHeight(b.visibleHeight)
		}
		vc.palette = b.palette
		b.columns = append(b.columns, vc)
	}
}

// virtualColumn returns the registered virtual column with the given id, or nil.
func (b *Board) virtualColumn(vid string) *Column {
	for _, c := range b.virtualCols {
		if c.VID == vid {
			return c
		}
	}
	return nil
}

// setVirtualColumn creates or updates a virtual column from a script push. A new
// column is appended to both the registry and the live b.columns tail; an
// existing one is updated in place (it is already attached).
func (b *Board) setVirtualColumn(id string, spec events.VirtualColumnSpec) {
	if col := b.virtualColumn(id); col != nil {
		col.ApplyVirtualSpec(spec)
		return
	}
	col := NewVirtualColumn(id, spec.Name, b.palette)
	if b.visibleHeight > 0 {
		col.SetHeight(b.visibleHeight)
	}
	col.ApplyVirtualSpec(spec)
	b.virtualCols = append(b.virtualCols, col)
	b.columns = append(b.columns, col)
}

// clearVirtualColumn removes a single virtual column by id from both the
// registry and the live column slice.
func (b *Board) clearVirtualColumn(id string) {
	b.virtualCols = dropColumnByVID(b.virtualCols, id)
	b.columns = dropColumnByVID(b.columns, id)
	b.clampSelectedCol()
}

// clearAllVirtualColumns removes every virtual column from both slices.
func (b *Board) clearAllVirtualColumns() {
	b.virtualCols = nil
	kept := b.columns[:0]
	for _, c := range b.columns {
		if !c.Virtual {
			kept = append(kept, c)
		}
	}
	b.columns = kept
	b.clampSelectedCol()
}

// dropColumnByVID returns cols without the virtual column whose VID matches id.
func dropColumnByVID(cols []*Column, id string) []*Column {
	out := cols[:0]
	for _, c := range cols {
		if c.Virtual && c.VID == id {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (b *Board) clampSelectedCol() {
	if b.selectedCol >= len(b.columns) {
		b.selectedCol = len(b.columns) - 1
	}
	if b.selectedCol < 0 {
		b.selectedCol = 0
	}
}

// debouncedReload runs when a watcher debounce tick fires. It drops stale ticks
// (a newer event has since bumped watchSeq) and otherwise launches one async
// reload, scoped to a single column when every changed path lives in it.
func (b *Board) debouncedReload(seq int) tea.Cmd {
	if seq != b.watchSeq {
		return nil // stale — a newer event scheduled a later tick
	}
	dirty := b.watchDirty
	b.watchDirty = nil
	b.changes.snapshot(dirty, b.columns)
	if colPath := b.lifecycle().singleDirtyColumn(dirty); colPath != "" {
		return b.reloadColumnCmd(seq, colPath)
	}
	return b.reloadCmd(seq)
}

// reloadCmd builds a full board rescan off the UI goroutine. It captures config
// and palette by value so it touches no Board state; the result is applied by
// the boardReloadedMsg handler.
func (b *Board) reloadCmd(seq int) tea.Cmd {
	cfg := b.cfg
	palette := b.palette
	// Snapshot current items by value on the UI goroutine; the closure only
	// reads it. Preview slices are shared but only ever reassigned (never
	// mutated in place) by NewItem/Refresh, so the concurrent read is safe.
	cache := b.itemsByPath()
	return func() tea.Msg {
		columns, err := buildColumns(cfg, palette, cache)
		if err != nil {
			return nil // leave the board as-is, matching the old silent path
		}
		return boardReloadedMsg{Seq: seq, columns: columns}
	}
}

// reloadColumnCmd rebuilds a single column off the UI goroutine. Git stats are
// board-wide, so they are refreshed too (a single edit can change one file's
// diff). Result is applied by the columnReloadedMsg handler.
func (b *Board) reloadColumnCmd(seq int, colPath string) tea.Cmd {
	cfg := b.cfg
	palette := b.palette
	name := filepath.Base(colPath)
	cache := b.itemsByPath()
	return func() tea.Msg {
		col := buildColumn(colPath, name, cfg, palette, cache)
		if col == nil {
			return nil
		}
		return columnReloadedMsg{Seq: seq, path: colPath, col: col}
	}
}

// itemsByPath snapshots every column's items into a reload cache keyed by
// FullPath (unique across columns), so a rebuild can skip re-reading unchanged
// files.
func (b *Board) itemsByPath() itemCache {
	cache := make(itemCache)
	for _, col := range b.columns {
		for _, it := range col.Items {
			cache[it.FullPath] = it
		}
	}
	return cache
}

func (b *Board) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if b.quitting {
		return b, nil
	}
	prevCol, prevItem := b.snapshotSelection()
	model, cmd := b.updateInner(msg)
	b.emitSelectionChanges(prevCol, prevItem)
	return model, b.lifecycle().DrainPostUpdate(cmd)
}

// batchCmd combines two tea.Cmds, tolerating nil in either slot.
func batchCmd(a, b tea.Cmd) tea.Cmd {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	default:
		return tea.Batch(a, b)
	}
}

// updateInner is the original Update body. Wrapped by Update so that hooks
// fired anywhere along the call path get their timer side-effects scheduled.
func (b *Board) updateInner(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.termWidth = msg.Width
		b.termHeight = msg.Height
		b.git.SetSize(msg.Width, msg.Height)
		b.visibleHeight = max(msg.Height-11, 1)
		for _, col := range b.columns {
			col.SetHeight(b.visibleHeight)
		}
		b.editor.SetTermSize(b.termWidth, b.termHeight)
		b.templateFlow.SetSize(b.termWidth, b.termHeight)
		b.frontmatterEdit.SetSize(b.termWidth, b.termHeight)
		return b, nil

	case tea.KeyMsg:
		return b.inputRouter().HandleKey(msg)

	case tea.MouseMsg:
		return b.mouseRouter().HandleMouse(msg)

	case notifyMsg:
		return b, b.notifier.Send(msg.Message, msg.Type)

	case watchStartMsg:
		return b.lifecycle().HandleWatchStart()

	case watchEventMsg:
		return b.lifecycle().HandleWatchEvent(msg)

	case watchDebounceMsg:
		return b.lifecycle().HandleWatchDebounce(msg)

	case refreshedMsg:
		b.applyColumnTransforms()
		return b, b.notifier.Success("refreshed")

	case boardReloadedMsg:
		return b.lifecycle().HandleBoardReloaded(msg)

	case columnReloadedMsg:
		return b.lifecycle().HandleColumnReloaded(msg)

	case initBoardRequestMsg:
		b.dialog.Open(DialogOptions{
			Title: "Initialize kanban board?",
			Body:  "Create default columns in " + b.cfg.Path + ":\n1. TO DO  •  2. IN PROGRESS  •  3. DONE",
			Buttons: []DialogButton{
				{Label: "Create", Kind: ButtonPrimary, Msg: initBoardConfirmMsg{}},
				{Label: "Quit", Msg: initBoardDeclineMsg{}},
			},
			DefaultIndex: 0,
		})
		return b, nil

	case initBoardConfirmMsg:
		if err := b.createDefaultColumns(); err != nil {
			return b, b.notifier.ErrorCause("failed to create columns", err)
		}
		if err := b.loadColumns(); err != nil {
			return b, b.notifier.ErrorCause("failed to load columns", err)
		}
		return b, tea.Batch(b.notifier.Success("created default columns"), b.watchCmd())

	case initBoardDeclineMsg:
		b.quitting = true
		return b, tea.Quit

	case editorSaveMsg:
		return b.mutationHandlers().handleSave(msg)

	case managedFileSaveMsg:
		return b.mutationHandlers().handleManagedFileSave(msg)

	case editorAppendMsg:
		return b.mutationHandlers().handleAppend(msg)

	case editorPrependMsg:
		return b.mutationHandlers().handlePrepend(msg)

	case editorJournalMsg:
		return b.mutationHandlers().handleJournal(msg)

	case editorNewMsg:
		return b.mutationHandlers().handleNew(msg)

	case deleteConfirmMsg:
		return b.mutationHandlers().handleDelete(msg)

	case templateRemoveConfirmMsg:
		return b.mutationHandlers().handleTemplateRemoveConfirm(msg)

	case pasteRequestMsg:
		return b, b.pasteActions().pasteToItem(msg)

	case pasteDoneMsg:
		return b.pasteActions().handleDone(msg)

	case renameItemRequestMsg:
		return b.mutationHandlers().handleRenameItemRequest(msg)

	case renameColumnRequestMsg:
		return b.mutationHandlers().handleRenameColumnRequest(msg)

	case renameItemConfirmMsg:
		return b.mutationHandlers().handleRenameItemConfirm(msg)

	case renameColumnConfirmMsg:
		return b.mutationHandlers().handleRenameColumnConfirm(msg)

	case editorConfirmDiscardMsg:
		b.dialog.OpenConfirmDestructive("Discard unsaved changes?", "Your edits will be lost.", "Discard", editorDiscardMsg{})
		return b, nil

	case editorDiscardMsg:
		// An explicit "Discard" throws the edits away on purpose, so remove the
		// crash-recovery swap too — otherwise the discarded content would be
		// offered for recovery the next time this card is opened. (No-op for the
		// textarea path, which never sets a swap file.)
		b.editor.clearSwap()
		b.editor.Close()
		b.resetEditor()
		return b, nil

	case editorEvalMsg:
		return b.editorEval().handle(msg)

	case recoverEditorMsg:
		return b.editorRecovery().handleRecoverEditor(msg)

	case recoverApplyMsg:
		return b.editorRecovery().handleRecoverApply(msg)

	case recoverDiscardMsg:
		return b.editorRecovery().handleRecoverDiscard()

	case quitConfirmedMsg:
		b.editor.Close()
		b.resetEditor()
		return b.finishShutdown()

	case quickCommandMsg:
		return b.quickCommands().handleCommand(msg)

	case switchBoardMsg:
		return b.session().handleSwitchBoard(msg)

	case pinBoardMsg:
		return b.session().handlePinBoard(msg)

	case removeBoardMsg:
		return b.session().handleRemoveBoard(msg)

	case searchMsg:
		// Search owns its async lifecycle (debounce + ripgrep); route opaquely,
		// the same way git.Msg is handled below.
		return b, b.search.Update(msg)

	case searchSelectMsg:
		return b.searchActions().activateFile(msg.BoardPath, msg.FilePath)

	case runCustomCommandMsg:
		return b.handleRunCustomCommand(msg)

	case openLineCommandsMsg:
		return b, b.lineCommands().open(msg)

	case runLineCommandMsg:
		return b.lineCommands().handleRun(msg)

	case lineShellDoneMsg:
		return b.lineCommands().handleShellDone(msg)

	case customCommandFinishedMsg:
		return b.handleCustomCommandFinished(msg)

	case scriptResumeMsg:
		return b.handleScriptResume(msg)

	case scriptTimerMsg:
		return b.handleScriptTimer(msg)

	case scriptStatusExpireMsg:
		return b.handleScriptStatusExpire(msg)

	case scriptAsyncDoneMsg:
		return b.handleScriptAsyncDone(msg)

	case hookDoneMsg:
		return boardHooks{board: b}.handleDone(msg)

	case git.Msg:
		// All git orchestration lives in the git package; route opaquely. Git
		// signals a deferred-quit completion itself via OnSyncSettled, so there
		// is no shutdown bookkeeping here.
		return b, b.git.Update(msg)

	case zellijDoneMsg:
		return b, b.zellij.Done(msg, b.notifier)

	case templateSubmitMsg:
		return b.mutationHandlers().handleTemplateSubmit(msg)

	case templateAuthorSubmitMsg:
		return b.mutationHandlers().handleTemplateAuthorSubmit(msg)

	case createEmptyItemMsg:
		return b.mutationHandlers().handleCreateEmptyItem(msg)

	case frontmatterSubmitMsg:
		return b.frontmatterActions().handleSubmit(msg)

	case templateShellDoneMsg:
		return b, b.templateExec.done(msg)

	default:
		// An active huh form drives itself with internal messages (cursor
		// blink, group transitions); route them to it ahead of the column
		// list so they don't leak into the list filter.
		if b.templateFlow.Active() {
			return b, b.templateFlow.Update(msg)
		}
		if b.frontmatterEdit.Active() {
			return b, b.frontmatterEdit.Update(msg)
		}
		// Pass list-internal messages (e.g. FilterMatchesMsg) to the active column
		if len(b.columns) > 0 {
			return b, b.columns[b.selectedCol].UpdateList(msg)
		}
	}

	return b, nil
}

// Close releases resources owned by the Board. Safe to call once after the
// Bubble Tea program returns. Idempotent.
func (b *Board) Close() {
	if b.watcher != nil {
		_ = b.watcher.Close()
		b.watcher = nil
	}
	if b.scripts != nil {
		b.scripts.Close()
		b.scripts = nil
	}
}

// beginShutdown is the entry point for every quit trigger. Guards unsaved
// editor changes before proceeding to finishShutdown.
func (b *Board) beginShutdown() (tea.Model, tea.Cmd) {
	if b.editor.IsDirty() {
		b.dialog.OpenConfirmDestructive(
			"Quit with unsaved changes?", "Your edits will be lost.",
			"Quit", quitConfirmedMsg{})
		return b, nil
	}
	return b.finishShutdown()
}

// finishShutdown defers the actual exit until an in-flight git sync completes,
// so a push isn't interrupted. A second Ctrl+C while waiting force-quits.
func (b *Board) finishShutdown() (tea.Model, tea.Cmd) {
	if b.git.RequestShutdown() {
		b.shuttingDown = true
		return b, nil
	}
	b.quitting = true
	return b, tea.Quit
}

// resetEditor replaces the editor with a fresh instance, re-seeding it with the
// current palette and terminal size. The size matters because applySize() falls
// back to a fixed default when termWidth/termHeight are 0, which would otherwise
// make expand/collapse (ctrl+e) a no-op until the next terminal resize.
func (b *Board) resetEditor() {
	b.editor = NewEditor(b.cfg.Editor.Vim)
	b.editor.palette = b.palette
	b.editor.SetTermSize(b.termWidth, b.termHeight)
	b.editorEval().wireCompletions()
}

func (b *Board) handleBoardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(b.columns) == 0 {
		return b, nil
	}

	col := b.columns[b.selectedCol]
	if col.IsFiltering() {
		return b, col.UpdateList(msg)
	}
	if col.Virtual {
		if cmd, handled := b.virtualCommands().handleKey(msg, col); handled {
			return b, cmd
		}
	}
	if m, cmd, handled := b.handleGlobalBoardKey(msg, col); handled {
		return m, cmd
	}
	if m, cmd, handled := b.handleColumnBoardKey(msg, col); handled {
		return m, cmd
	}
	if m, cmd, handled := b.handleItemBoardKey(msg, col); handled {
		return m, cmd
	}
	return b.handleListBoardKey(msg, col)
}

func (b *Board) View() string {
	return boardViewFrame{b: b}.render()
}

func (b *Board) boardLabel() string {
	if b.cfg.BoardName != "" {
		return "[" + b.cfg.BoardName + "] " + filepath.Base(b.cfg.Path)
	}
	return filepath.Base(b.cfg.Path)
}

type quickCommandMsg struct {
	Command string
}
