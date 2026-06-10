package model

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"

	"kbrd/board"
	"kbrd/config"
	"kbrd/events"
	kbrdfs "kbrd/fs"
	"kbrd/git"
	"kbrd/recents"
	"kbrd/script"
	"kbrd/template"
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

type pasteMode int

const (
	pasteAtEnd pasteMode = iota
	pasteAtStart
	pasteReplace
	pasteJournal
)

type pasteRequestMsg struct {
	ColIndex int
	FileName string
	Mode     pasteMode
}

type initBoardRequestMsg struct{}
type initBoardConfirmMsg struct{}
type initBoardDeclineMsg struct{}

type itemRef struct {
	ColIndex int
	Name     string
}

type Board struct {
	cfg                config.Config
	columns            []*Column
	visibleHeight      int
	termWidth          int
	termHeight         int
	selectedCol        int
	firstVisibleCol    int
	quitting           bool
	shuttingDown       bool // waiting for an in-flight git sync before quitting
	editor             *Editor
	notifier           *Notifier
	quickCmdMode       bool
	quickCmdInput      textinput.Model
	theme              string
	palette            Palette
	watcher            *kbrdfs.Watcher
	dialog             Dialog
	helpOpen           bool
	configMenuOpen     bool
	peek               Peek
	zoom               Zoom
	switcher           Switcher
	search             Search
	git                git.Controller
	zellij             Zellij
	customCmds         CustomCommandMenu
	commands           []config.Command
	commandWarnings    []config.CommandLoadWarning
	leftIndicatorWidth int
	logoHeight         int
	cells              CellBar
	mcpStatus          MCPStatus // drives the header MCP chip (off / running / failed-to-bind)

	asyncInflight int // count of kbrd.async.run jobs currently running

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
	mnemonicByRef  map[itemRef]string
	refByMnemonic  map[string]itemRef
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
		editor:        NewEditor(),
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
	b.initHooks()
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
// board is; OnSyncSettled lets a deferred quit complete once a sync finishes.
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
		OnSyncSettled: func() tea.Cmd { b.quitting = true; return tea.Quit },
	})
}

// gitNotifier adapts the board's Notifier to the git package's narrow interface.
type gitNotifier struct{ n *Notifier }

func (g gitNotifier) Success(msg string) tea.Cmd { return g.n.Send(msg, notifySuccess) }
func (g gitNotifier) Error(msg string) tea.Cmd   { return g.n.Send(msg, notifyError) }

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
		cmds = append(cmds, b.notifier.Send("commands: "+first.Message+extra, notifyError))
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
				// Chmod-only events fire on atime updates (e.g. when git diff
				// reads tracked files). Ignore them — we only care about real
				// content changes.
				if ev.Op == fsnotify.Chmod {
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
	if colPath := b.singleDirtyColumn(dirty); colPath != "" {
		return b.reloadColumnCmd(seq, colPath)
	}
	return b.reloadCmd(seq)
}

