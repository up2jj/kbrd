// Package git owns all git orchestration for a board: the git panel UI, diffing,
// commit/sync, the per-item diff-stat data, and the background auto-sync loop.
// It is deliberately a sibling of the model package (model imports git, never the
// reverse) so the boundary is enforced by the compiler. Everything it needs from
// the host is injected via Deps; per-item stats are read back via StatFor.
package git

import (
	tea "github.com/charmbracelet/bubbletea"

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
// learns what it does). OnSyncSettled is invoked once an in-flight auto-sync
// finishes after a shutdown was requested, so the host can finally quit.
type Deps struct {
	Cfg           config.Config
	Notifier      Notifier
	Bus           *events.Bus
	BeforeCommit  func() error
	OnSyncSettled func() tea.Cmd
}

// Controller holds all git state and orchestrates the panel + background work.
type Controller struct {
	cfg           config.Config
	notifier      Notifier
	bus           *events.Bus
	beforeCommit  func() error
	onSyncSettled func() tea.Cmd

	panel    GitPanel
	repoRoot string
	stats    map[string]kbrdfs.DiffStat

	syncing         bool // auto-sync in progress
	shutdownPending bool // host asked to quit; signal once the in-flight sync settles

	termW, termH int
}

func New(d Deps) Controller {
	return Controller{
		cfg:           d.Cfg,
		notifier:      d.Notifier,
		bus:           d.Bus,
		beforeCommit:  d.BeforeCommit,
		onSyncSettled: d.OnSyncSettled,
	}
}

// Detect locates the repo root for the configured path and computes the initial
// diff stats. Called once at startup (off the main goroutine).
func (c *Controller) Detect() {
	c.repoRoot = kbrdfs.GitRepoRoot(c.cfg.Path)
	c.refreshStats()
}

// StartAutoSync returns the initial auto-sync tick (nil if disabled).
func (c *Controller) StartAutoSync() tea.Cmd { return c.scheduleAutoSync() }

func (c *Controller) SetPalette(p theme.Palette) {
	c.panel.SetPalette(p)
	setGitStyles(p)
}

func (c *Controller) SetSize(w, h int) { c.termW, c.termH = w, h }

func (c *Controller) Active() bool     { return c.panel.Active() }
func (c *Controller) View() string     { return c.panel.View() }
func (c *Controller) Syncing() bool    { return c.syncing }
func (c *Controller) RepoRoot() string { return c.repoRoot }

// RequestShutdown reports whether an auto-sync is in flight. When it returns
// true, the host should wait; the controller will invoke OnSyncSettled once the
// sync finishes. When false, the host may quit immediately.
func (c *Controller) RequestShutdown() bool {
	if !c.syncing {
		return false
	}
	c.shutdownPending = true
	return true
}

// StatFor returns the diff stat for an absolute path, for render-time badges.
func (c *Controller) StatFor(abs string) (kbrdfs.DiffStat, bool) {
	s, ok := c.stats[abs]
	return s, ok
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
func (c *Controller) HandleKey(k tea.KeyMsg) tea.Cmd { return c.panel.Update(k) }

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
	case gitSyncRequestMsg:
		return c.handleGitSync()
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
