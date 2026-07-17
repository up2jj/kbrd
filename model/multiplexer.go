package model

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Multiplexer exposes the terminal operations used by the board. Implementations
// decide how an intent maps to their native pane/window model.
type Multiplexer interface {
	Name() string
	Supports(MultiplexerCapability) bool
	FocusPane(id string) error
	OpenEditor(boardDir, path string, placement EditorPlacement) (string, error)
	OpenShell(boardDir string) error
	RenameWorkspace(name string) error
}

type MultiplexerCapability uint8

const FloatingPanes MultiplexerCapability = iota

type EditorPlacement uint8

const (
	EditorPreferred EditorPlacement = iota
	EditorTiled
)

// muxRunner is the process boundary used by concrete backends. Keeping it
// narrow makes command construction testable without starting a multiplexer.
type muxRunner interface {
	Run(name string, args ...string) error
	Output(name string, args []string, env []string) ([]byte, error)
}

type execMuxRunner struct{}

func (execMuxRunner) Run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func (execMuxRunner) Output(name string, args []string, env []string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	if env != nil {
		cmd.Env = env
	}
	return cmd.Output()
}

// terminalDoneMsg reports an asynchronous multiplexer operation. path/paneID
// are populated for editors so reopening a card can focus its existing pane.
type terminalDoneMsg struct {
	backend string
	path    string
	paneID  string
	desc    string
	err     error
}

// Terminal owns terminal-menu state and pane reuse. It contains no
// multiplexer-specific command knowledge.
type Terminal struct {
	backend     Multiplexer
	active      bool
	boardDir    string
	path        string
	cardName    string
	editorPanes map[string]string
	palette     Palette
}

func NewTerminal() Terminal {
	return newTerminal(os.Getenv, execMuxRunner{})
}

func newTerminal(getenv func(string) string, runner muxRunner) Terminal {
	return Terminal{
		backend:     detectMultiplexer(getenv, runner),
		editorPanes: map[string]string{},
	}
}

func detectMultiplexer(getenv func(string) string, runner muxRunner) Multiplexer {
	switch {
	case getenv("ZELLIJ") != "":
		return zellijMultiplexer{runner: runner}
	case getenv("TMUX") != "":
		return tmuxMultiplexer{runner: runner}
	default:
		return nil
	}
}

func multiplexerAvailable() bool {
	return os.Getenv("ZELLIJ") != "" || os.Getenv("TMUX") != ""
}

func (t *Terminal) Enabled() bool        { return t.backend != nil }
func (t *Terminal) SetPalette(p Palette) { t.palette = p }
func (t *Terminal) Active() bool         { return t.active }

// StartCmd performs best-effort workspace naming for the selected backend.
func (t *Terminal) StartCmd(boardName, boardPath string) tea.Cmd {
	if t.backend == nil {
		return nil
	}
	if boardName == "" {
		boardName = filepath.Base(boardPath)
	}
	backend := t.backend
	return func() tea.Msg {
		_ = backend.RenameWorkspace(boardName)
		return nil
	}
}

func (t *Terminal) OpenFor(boardDir string, col *Column) {
	if t.backend == nil || !col.HasSelectedItem() || col.SelectedItem().Separator {
		return
	}
	item := col.SelectedItem()
	t.active = true
	t.boardDir = boardDir
	t.path = item.FullPath
	t.cardName = item.Name
}

func (t *Terminal) close() {
	t.active = false
	t.boardDir = ""
	t.path = ""
	t.cardName = ""
}

func (t *Terminal) Done(msg terminalDoneMsg, n *Notifier) tea.Cmd {
	if msg.err != nil {
		return n.ErrorCause(msg.backend, msg.err)
	}
	if msg.paneID != "" {
		t.editorPanes[msg.path] = msg.paneID
	}
	return n.Success(msg.desc)
}

func (t *Terminal) Update(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, Keys.TerminalPreferred):
		return t.openEditor(EditorPreferred)
	case key.Matches(msg, Keys.TerminalTiled):
		return t.openEditor(EditorTiled)
	case key.Matches(msg, Keys.TerminalShell):
		backend, boardDir := t.backend, t.boardDir
		t.close()
		return func() tea.Msg {
			err := backend.OpenShell(boardDir)
			return terminalDoneMsg{backend: backend.Name(), desc: "opened shell", err: err}
		}
	case key.Matches(msg, Keys.TerminalClose):
		t.close()
	}
	return nil
}

func (t *Terminal) openEditor(placement EditorPlacement) tea.Cmd {
	backend := t.backend
	boardDir, path, existing := t.boardDir, t.path, t.editorPanes[t.path]
	t.close()
	return func() tea.Msg {
		if existing != "" && backend.FocusPane(existing) == nil {
			return terminalDoneMsg{backend: backend.Name(), path: path, paneID: existing, desc: "focused editor"}
		}
		paneID, err := backend.OpenEditor(boardDir, path, placement)
		if err != nil {
			return terminalDoneMsg{backend: backend.Name(), path: path, err: err}
		}
		desc := "opened editor window"
		if placement == EditorTiled {
			desc = "opened tiled editor"
		} else if backend.Supports(FloatingPanes) {
			desc = "opened floating editor"
		}
		return terminalDoneMsg{backend: backend.Name(), path: path, paneID: paneID, desc: desc}
	}
}

func (t *Terminal) View() string {
	heading := "terminal actions"
	if t.cardName != "" {
		heading = "terminal · " + t.cardName
	}
	preferred := "open editor in new window"
	if t.backend != nil && t.backend.Supports(FloatingPanes) {
		preferred = "open in floating editor pane"
	}
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(t.palette.Primary)
	labelStyle := lipgloss.NewStyle().Foreground(t.palette.FgBase)
	row := func(shortcut, label string) string {
		return " " + keyStyle.Render(shortcut) + "   " + labelStyle.Render(label)
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		row("f", preferred),
		row("e", "open in new tiled pane"),
		row("s", "shell in board dir"),
	)
	footer := RenderInlineHints([]Shortcut{{"f/e/s", "select"}, {"esc", "cancel"}})
	return OverlayFrame{Title: heading, Body: body, Footer: footer, Palette: t.palette}.Render()
}

func resolveEditor() string {
	if visual := os.Getenv("VISUAL"); visual != "" {
		return visual
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	return "vi"
}

func parsePaneID(out []byte) string {
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}