// singleDirtyColumn returns the path of the column that contains every changed
// path, or "" when the change spans multiple columns, touches the board root
// (a column added/removed), or came from a watcher error. "" means full reload.
func (b *Board) singleDirtyColumn(dirty map[string]struct{}) string {
	if len(dirty) == 0 {
		return ""
	}
	match := ""
	for p := range dirty {
		if p == "" {
			return "" // watcher error → full reload
		}
		dir := filepath.Dir(p)
		found := ""
		for _, col := range b.columns {
			if samePath(col.Path, dir) {
				found = col.Path
				break
			}
		}
		if found == "" {
			return "" // not inside a known column (root-level change)
		}
		switch {
		case match == "":
			match = found
		case !samePath(match, found):
			return "" // spans multiple columns
		}
	}
	return match
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

// selectionByPath captures each column's currently selected item name keyed by
// column path, so a reload can restore the cursor after swapping in fresh
// columns (whose list index defaults to 0). Columns with no selection are
// omitted.
func (b *Board) selectionByPath() map[string]string {
	sel := make(map[string]string, len(b.columns))
	for _, col := range b.columns {
		if col.HasSelectedItem() {
			sel[col.Path] = col.SelectedItem().Name
		}
	}
	return sel
}

// applyReloadedColumns swaps in freshly built columns on the UI goroutine,
// re-applying height and palette, restoring each column's selected item, and
// clamping the column selection.
func (b *Board) applyReloadedColumns(columns []*Column) {
	prevSel := b.selectionByPath()
	for _, col := range columns {
		if b.visibleHeight > 0 {
			col.SetHeight(b.visibleHeight)
		}
		col.palette = b.palette
		if name, ok := prevSel[col.Path]; ok {
			col.SelectByName(name)
		}
	}
	b.columns = columns
	b.appendVirtualColumns()
	if len(b.columns) > 0 && b.selectedCol >= len(b.columns) {
		b.selectedCol = len(b.columns) - 1
	}
}

func (b *Board) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if b.quitting {
		return b, nil
	}
	prevCol, prevItem := b.snapshotSelection()
	model, cmd := b.updateInner(msg)
	// Selection events may fire hooks that schedule timers or async work;
	// drain both AFTER emitSelectionChanges so newly-scheduled tea.Cmds
	// don't get stranded in the Host's pending queues.
	b.emitSelectionChanges(prevCol, prevItem)
	if tcmd := b.collectTimerCmds(); tcmd != nil {
		cmd = batchCmd(cmd, tcmd)
	}
	if acmd := b.collectAsyncCmds(); acmd != nil {
		cmd = batchCmd(cmd, acmd)
	}
	if hcmd := b.collectHookCmd(); hcmd != nil {
		cmd = batchCmd(cmd, hcmd)
	}
	if scmd := b.collectStatusCmd(); scmd != nil {
		cmd = batchCmd(cmd, scmd)
	}
	// Re-apply any column_items transform that was skipped while a script was
	// running (the script has finished by the time the wrapper runs).
	b.drainColumnTransform()
	return model, cmd
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
		b.visibleHeight = msg.Height - 11
		if b.visibleHeight < 1 {
			b.visibleHeight = 1
		}
		for _, col := range b.columns {
			col.SetHeight(b.visibleHeight)
		}
		b.editor.SetTermSize(b.termWidth, b.termHeight)
		b.templateFlow.SetSize(b.termWidth, b.termHeight)
		return b, nil

	case tea.KeyMsg:
		return b.handleKey(msg)

	case tea.MouseMsg:
		return b.handleMouse(msg)

	case notifyMsg:
		return b, b.notifier.Send(msg.Message, msg.Type)

	case watchStartMsg:
		// board_load fires once, after the initial columns are populated and the
		// script host is subscribed. Publishing here (on the UI goroutine, not in
		// Init's off-thread startup) keeps Lua single-threaded.
		b.bus.Publish(events.BoardLoad{})
		b.bus.Publish(events.BoardRefresh{Reason: "startup"})
		// Cold-load transform: columns were built in Init's startup goroutine
		// (no VM access there); apply the script order now, on the UI goroutine,
		// after board_load has let init.lua finish its registrations.
		b.applyColumnTransforms()
		// Catch up from the remote on open (config-gated, default on). Detection
		// has run in the startup goroutine, so repoRoot is set; SyncOnce no-ops
		// when there is no remote or the tree is dirty.
		if b.cfg.GitSyncOnStartup {
			return b, tea.Batch(b.watchCmd(), b.git.SyncOnce())
		}
		return b, b.watchCmd()

	case watchEventMsg:
		// Coalesce a storm of events into one reload: bump the generation,
		// record the changed path, and schedule a debounce tick. Only the
		// final tick will survive the Seq guard. Re-arm the watcher so it
		// keeps listening.
		b.watchSeq++
		if b.watchDirty == nil {
			b.watchDirty = map[string]struct{}{}
		}
		b.watchDirty[msg.Path] = struct{}{}
		seq := b.watchSeq
		debounce := tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
			return watchDebounceMsg{Seq: seq}
		})
		return b, tea.Batch(debounce, b.watchCmd())

	case watchDebounceMsg:
		return b, b.debouncedReload(msg.Seq)

	case refreshedMsg:
		b.applyColumnTransforms()
		return b, b.notifier.Send("refreshed", notifySuccess)

	case boardReloadedMsg:
		if msg.Seq != b.watchSeq {
			return b, nil // stale — a newer change is already queued
		}
		b.applyReloadedColumns(msg.columns)
		b.applyColumnTransforms()
		b.publishItemChanges()
		b.bus.Publish(events.BoardRefresh{Reason: "watcher"})
		return b, b.git.RefreshStats()

	case columnReloadedMsg:
		if msg.Seq != b.watchSeq {
			return b, nil // stale
		}
		idx := -1
		for i, col := range b.columns {
			if samePath(col.Path, msg.path) {
				idx = i
				break
			}
		}
		if idx < 0 {
			// Column vanished since the event — fall back to a full reload.
			return b, b.reloadCmd(b.watchSeq)
		}
		prevName := ""
		if b.columns[idx].HasSelectedItem() {
			prevName = b.columns[idx].SelectedItem().Name
		}
		if b.visibleHeight > 0 {
			msg.col.SetHeight(b.visibleHeight)
		}
		msg.col.palette = b.palette
		if prevName != "" {
			msg.col.SelectByName(prevName)
		}
		b.columns[idx] = msg.col
		b.applyColumnTransform(msg.col)
		b.publishItemChanges()
		b.bus.Publish(events.BoardRefresh{Reason: "watcher"})
		return b, b.git.RefreshStats()

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

	case pasteRequestMsg:
		return b, b.pasteToItem(msg.ColIndex, msg.FileName, msg.Mode)

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
		b.editor.palette = b.palette
		return b, nil

	case quitConfirmedMsg:
		b.editor.Close()
		b.editor = NewEditor()
		b.editor.palette = b.palette
		return b.finishShutdown()

	case quickCommandMsg:
		return b.handleQuickCommand(msg)

	case switchBoardMsg:
		return b.handleSwitchBoard(msg)

	case pinBoardMsg:
		return b.handlePinBoard(msg)

	case removeBoardMsg:
		return b.handleRemoveBoard(msg)

	case searchMsg:
		// Search owns its async lifecycle (debounce + ripgrep); route opaquely,
		// the same way git.Msg is handled below.
		return b, b.search.Update(msg)

	case searchSelectMsg:
		return b.activateFile(msg.BoardPath, msg.FilePath)

	case runCustomCommandMsg:
		return b.handleRunCustomCommand(msg)

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
		return b.handleHookDone(msg)

	case git.Msg:
		// All git orchestration lives in the git package; route opaquely. Git
		// signals a deferred-quit completion itself via OnSyncSettled, so there
		// is no shutdown bookkeeping here.
		return b, b.git.Update(msg)

	case zellijDoneMsg:
		return b, b.zellij.Done(msg, b.notifier)

	case templateSubmitMsg:
		return b.handleTemplateSubmit(msg)

	case templateShellDoneMsg:
		return b, b.templateExec.done(msg)

	default:
		// An active huh form drives itself with internal messages (cursor
		// blink, group transitions); route them to it ahead of the column
		// list so they don't leak into the list filter.
		if b.templateFlow.Active() {
			return b, b.templateFlow.Update(msg)
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

func (b *Board) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While waiting for a sync to finish, a second Ctrl+C force-quits.
	if b.shuttingDown && key.Matches(msg, Keys.Quit) {
		b.quitting = true
		return b, tea.Quit
	}

	// Handle help overlay
	if b.helpOpen {
		switch {
		case key.Matches(msg, Keys.Quit):
			b.helpOpen = false
			return b.beginShutdown()
		case key.Matches(msg, Keys.HelpClose):
			b.helpOpen = false
		}
		return b, nil
	}

	// Handle config menu
	if b.configMenuOpen {
		switch {
		case key.Matches(msg, Keys.Quit):
			b.configMenuOpen = false
			return b.beginShutdown()
		case key.Matches(msg, Keys.ConfigMenuClose):
			b.configMenuOpen = false
		case key.Matches(msg, Keys.ConfigOpenLocal):
			b.configMenuOpen = false
			return b, b.openLocalConfig()
		case key.Matches(msg, Keys.ConfigOpenGlobal):
			b.configMenuOpen = false
			return b, b.openGlobalConfig()
		case key.Matches(msg, Keys.ConfigOpenLocalCommands):
			b.configMenuOpen = false
			return b, b.openLocalCommands()
		case key.Matches(msg, Keys.ConfigCreateLocalMCP):
			b.configMenuOpen = false
			return b, b.createLocalMCP()
		case key.Matches(msg, Keys.ConfigCreateLocalAgents):
			b.configMenuOpen = false
			return b, b.createLocalAgents()
		}
		return b, nil
	}

	// Handle dialog
	if b.dialog.active {
		return b, b.dialog.Update(msg)
	}

	// Handle editor
	if b.editor.state != editorNone {
		if key.Matches(msg, Keys.EditorCancel) && b.editor.IsDirty() {
			b.dialog.OpenConfirmDestructive("Discard unsaved changes?", "Your edits will be lost.", "Discard", editorDiscardMsg{})
			return b, nil
		}
		cmd, _ := b.editor.Update(msg)
		if b.editor.state == editorNone {
			b.editor = NewEditor()
			b.editor.palette = b.palette
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

	// Handle global search
	if b.search.Active() {
		return b, b.search.HandleKey(msg)
	}

	// Handle custom commands menu
	if b.customCmds.Active() {
		return b, b.customCmds.Update(msg)
	}

	// Handle script-driven UI (kbrd.ui.pick / prompt). Confirms go through Dialog.
	if b.scriptUI.Active() {
		return b, b.scriptUI.Update(msg)
	}

	// Handle template picker / form
	if b.templateFlow.Active() {
		return b, b.templateFlow.Update(msg)
	}

	// Handle git panel
	if b.git.Active() {
		return b, b.git.HandleKey(msg)
	}

	// Handle zellij actions menu
	if b.zellij.Active() {
		return b, b.zellij.Update(msg)
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

	// Virtual columns have no built-in item mutations: their command keys and
	// Enter run script-declared actions, and the file-mutation keys are swallowed.
	// Navigation/global keys fall through to the shared switch below.
	if col.Virtual {
		if cmd, handled := b.handleVirtualColumnKey(msg, col); handled {
			return b, cmd
		}
	}

	switch {
	case key.Matches(msg, Keys.Quit):
		return b.beginShutdown()
	case key.Matches(msg, Keys.ToggleHelp):
		b.helpOpen = true
		return b, nil
	case key.Matches(msg, Keys.ConfigMenu):
		b.configMenuOpen = true
		return b, nil
	case key.Matches(msg, Keys.QuickCmd):
		return b, b.openQuickCommand()
	case key.Matches(msg, Keys.SwitchBoard):
		return b, b.openSwitcher()
	case key.Matches(msg, Keys.Search):
		return b, b.openSearch()
	case key.Matches(msg, Keys.GitPanel):
		return b, b.git.Open()
	case b.zellij.Enabled && key.Matches(msg, Keys.ZellijMenu):
		b.zellij.OpenFor(b.cfg.Path, col)
		return b, nil
	case key.Matches(msg, Keys.Refresh):
		return b, b.refresh()
	case key.Matches(msg, Keys.RenameItem):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			return b, b.editor.OpenRenameItem(b.selectedCol, item.Name)
		}
	case key.Matches(msg, Keys.CustomCommands):
		if col.HasSelectedItem() && !col.SelectedItem().Separator {
			item := col.SelectedItem()
			b.loadCommands()
			var vctx map[string]interface{}
			switch {
			case col.Virtual:
				vctx = b.buildVirtualVars(col, item)
			case item.Data != nil:
				// Frontmatter-carrying item: Lua commands get the rich ctx
				// (nested data table); shell commands still use the flat vars.
				vctx = b.buildFilesystemCtx(b.selectedCol, item)
			}
			b.customCmds.Open(b.commandsForColumn(col), b.commandWarnings, b.buildCommandVars(b.selectedCol, item), vctx)
		}
		return b, nil
	case key.Matches(msg, Keys.RenameCol):
		return b, b.editor.OpenRenameColumn(b.selectedCol, col.Name)
	case key.Matches(msg, Keys.ZoomToggle):
		b.zoom.Toggle()
		return b, nil
	case key.Matches(msg, Keys.ZoomOff) && b.zoom.Active():
		// Only consume -/esc while zoomed; otherwise they fall through to the
		// list passthrough below, preserving their pre-zoom behavior.
		b.zoom.Off()
		return b, nil
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
	case key.Matches(msg, Keys.JumpCol):
		idx := int(msg.Runes[0] - '1') // '1' -> 0 ... '9' -> 8
		if idx >= 0 && idx < len(b.columns) {
			b.selectedCol = idx
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
			b.bus.Publish(events.ItemOpen{
				Item: events.ItemRef{Column: col.Name, Name: item.Name},
				Kind: "edit",
			})
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
			return b, b.openPasteMenu(b.selectedCol, item.Name)
		}
	case key.Matches(msg, Keys.OpenExternal):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			err := col.OpenFile(item.Name)
			if err != nil {
				return b, b.notifier.Send("failed to open: "+err.Error(), notifyError)
			}
			b.bus.Publish(events.ItemOpen{
				Item: events.ItemRef{Column: col.Name, Name: item.Name},
				Kind: "external",
			})
			return b, b.notifier.Send("opened "+item.Name, notifySuccess)
		}
	case key.Matches(msg, Keys.Pin):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			err := col.PinItem(item.Name)
			if err != nil {
				return b, b.notifier.Send("failed to pin: "+err.Error(), notifyError)
			}
			b.applyColumnTransform(col)
			pinState := "unpinned"
			if item.Pinned {
				pinState = "pinned"
			}
			return b, b.notifier.Send(item.Name+" "+pinState, notifySuccess)
		}
	case key.Matches(msg, Keys.Delete):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			b.dialog.OpenConfirmDestructive("Delete item?", item.Name+".md", "Yes", deleteConfirmMsg{ColIndex: b.selectedCol, FileName: item.Name})
		}
	case key.Matches(msg, Keys.MoveNext):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			nextCol := (b.selectedCol + 1) % len(b.columns)
			if err := b.moveItem(col, b.columns[nextCol], item.Name); err != nil {
				if errors.Is(err, os.ErrExist) {
					return b, b.notifier.Send("file already exists in target: "+item.Name+".md", notifyError)
				}
				return b, b.notifier.Send("failed to move: "+err.Error(), notifyError)
			}
			b.selectedCol = nextCol
			b.columns[nextCol].SelectByName(item.Name)
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
			if err := b.moveItem(col, b.columns[0], item.Name); err != nil {
				if errors.Is(err, os.ErrExist) {
					return b, b.notifier.Send("file already exists in target: "+item.Name+".md", notifyError)
				}
				return b, b.notifier.Send("failed to move: "+err.Error(), notifyError)
			}
			b.selectedCol = 0
			b.columns[0].SelectByName(item.Name)
		}
	case key.Matches(msg, Keys.Peek):
		if col.HasSelectedItem() {
			item := col.SelectedItem()
			content, err := col.CopyContent(item.Name)
			if err != nil {
				return b, b.notifier.Send("failed to peek: "+err.Error(), notifyError)
			}
			return b, b.peek.Open(item.Title, string(content), b.termWidth)
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
	case key.Matches(msg, Keys.NewFromTemplate):
		return b.openTemplateFlow(col)
	default:
		return b, col.UpdateList(msg)
	}

	return b, nil
}

