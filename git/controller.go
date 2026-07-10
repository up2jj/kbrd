// Package git owns all git orchestration for a board: the git panel UI, diffing,
// commit/sync, the per-item diff-stat data, and the background auto-sync loop.
// It is deliberately a sibling of the model package (model imports git, never the
// reverse) so the boundary is enforced by the compiler. Everything it needs from
// the host is injected via Deps; per-item stats are read back via StatFor.
package git

import (
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"kbrd/config"
	"kbrd/events"
	kbrdfs "kbrd/fs"
	"kbrd/theme"
)

// Msg marks every git-internal message so the host can route them opaquely
// (`case git.Msg: return b, b.git.Update(m)`) without knowing the concrete types.
type Msg interface{ isGitMsg() }

// gitMsg is embedded in every git message to satisfy Msg.
type gitMsg struct{}

func (gitMsg) isGitMsg() {}

// Notifier is the narrow toast surface git needs; the host adapts its own
// notifier to this. Keeping it here (rather than importing the host's) is what
// lets git stay decoupled from model.
type Notifier interface {
	Success(msg string) tea.Cmd
	Error(msg string) tea.Cmd
}

// Deps are the host-provided collaborators. BeforeCommit is an opaque pre-commit
// hook (the host uses it to regenerate a README from board content — git never
// learns what it does). EditorActive lets the host pause automatic sync starts
// while an in-app editor owns a buffer. OnSyncSettled is invoked once an
// in-flight auto-sync finishes after a shutdown was requested, so the host can
// finally quit.
type Deps struct {
	Cfg           config.Config
	Notifier      Notifier
	Bus           *events.Bus
	BeforeCommit  func() error
	EditorActive  func() bool
	OnSyncSettled func() tea.Cmd
}

// Controller holds all git state and orchestrates the panel + background work.
type Controller struct {
	cfg           config.Config
	notifier      Notifier
	bus           *events.Bus
	beforeCommit  func() error
	editorActive  func() bool
	onSyncSettled func() tea.Cmd

	panel    GitPanel
	repoRoot string
	stats    map[string]kbrdfs.DiffStat

	syncing         bool // manual or automatic sync in progress
	shutdownPending bool // host asked to quit; signal once the in-flight sync settles
	syncDeadline    time.Time
	autoSyncSeq     int64
	activeSyncSeq   int64
	expiredSyncSeq  int64

	hasRemote         bool      // cached: repo has an "origin" (drives the sync indicator)
	lastSyncFailed    bool      // the most recent reconcile errored
	lastSyncConflicts int       // conflict copies from the last reconcile, sticky until a clean sync
	lastSyncAt        time.Time // when the last reconcile succeeded; zero before the first

	termW, termH int
}

// SyncStatus is the header sync-indicator state. The cell is hidden unless
// HasRemote; Syncing shows while a background reconcile runs; Failed is sticky
// until the next success; Conflicts is sticky until the next clean sync.
type SyncStatus struct {
	HasRemote bool
	Syncing   bool
	Failed    bool
	Conflicts int
	LastSync  time.Time // zero before the first successful sync this session
}

func New(d Deps) Controller {
	return Controller{
		cfg:           d.Cfg,
		notifier:      d.Notifier,
		bus:           d.Bus,
		beforeCommit:  d.BeforeCommit,
		editorActive:  d.EditorActive,
		onSyncSettled: d.OnSyncSettled,
	}
}

// SetConfig updates the controller's live policy after the board reloads TOML.
// The reload decision stays with the host/model; git only consumes the latest
// config snapshot for future actions and ticks.
func (c *Controller) SetConfig(cfg config.Config) { c.cfg = cfg }

func (c *Controller) SetNotifier(n Notifier) { c.notifier = n }

// Detect locates the repo root for the configured path and computes the initial
// diff stats. Called once at startup (off the main goroutine).
func (c *Controller) Detect() {
	c.repoRoot = kbrdfs.GitRepoRoot(c.cfg.Path)
	c.refreshRemote()
	c.refreshStats()
}

// refreshRemote caches whether the repo has a remote, so the per-render sync
// indicator never shells out to git. Called whenever the repo or its remotes
// may have changed.
func (c *Controller) refreshRemote() {
	c.hasRemote = c.repoRoot != "" && kbrdfs.GitHasRemote(c.repoRoot)
}

// SyncState reports the current header sync-indicator state.
func (c *Controller) SyncState() SyncStatus {
	c.expireStaleAutoSync(time.Now())
	return SyncStatus{
		HasRemote: c.hasRemote,
		Syncing:   c.syncing,
		Failed:    c.lastSyncFailed,
		Conflicts: c.lastSyncConflicts,
		LastSync:  c.lastSyncAt,
	}
}

// recordSyncOutcome updates the indicator after a reconcile settles: a failure
// is sticky until the next success; conflict copies are sticky until a clean
// sync clears them.
func (c *Controller) recordSyncOutcome(err error, conflicts int) {
	if err != nil {
		c.lastSyncFailed = true
		return
	}
	c.lastSyncFailed = false
	c.lastSyncConflicts = conflicts
	c.lastSyncAt = time.Now()
}

