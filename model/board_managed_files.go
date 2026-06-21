package model

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/config"
)

type boardManagedFiles struct {
	b *Board
}

func (m boardManagedFiles) openLocalConfig() tea.Cmd {
	return m.open(localConfigPath, ensureConfigFile)
}

func (m boardManagedFiles) openGlobalConfig() tea.Cmd {
	return m.open(globalConfigPath, ensureConfigFile)
}

func (m boardManagedFiles) openLocalCommands() tea.Cmd {
	return m.open(localCommandsPath, ensureCommandsFile)
}

// createLocalMCP writes a .mcp.json into the current board directory pointing
// at kbrd's built-in MCP server, then opens it. The address comes from the
// active board's config so the file matches the running server.
func (m boardManagedFiles) createLocalMCP() tea.Cmd {
	b := m.b
	addr := b.cfg.MCP.Addr
	resolve := func() (string, error) { return filepath.Join(b.cfg.Path, config.FolderMCPFile), nil }
	ensure := func(path string) error { return ensureMCPFile(path, addr) }
	return m.open(resolve, ensure)
}

// createLocalAgents writes an AGENTS.md describing kbrd into the current board
// directory, then opens it.
func (m boardManagedFiles) createLocalAgents() tea.Cmd {
	b := m.b
	resolve := func() (string, error) { return filepath.Join(b.cfg.Path, config.FolderAgentsFile), nil }
	return m.open(resolve, ensureAgentsFile)
}

func (m boardManagedFiles) open(resolve func() (string, error), ensure func(string) error) tea.Cmd {
	b := m.b
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

func (b *Board) managedFiles() boardManagedFiles {
	return boardManagedFiles{b: b}
}