func (b *Board) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Zoom is excluded because click hit-testing assumes the normal multi-column
	// slot geometry and card height.
	if b.helpOpen || b.configMenuOpen || b.dialog.active || b.editor.state != editorNone ||
		b.peek.Active() || b.switcher.Active() || b.search.Active() || b.customCmds.Active() || b.scriptUI.Active() || b.templateFlow.Active() || b.git.Active() || b.zellij.Active() || b.quickCmdMode || b.zoom.Active() || len(b.columns) == 0 {
		return b, nil
	}
	if b.columns[b.selectedCol].IsFiltering() {
		return b, nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return b, nil
	}

	xc := msg.X - b.leftIndicatorWidth
	if xc < 0 {
		return b, nil
	}
	first, count := b.visibleColRange()
	slotIdx := xc / b.slotWidth()
	if slotIdx < 0 || slotIdx >= count {
		return b, nil
	}
	colIdx := first + slotIdx
	col := b.columns[colIdx]

	itemIdx, ok := col.HitTest(msg.Y - b.logoHeight)
	if !ok {
		return b, nil
	}
	b.selectedCol = colIdx
	col.SelectIndex(itemIdx)
	return b, nil
}

func (b *Board) openLocalConfig() tea.Cmd {
	return b.openManagedFile(localConfigPath, ensureConfigFile)
}
func (b *Board) openGlobalConfig() tea.Cmd {
	return b.openManagedFile(globalConfigPath, ensureConfigFile)
}
func (b *Board) openLocalCommands() tea.Cmd {
	return b.openManagedFile(localCommandsPath, ensureCommandsFile)
}