// StartAutoSync returns the initial auto-sync tick (nil if disabled).
func (c *Controller) StartAutoSync() tea.Cmd { return c.scheduleAutoSync() }

func (c *Controller) SetPalette(p theme.Palette) {
	c.panel.SetPalette(p)
}

func (c *Controller) SetSize(w, h int) { c.termW, c.termH = w, h }

func (c *Controller) Active() bool { return c.panel.Active() }
func (c *Controller) View() string { return c.panel.View() }
func (c *Controller) Syncing() bool {
	c.expireStaleAutoSync(time.Now())
	return c.syncing
}
func (c *Controller) RepoRoot() string { return c.repoRoot }

// RequestShutdown reports whether a sync is in flight. When it returns true,
// the host should wait; the controller will invoke OnSyncSettled once the sync
// finishes. When false, the host may quit immediately.
func (c *Controller) RequestShutdown() bool {
	c.expireStaleAutoSync(time.Now())
	if !c.syncing {
		return false
	}
	c.shutdownPending = true
	return true
}

// DirtyCount returns the number of files with uncommitted changes, for the
// header git-status cell. Zero means a clean working tree (or no repo).
func (c *Controller) DirtyCount() int { return len(c.stats) }

// StatFor returns the diff stat for an absolute path, for render-time badges.
func (c *Controller) StatFor(abs string) (kbrdfs.DiffStat, bool) {
	s, ok := c.stats[abs]
	if ok {
		return s, true
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil || resolved == abs {
		return kbrdfs.DiffStat{}, false
	}
	s, ok = c.stats[resolved]
	return s, ok
}

// LineChanges returns optional per-line change markers for a file. It is a
// side-effect-free read path: no git init, panel open, or user notification.
func (c *Controller) LineChanges(absPath string) []LineChange {
	return lineChangesFor(c.repoRoot, absPath)
}

// RefreshStats recomputes the diff stats off-thread and delivers them back as a
// git message. The host fires this after a board reload; git owns the data.
func (c *Controller) RefreshStats() tea.Cmd {
	root := c.repoRoot
	return func() tea.Msg { return gitStatsRefreshedMsg{stats: statsFor(root)} }
}

func (c *Controller) refreshStats() { c.stats = statsFor(c.repoRoot) }

// RefreshStatsNow recomputes diff stats synchronously, for host call sites that
// are themselves synchronous (e.g. a script-triggered board refresh). The reload
// pipeline should prefer the off-thread RefreshStats cmd instead.
func (c *Controller) RefreshStatsNow() { c.refreshStats() }

// statsFor computes per-file diff stats for a repo root, or nil when there is no
// repo. Pure, so it is safe to call inside a tea.Cmd goroutine.
func statsFor(repoRoot string) map[string]kbrdfs.DiffStat {
	if repoRoot == "" {
		return nil
	}
	return kbrdfs.GitDiffStats(repoRoot)
}

// HandleKey forwards a key to the panel while it is active.
func (c *Controller) HandleKey(k tea.KeyPressMsg) tea.Cmd { return c.panel.Update(k) }

// HandleMouse forwards mouse input to the panel while it is active.
func (c *Controller) HandleMouse(m tea.MouseMsg) tea.Cmd { return c.panel.HandleMouse(m) }

// gitStatsRefreshedMsg carries off-thread-computed diff stats back to the
// controller (see RefreshStats).
type gitStatsRefreshedMsg struct {
	gitMsg
	stats map[string]kbrdfs.DiffStat
}

// Update dispatches a git message to its handler.
func (c *Controller) Update(m Msg) tea.Cmd {
	switch msg := m.(type) {
	case gitPanelCloseMsg:
		return c.handleGitPanelClose()
	case gitDiffForFileMsg:
		return c.handleGitDiffForFile(msg)
	case gitCommitRequestMsg:
		return c.handleGitCommit(msg)
	case gitPostCommitMsg:
		return c.handleGitPostCommit(msg)
	case gitPullRequestMsg:
		return c.handleGitPull()
	case gitContinueSyncMsg:
		return c.handleGitSync()
	case gitSyncStepMsg:
		return c.handleGitSyncStep(msg)
	case gitLogRequestMsg:
		return c.handleGitLog()
	case gitRefreshMsg:
		return c.handleGitRefresh()
	case gitAddRemoteRequestMsg:
		return c.handleGitAddRemote(msg)
	case gitConnectRemoteSyncRequestMsg:
		return c.handleGitConnectRemoteSync()
	case gitConnectRemoteSyncDoneMsg:
		return c.handleGitConnectRemoteSyncDone(msg)
	case autoSyncTickMsg:
		return c.handleAutoSyncTick()
	case autoSyncDoneMsg:
		return c.handleAutoSyncDone(msg)
	case gitStatsRefreshedMsg:
		c.stats = msg.stats
		return nil
	}
	return nil
}
