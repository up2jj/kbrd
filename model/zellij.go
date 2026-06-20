package model

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// inZellij reports whether kbrd is running inside a zellij session. zellij
// exports the ZELLIJ env var into every pane of a session, mirroring the
// TERM_PROGRAM detection in notify.go.
func inZellij() bool { return os.Getenv("ZELLIJ") != "" }

// resolveEditor returns the user's preferred editor following the conventional
// $VISUAL → $EDITOR → vi precedence. We pass this through to `zellij edit` so
// the fallback holds even when zellij would otherwise refuse on an unset
// $EDITOR.
func resolveEditor() string {
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	return "vi"
}

// zellijDoneMsg reports the outcome of any pane launch (editor or shell). desc
// is the ready-to-show toast text; path/paneID are set only for editor panes,
// so a later open of the same card focuses the existing pane instead of
// spawning a duplicate.
type zellijDoneMsg struct {
	path   string
	paneID string
	desc   string
	err    error
}

// Zellij is the self-contained controller for kbrd's zellij integration: env
// detection, the `z` actions menu (it doubles as the modal), per-card editor
// pane tracking, and the launch/focus/rename helpers. board.go only wires
// dispatch into it, keeping that file thin.
type Zellij struct {
	Enabled     bool   // inZellij(), set once at construction
	active      bool   // menu currently open
	boardDir    string // captured at OpenMenu (board root, for --cwd)
	path        string // selected card FullPath
	cardName    string
	editorPanes map[string]string // card FullPath → zellij pane id
	palette     Palette
}

func NewZellij() Zellij {
	return Zellij{Enabled: inZellij(), editorPanes: map[string]string{}}
}

func (z *Zellij) SetPalette(p Palette) { z.palette = p }

func (z *Zellij) Active() bool { return z.active }

// StartCmd performs the one-time startup integration (naming the tab after the
// board). Returns nil when not running inside zellij. boardName falls back to
// the board directory's base name when empty.
func (z *Zellij) StartCmd(boardName, boardPath string) tea.Cmd {
	if !z.Enabled {
		return nil
	}
	if boardName == "" {
		boardName = filepath.Base(boardPath)
	}
	return z.renameTabCmd(boardName)
}

// OpenFor opens the actions menu for the column's selected card. It is a no-op
// for a virtual/separator row or an empty column, so the caller can route the
// key unconditionally.
func (z *Zellij) OpenFor(boardDir string, col *Column) {
	if !col.HasSelectedItem() || col.SelectedItem().Separator {
		return
	}
	item := col.SelectedItem()
	z.active = true
	z.boardDir = boardDir
	z.path = item.FullPath
	z.cardName = item.Name
}

func (z *Zellij) close() {
	z.active = false
	z.boardDir = ""
	z.path = ""
	z.cardName = ""
}

// Done records the editor pane (so reopening focuses it) and returns the toast
// for any completed launch. The notifier is passed in rather than stored so the
// controller keeps no board-wide dependencies.
func (z *Zellij) Done(msg zellijDoneMsg, n *Notifier) tea.Cmd {
	if msg.err != nil {
		return n.Send("zellij: "+msg.err.Error(), notifyError)
	}
	if msg.paneID != "" {
		if z.editorPanes == nil {
			z.editorPanes = map[string]string{}
		}
		z.editorPanes[msg.path] = msg.paneID
	}
	return n.Send(msg.desc, notifySuccess)
}

// Update handles the menu's own keys. It is a keyed menu (press f/e/s), not a
// cursor list, so there is no selection to move. Returns a launch tea.Cmd when
// the user picks an action.
func (z *Zellij) Update(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, Keys.ZellijFloating):
		boardDir, path, existing := z.boardDir, z.path, z.editorPanes[z.path]
		z.close()
		return openEditorCmd(boardDir, path, true, existing)
	case key.Matches(msg, Keys.ZellijTiled):
		boardDir, path, existing := z.boardDir, z.path, z.editorPanes[z.path]
		z.close()
		return openEditorCmd(boardDir, path, false, existing)
	case key.Matches(msg, Keys.ZellijShell):
		boardDir := z.boardDir
		z.close()
		return shellPaneCmd(boardDir)
	case key.Matches(msg, Keys.ZellijClose):
		z.close()
	}
	return nil
}

func (z *Zellij) View() string {
	heading := "zellij actions"
	if z.cardName != "" {
		heading = "zellij · " + z.cardName
	}

	// Keyed menu: emphasize the mnemonic so it reads as the button to press,
	// with the label in the normal foreground.
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(z.palette.Primary)
	labelStyle := lipgloss.NewStyle().Foreground(z.palette.FgBase)
	row := func(key, label string) string {
		return " " + keyStyle.Render(key) + "   " + labelStyle.Render(label)
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		row("f", "open in floating editor pane"),
		row("e", "open in new tiled pane"),
		row("s", "shell in board dir"),
	)

	footer := RenderInlineHints([]Shortcut{{"f/e/s", "select"}, {"esc", "cancel"}})
	return OverlayFrame{Title: heading, Body: body, Footer: footer, Palette: z.palette}.Render()
}

// openEditorCmd opens path in the user's editor in a new zellij pane via
// `zellij edit`. When existingID names a still-live pane it is focused instead,
// avoiding duplicates; a stale id falls through to a fresh pane. `zellij edit`
// returns immediately (the pane is session-owned), so kbrd stays in altscreen —
// no suspend/ExecProcess needed.
func openEditorCmd(boardDir, path string, floating bool, existingID string) tea.Cmd {
	return func() tea.Msg {
		if existingID != "" {
			if exec.Command("zellij", "action", "focus-pane-id", existingID).Run() == nil {
				return zellijDoneMsg{path: path, paneID: existingID, desc: "focused editor"}
			}
			// Pane is gone — fall through and open a fresh one.
		}
		args := []string{"edit", "--cwd", boardDir}
		if floating {
			args = append(args, "-f")
		}
		args = append(args, path)
		cmd := exec.Command("zellij", args...)
		ed := resolveEditor()
		cmd.Env = append(os.Environ(), "EDITOR="+ed, "VISUAL="+ed)
		out, err := cmd.Output()
		if err != nil {
			return zellijDoneMsg{path: path, err: err}
		}
		desc := "opened tiled editor"
		if floating {
			desc = "opened floating editor"
		}
		return zellijDoneMsg{path: path, paneID: parsePaneID(out), desc: desc}
	}
}

// shellPaneCmd opens a shell pane scoped to the board directory.
func shellPaneCmd(boardDir string) tea.Cmd {
	return func() tea.Msg {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "sh"
		}
		err := exec.Command("zellij", "run", "--cwd", boardDir, "--", shell).Run()
		return zellijDoneMsg{desc: "opened shell", err: err}
	}
}

// renameTabCmd labels the current zellij tab with the board name. Best-effort:
// failures are swallowed (the integration is cosmetic and must never block).
func (z *Zellij) renameTabCmd(name string) tea.Cmd {
	return func() tea.Msg {
		_ = exec.Command("zellij", "action", "rename-tab", name).Run()
		return nil
	}
}

// parsePaneID extracts a pane id from `zellij edit` output. The command prints
// the created pane id; we keep the last whitespace-delimited token, tolerating
// any surrounding label. An empty result simply disables reuse for that card
// (the next open spawns a fresh pane).
func parsePaneID(out []byte) string {
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}