// createLocalMCP writes a .mcp.json into the current board directory pointing
// at kbrd's built-in MCP server, then opens it. The address comes from the
// active board's config so the file matches the running server.
func (b *Board) createLocalMCP() tea.Cmd {
	addr := b.cfg.MCP.Addr
	resolve := func() (string, error) { return filepath.Join(b.cfg.Path, config.FolderMCPFile), nil }
	ensure := func(path string) error { return ensureMCPFile(path, addr) }
	return b.openManagedFile(resolve, ensure)
}

// createLocalAgents writes an AGENTS.md describing kbrd into the current board
// directory, then opens it.
func (b *Board) createLocalAgents() tea.Cmd {
	resolve := func() (string, error) { return filepath.Join(b.cfg.Path, config.FolderAgentsFile), nil }
	return b.openManagedFile(resolve, ensureAgentsFile)
}

func (b *Board) openManagedFile(resolve func() (string, error), ensure func(string) error) tea.Cmd {
	path, err := resolve()
	if err != nil {
		return b.notifier.Send(err.Error(), notifyError)
	}
	if err := ensure(path); err != nil {
		return b.notifier.Send("write "+path+": "+err.Error(), notifyError)
	}
	if err := openFile(path); err != nil {
		return b.notifier.Send("open: "+err.Error(), notifyError)
	}
	return b.notifier.Send("opened "+path, notifySuccess)
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
	// board.ReplaceFileContent is existing-only: a card deleted while the
	// editor was open errors instead of being silently resurrected.
	if err := board.ReplaceFileContent(fullPath, msg.Content); err != nil {
		return b, b.notifier.Send("failed to save: "+err.Error(), notifyError)
	}
	b.reloadColumnAfterMutation(col)
	b.bus.Publish(events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: msg.FileName}, Kind: "save"})
	return b, b.notifier.Send("saved "+msg.FileName, notifySuccess)
}

func (b *Board) handleAppend(msg editorAppendMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	err := col.AppendText(msg.FileName, msg.Text)
	if err != nil {
		return b, b.notifier.Send("failed to append: "+err.Error(), notifyError)
	}
	b.reloadColumnAfterMutation(col)
	b.bus.Publish(events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: msg.FileName}, Kind: "append"})
	return b, b.notifier.Send("appended to "+msg.FileName, notifySuccess)
}

func (b *Board) handlePrepend(msg editorPrependMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	err := col.PrependText(msg.FileName, msg.Text)
	if err != nil {
		return b, b.notifier.Send("failed to prepend: "+err.Error(), notifyError)
	}
	b.reloadColumnAfterMutation(col)
	b.bus.Publish(events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: msg.FileName}, Kind: "prepend"})
	return b, b.notifier.Send("prepended to "+msg.FileName, notifySuccess)
}

func (b *Board) handleJournal(msg editorJournalMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	err := col.JournalText(msg.FileName, msg.Text)
	if err != nil {
		return b, b.notifier.Send("failed to journal: "+err.Error(), notifyError)
	}
	b.reloadColumnAfterMutation(col)
	return b, b.notifier.Send("journal entry added to "+msg.FileName, notifySuccess)
}

// openTemplateFlow starts the new-item-from-template overlay for col: lists
// the column's .kbrd_templates merged with the board-level ones and opens the
// picker (or, with a single template, its form directly).
func (b *Board) openTemplateFlow(col *Column) (tea.Model, tea.Cmd) {
	if col.Virtual {
		return b, b.notifier.Send(errVirtualColumn.Error(), notifyError)
	}
	tmpls, warns, err := template.List(b.cfg.Path, col.Path)
	if err != nil {
		return b, b.notifier.Send("failed to list templates: "+err.Error(), notifyError)
	}
	var warnCmd tea.Cmd
	if len(warns) > 0 {
		w := warns[0]
		warnCmd = b.notifier.Send("skipped "+filepath.Base(w.Path)+": "+w.Err.Error(), notifyError)
	}
	if len(tmpls) == 0 {
		if warnCmd != nil {
			return b, warnCmd
		}
		return b, b.notifier.Send("no templates — add .md files to "+col.Name+"/"+template.Dir+" or "+template.Dir, notifyError)
	}
	return b, tea.Batch(warnCmd, b.templateFlow.Open(b.selectedCol, tmpls))
}

// handleTemplateSubmit renders the completed template form and creates the
// new card, mirroring handleNew's error reporting.
func (b *Board) handleTemplateSubmit(msg templateSubmitMsg) (tea.Model, tea.Cmd) {
	if msg.ColIndex < 0 || msg.ColIndex >= len(b.columns) {
		return b, b.notifier.Send("invalid column", notifyError)
	}
	col := b.columns[msg.ColIndex]
	vctx := board.VarContext{
		BoardPath:  b.cfg.Path,
		BoardName:  b.cfg.BoardName,
		ColumnPath: col.Path,
		ColumnName: col.Name,
	}
	name, body, err := template.Instantiate(msg.Template, vctx, msg.Values)
	if err != nil {
		return b, b.notifier.Send("template: "+err.Error(), notifyError)
	}
	// Resolve {{shell}} markers: rewrite to inert notes when exec is disabled,
	// or spawn a background worker per marker that fills it in. cardPath is the
	// path the card is about to be written to.
	cardPath := filepath.Join(col.Path, name+".md")
	body, shellCmd := b.templateExec.dispatch(cardPath, body, b.cfg.Path, b.cfg.Template)
	if _, err := b.createItemContent(col, name, body); err != nil {
		if errors.Is(err, os.ErrExist) {
			return b, b.notifier.Send("file already exists: "+name+".md", notifyError)
		}
		return b, b.notifier.Send("failed to create: "+err.Error(), notifyError)
	}
	col.SelectByName(name)
	return b, tea.Batch(shellCmd, b.notifier.Send("created "+name+".md", notifySuccess))
}

func (b *Board) handleNew(msg editorNewMsg) (tea.Model, tea.Cmd) {
	col := b.columns[msg.ColIndex]
	if msg.FileName == "" {
		return b, b.notifier.Send("filename cannot be empty", notifyError)
	}
	if _, err := b.createItem(col, msg.FileName); err != nil {
		return b, b.notifier.Send("failed to create: "+err.Error(), notifyError)
	}
	return b, b.notifier.Send("created "+msg.FileName+".md", notifySuccess)
}

func validateRenameName(name string) error {
	_, err := board.SanitizeFolder(name)
	return err
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
	b.dialog.OpenConfirm("Rename item?", msg.OldName+".md → "+newName+".md", renameItemConfirmMsg{ColIndex: msg.ColIndex, OldName: msg.OldName, NewName: newName})
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
	b.dialog.OpenConfirm("Rename column?", msg.OldName+" → "+newName, renameColumnConfirmMsg{ColIndex: msg.ColIndex, OldName: msg.OldName, NewName: newName})
	return b, nil
}

func (b *Board) handleRenameItemConfirm(msg renameItemConfirmMsg) (tea.Model, tea.Cmd) {
	if msg.ColIndex < 0 || msg.ColIndex >= len(b.columns) {
		return b, b.notifier.Send("invalid column", notifyError)
	}
	col := b.columns[msg.ColIndex]
	if err := b.renameItem(col, msg.OldName, msg.NewName); err != nil {
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
	if err := b.deleteItem(col, msg.FileName); err != nil {
		return b, b.notifier.Send("failed to delete: "+err.Error(), notifyError)
	}
	b.reloadColumnAfterMutation(col)
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
		if len(cmd) >= 2 && isItemCommandAction(cmd[0]) {
			action := cmd[0]
			suffix := cmd[1:]
			if ref, ok := b.refByMnemonic[suffix]; ok {
				return b, b.dispatchItemCommand(action, ref)
			}
			return b, b.notifier.Send("no item: "+suffix, notifyError)
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
		suffix := buf[1:]
		if suffix == "" {
			return b, cmd
		}
		if _, ok := b.refByMnemonic[suffix]; ok {
			return b, cmd
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

func (b *Board) handlePinBoard(msg pinBoardMsg) (tea.Model, tea.Cmd) {
	store, err := recents.Load()
	if err != nil {
		return b, b.notifier.Send("failed to load recents: "+err.Error(), notifyError)
	}
	store.SetPinned(msg.Path, msg.Name, msg.Pinned)
	if err := store.Save(); err != nil {
		return b, b.notifier.Send("failed to save recents: "+err.Error(), notifyError)
	}
	activeAbs, _ := filepath.Abs(b.cfg.Path)
	b.switcher.Open(store.Entries, activeAbs)
	return b, nil
}

func (b *Board) handleRemoveBoard(msg removeBoardMsg) (tea.Model, tea.Cmd) {
	store, err := recents.Load()
	if err != nil {
		return b, b.notifier.Send("failed to load recents: "+err.Error(), notifyError)
	}
	store.Remove(msg.Path)
	if err := store.Save(); err != nil {
		return b, b.notifier.Send("failed to save recents: "+err.Error(), notifyError)
	}
	activeAbs, _ := filepath.Abs(b.cfg.Path)
	b.switcher.Open(store.Entries, activeAbs)
	return b, nil
}

func (b *Board) handleSwitchBoard(msg switchBoardMsg) (tea.Model, tea.Cmd) {
	cmd, err := b.loadBoard(msg.Path)
	if err != nil {
		return b, b.notifier.Send(err.Error(), notifyError)
	}
	return b, cmd
}

// loadBoard switches the board to path: closes the old watcher, reloads config,
// columns, scripting, git state and a fresh watcher, and records the board in
// recents. selectedCol is reset to 0. Returns the watch+notify command. Errors
// are returned without sending a notification so callers can phrase them.
func (b *Board) loadBoard(path string) (tea.Cmd, error) {
	newCfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load board: %w", err)
	}

	if b.watcher != nil {
		_ = b.watcher.Close()
		b.watcher = nil
	}

	b.cfg = newCfg
	b.theme = newCfg.Theme
	b.initGit()
	b.applyPalette()
	b.selectedCol = 0
	// Virtual columns belong to the previous board's (now-closed) script host;
	// drop them so they don't leak onto the new board before its board_load runs.
	b.virtualCols = nil
	b.initScripting()
	b.loadCommands()
	b.initHooks()

	if err := b.loadColumns(); err != nil {
		return nil, fmt.Errorf("failed to load columns: %w", err)
	}
	b.applyPalette()
	b.git.Detect()
	// Re-fire board_load on the new board so its init script can repopulate any
	// virtual columns (runs on the UI goroutine, host already subscribed).
	b.bus.Publish(events.BoardLoad{})
	b.applyColumnTransforms()

	if paths, err := board.DiscoverPaths(b.cfg.Path); err == nil {
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
	return tea.Batch(b.watchCmd(), b.notifier.Send("switched to "+label, notifySuccess)), nil
}

// openSearch loads the recents store and opens the global search dialog with
// every recent board plus the currently open board as search roots.
func (b *Board) openSearch() tea.Cmd {
	store, err := recents.Load()
	if err != nil {
		return b.notifier.Send("failed to load recents: "+err.Error(), notifyError)
	}
	if store.Prune() > 0 {
		_ = store.Save()
	}

	activeAbs, _ := filepath.Abs(b.cfg.Path)
	roots := buildSearchRoots(activeAbs, b.cfg.BoardName, store.Entries)
	b.search.Open(roots, b.palette)
	return nil
}

// activateFile switches to boardPath (if not already active) and selects the
// column/item containing filePath. Used by the global search dialog.
func (b *Board) activateFile(boardPath, filePath string) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if !samePath(boardPath, b.cfg.Path) {
		c, err := b.loadBoard(boardPath)
		if err != nil {
			return b, b.notifier.Send(err.Error(), notifyError)
		}
		cmd = c
	}

	if colIdx, itemIdx, ok := locateFile(b.columns, filePath); ok {
		b.selectedCol = colIdx
		b.columns[colIdx].SelectIndex(itemIdx)
		return b, cmd
	}
	if cmd != nil {
		return b, tea.Batch(cmd, b.notifier.Send("opened board; file not in a column", notifySuccess))
	}
	return b, b.notifier.Send("file not in a column", notifyError)
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
	if col.Virtual {
		return b.notifier.Send("virtual columns have no built-in item actions — use x", notifyError)
	}
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
	case 'v':
		return b.openPasteMenu(ref.ColIndex, item.Name)
	case 'o':
		if err := col.OpenFile(item.Name); err != nil {
			return b.notifier.Send("failed to open: "+err.Error(), notifyError)
		}
		return b.notifier.Send("opened "+item.Name, notifySuccess)
	case '!':
		if err := col.PinItem(item.Name); err != nil {
			return b.notifier.Send("failed to pin: "+err.Error(), notifyError)
		}
		b.applyColumnTransform(col)
		state := "unpinned"
		if !item.Pinned {
			state = "pinned"
		}
		return b.notifier.Send(item.Name+" "+state, notifySuccess)
	case 'd':
		b.dialog.OpenConfirmDestructive("Delete item?", item.Name+".md", "Yes", deleteConfirmMsg{ColIndex: ref.ColIndex, FileName: item.Name})
		return nil
	case 'm':
		nextCol := (ref.ColIndex + 1) % len(b.columns)
		toName := b.columns[nextCol].Name
		if err := b.moveItem(col, b.columns[nextCol], item.Name); err != nil {
			return b.notifier.Send("failed to move: "+err.Error(), notifyError)
		}
		return b.notifier.Send("moved "+item.Name+" → "+toName, notifySuccess)
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
			if item.Separator {
				continue // inert grouping rows get no quick-jump tag
			}
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
		b.bus.Publish(events.BoardRefresh{Reason: "refresh"})
		// The column_items transform needs the UI goroutine (Lua VM); this
		// closure runs on a worker, so hand off via the message handler.
		return refreshedMsg{}
	}
}

func (b *Board) toggleTheme() {
	if b.theme == "dark" {
		b.theme = "light"
	} else {
		b.theme = "dark"
	}
	b.applyPalette()
}

func (b *Board) copyToClipboard(content []byte) tea.Cmd {
	return func() tea.Msg {
		if err := clipboard.WriteAll(string(content)); err != nil {
			return notifyMsg{Message: "clipboard not available", Type: notifyError}
		}
		return notifyMsg{Message: "copied to clipboard", Type: notifySuccess}
	}
}

func (b *Board) openPasteMenu(colIdx int, fileName string) tea.Cmd {
	text, err := clipboard.ReadAll()
	if err != nil || text == "" {
		return b.notifier.Send("clipboard empty or unavailable", notifyError)
	}
	b.dialog.Open(DialogOptions{
		Title: "Paste from clipboard",
		Body:  "Into " + fileName + ".md",
		Buttons: []DialogButton{
			{Label: "At beginning", Hotkey: 'a',
				Msg: pasteRequestMsg{ColIndex: colIdx, FileName: fileName, Mode: pasteAtStart}},
			{Label: "Append at end", Hotkey: 'p',
				Msg: pasteRequestMsg{ColIndex: colIdx, FileName: fileName, Mode: pasteAtEnd}},
			{Label: "Journal entry", Hotkey: 'j',
				Msg: pasteRequestMsg{ColIndex: colIdx, FileName: fileName, Mode: pasteJournal}},
			{Label: "Replace whole file", Kind: ButtonDanger, Hotkey: 'R',
				Msg: pasteRequestMsg{ColIndex: colIdx, FileName: fileName, Mode: pasteReplace}},
		},
		DefaultIndex: 1,
	})
	return nil
}

func (b *Board) pasteToItem(colIdx int, fileName string, mode pasteMode) tea.Cmd {
	return func() tea.Msg {
		text, err := clipboard.ReadAll()
		if err != nil || text == "" {
			return notifyMsg{Message: "clipboard empty or unavailable", Type: notifyError}
		}
		col := b.columns[colIdx]
		var verb string
		switch mode {
		case pasteAtStart:
			err = col.PrependText(fileName, text)
			verb = "prepended to "
		case pasteJournal:
			err = col.JournalText(fileName, text)
			verb = "journaled to "
		case pasteReplace:
			err = col.ReplaceFile(fileName, text)
			verb = "replaced "
		default:
			err = col.AppendText(fileName, text)
			verb = "appended to "
		}
		if err != nil {
			return notifyMsg{Message: "failed to paste: " + err.Error(), Type: notifyError}
		}
		col.LoadItems()
		return notifyMsg{Message: verb + fileName, Type: notifySuccess}
	}
}

func (b *Board) renderLogo() string {
	name := lipgloss.NewStyle().
		Foreground(b.palette.Primary).
		Bold(true).
		Render("kbrd")
	version := lipgloss.NewStyle().
		Foreground(b.palette.FgSubtle).
		Italic(true).
		Render(Version)
	board := lipgloss.NewStyle().
		Foreground(b.palette.FgMuted).
		Render(b.boardLabel())
	// ⌨️ is a wide (2-cell) emoji; keep it as a literal prefix and let lipgloss
	// measure widths downstream rather than counting runes by hand.
	return "⌨️  " + name + "  " + version + "  " + board
}

// updateBuiltinCells recomputes the internal (negative-id) cells from current
// board state on every render. They are cheap to derive and event-free, so
// deriving them here keeps the strip always-accurate without any host ticker.
// Script-set cells (positive ids) are untouched. Ids are ordered so the
// persistent metrics (count, git) sit to the right and the transient activity
// indicators (sync, jobs, kbrd.status) flow in to their left as they appear.
func (b *Board) updateBuiltinCells() {
	// Sync indicator (id -5): transient spinner while reconciling, else the
	// persistent remote-sync status. The mapping lives in syncCell.
	if cell, ok := syncCell(b.git.SyncState(), b.git.DirtyCount(), b.shuttingDown, b.palette); ok {
		b.cells.SetInternal(cell)
	} else {
		b.cells.Clear(syncCellID)
	}

	if b.asyncInflight > 0 {
		label := "⟳ 1 running"
		if b.asyncInflight > 1 {
			label = "⟳ " + strconv.Itoa(b.asyncInflight) + " running"
		}
		b.setActivityCell(-4, label)
	} else {
		b.cells.Clear(-4)
	}

	if n := b.templateExec.Inflight(); n > 0 {
		label := "✦ generating"
		if n > 1 {
			label = "✦ " + strconv.Itoa(n) + " generating"
		}
		b.setActivityCell(-8, label)
	} else {
		b.cells.Clear(-8)
	}

	if b.hooks.busy() {
		label := "⚙ hooks"
		if n := b.hooks.pending(); n > 1 {
			label = "⚙ hooks " + strconv.Itoa(n)
		}
		b.setActivityCell(-6, label)
	} else {
		b.cells.Clear(-6)
	}

	if b.scriptStatus != "" {
		b.cells.SetInternal(Cell{ID: -3, Text: b.scriptStatus, FG: string(b.palette.FgMuted)})
	} else {
		b.cells.Clear(-3)
	}

	// Persistent MCP indicator: filled+green when bound, danger when requested
	// but the bind failed (e.g. the port is already in use), hollow+muted when
	// off. Leftmost (most negative) id so it survives header truncation alongside
	// the other built-ins.
	switch b.mcpStatus {
	case MCPRunning:
		b.cells.SetInternal(Cell{ID: -7, Text: "◆ mcp", FG: string(b.palette.Success)})
	case MCPFailed:
		b.cells.SetInternal(Cell{ID: -7, Text: "✕ mcp", FG: string(b.palette.Danger)})
	default:
		b.cells.SetInternal(Cell{ID: -7, Text: "◇ mcp", FG: string(b.palette.FgMuted)})
	}

	total := 0
	for _, c := range b.columns {
		total += c.TotalCount()
	}
	b.cells.SetInternal(Cell{
		ID:   -2,
		Text: strconv.Itoa(total) + " items",
		FG:   string(b.palette.FgMuted),
	})

	if b.git.RepoRoot() != "" {
		if dirty := b.git.DirtyCount(); dirty > 0 {
			b.cells.SetInternal(Cell{
				ID:   -1,
				Text: "● " + strconv.Itoa(dirty),
				FG:   string(b.palette.Warning),
			})
		} else {
			b.cells.SetInternal(Cell{
				ID:   -1,
				Text: "✓ clean",
				FG:   string(b.palette.Success),
			})
		}
	} else {
		b.cells.Clear(-1)
	}
}

// setActivityCell sets a transient activity indicator cell in the accent color.
func (b *Board) setActivityCell(id int, text string) {
	b.cells.SetInternal(Cell{ID: id, Text: text, FG: string(b.palette.AccentSoft)})
}

// slotWidth is the rendered width of one column cell on the row (see
// layout.go for the geometry).
func (b *Board) slotWidth() int { return slotWidth(b.cfg.ColumnWidth) }

// visibleColRange returns the index of the first column to render and the
// number of columns that fit horizontally. It also adjusts firstVisibleCol so
// the active column is always within the visible window. The math lives in
// layout.go; this wrapper applies it to board state.
func (b *Board) visibleColRange() (first, count int) {
	if len(b.columns) == 0 {
		return 0, 0
	}
	count = visibleCount(b.termWidth, b.slotWidth(), len(b.columns))
	b.firstVisibleCol = clampFirstVisible(b.firstVisibleCol, b.selectedCol, count, len(b.columns))
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
	if b.termWidth > 0 && b.termWidth < minBoardWidth(b.cfg.ColumnWidth) {
		w, h := b.termWidth, b.termHeight
		if h == 0 {
			h = 24
		}
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(b.palette.FgMuted).Render("terminal too small"))
	}
	if b.termHeight > 0 && b.termHeight < 10 {
		w := b.termWidth
		if w == 0 {
			w = 80
		}
		return lipgloss.Place(w, b.termHeight, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(b.palette.FgMuted).Render("terminal too small"))
	}

	b.rebuildMnemonics()

	gap := lipgloss.NewStyle().MarginRight(1)
	gutterW := gutterWidth(b.mnemonicMaxLen)

	slots, first := computeSlots(b.zoom.Active(), b.termWidth, b.selectedCol, b.firstVisibleCol, len(b.columns), b.cfg.ColumnWidth)
	b.firstVisibleCol = first
	end := first + len(slots)
	rendered := make([]string, 0, len(slots)+2)

	indicatorStyle := lipgloss.NewStyle().
		Foreground(b.palette.FgSubtle).
		Bold(true).
		PaddingTop(1).
		MarginRight(1)
	b.leftIndicatorWidth = 0
	if !b.zoom.Active() && first > 0 {
		chip := indicatorStyle.Render(fmt.Sprintf("◀ %d", first))
		rendered = append(rendered, chip)
		b.leftIndicatorWidth = lipgloss.Width(chip) + 1
	}
	for _, s := range slots {
		col := b.columns[s.Col]
		rendered = append(rendered, gap.Render(col.View(RenderCtx{
			Active:       s.Col == b.selectedCol,
			Width:        s.Width,
			PreviewLines: s.PreviewLines,
			GutterW:      gutterW,
			MnemonicOf:   b.mnemonicLookup(s.Col),
			StatFor:      b.git.StatFor,
		})))
	}
	if !b.zoom.Active() && end < len(b.columns) {
		rendered = append(rendered, indicatorStyle.Render(fmt.Sprintf("%d ▶", len(b.columns)-end)))
	}
	columnsView := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	if b.zoom.Active() {
		// Zoom renders a single column; center it on the row.
		columnsView = lipgloss.PlaceHorizontal(b.termWidth, lipgloss.Center, columnsView)
	}

	quickCmdView := b.renderQuickCommand()

	w, h := b.termWidth, b.termHeight
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}

	// Header row: logo on the left, cells strip right-aligned on the same line.
	b.updateBuiltinCells()
	logo := b.renderLogo()
	header := logo
	if !b.cells.Empty() {
		avail := w - lipgloss.Width(logo) - 2
		if strip := b.cells.render(avail); lipgloss.Width(strip) > 0 {
			pad := w - lipgloss.Width(logo) - lipgloss.Width(strip)
			if pad < 1 {
				pad = 1
			}
			header = logo + strings.Repeat(" ", pad) + strip
		}
	}
	// Tint the whole header line with a subtle surface background, padded to the
	// full terminal width, and underline it with a muted rule to separate the
	// header from the columns. Chips with their own bg keep it; bare text and
	// the gap inherit the tint. The rule adds a row, which lipgloss.Height picks
	// up so logoHeight (and thus mouse hit-testing) stays correct.
	header = lipgloss.NewStyle().
		Background(b.palette.BgCodeInline).
		Width(w).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(b.palette.BorderMuted).
		Render(header)
	b.logoHeight = lipgloss.Height(header)
	result := header + "\n" + columnsView
	if quickCmdView != "" {
		result += "\n" + quickCmdView
	}
	if b.helpOpen {
		overlay := RenderHelpOverlay(w, h, GlobalShortcuts(ShortcutContext{
			HasSelectedItem: b.selectedCol < len(b.columns) && b.columns[b.selectedCol].HasSelectedItem(),
		}))
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, overlay)
	}
	if b.configMenuOpen {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, RenderConfigCommandsOverlay(configCommandEntries()))
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
	if b.search.Active() {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, b.search.View(w, h))
	}
	if b.customCmds.Active() {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, b.customCmds.View(b.termWidth, b.termHeight))
	}
	if b.scriptUI.Active() {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, b.scriptUI.View())
	}
	if b.templateFlow.Active() {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, b.templateFlow.View())
	}
	if b.git.Active() {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, b.git.View())
	}
	if b.zellij.Active() {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, b.zellij.View())
	}
	result += "\n" + b.renderStatusBar()

	return result
}

func (b *Board) renderStatusBar() string {
	width := b.termWidth
	if width == 0 {
		width = 80
	}

	ctx := ShortcutContext{QuickCmdMode: b.quickCmdMode, Zoomed: b.zoom.Active()}
	ctx.HasSelectedItem = b.selectedCol < len(b.columns) && b.columns[b.selectedCol].HasSelectedItem()
	if b.selectedCol < len(b.columns) && b.columns[b.selectedCol].Virtual {
		col := b.columns[b.selectedCol]
		ctx.Virtual = true
		for _, vc := range col.colCmds {
			if vc.Key != "" {
				ctx.VirtualCmds = append(ctx.VirtualCmds, Shortcut{Keys: vc.Key, Label: vc.Name})
			}
		}
	}

	// The board/column info and the transient activity indicators (git sync,
	// background jobs, kbrd.status) now live in the header cells, so the bottom
	// bar is just the keyboard hints.
	secondary := RenderInlineHints(ContextShortcuts(ctx))
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(secondary)
}

func (b *Board) boardLabel() string {
	if b.cfg.BoardName != "" {
		return "[" + b.cfg.BoardName + "] " + filepath.Base(b.cfg.Path)
	}
	return filepath.Base(b.cfg.Path)
}

func (b *Board) renderQuickCommand() string {
	if !b.quickCmdMode {
		return ""
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(b.palette.BorderActive).
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
