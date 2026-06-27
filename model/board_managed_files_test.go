package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"kbrd/config"
)

func stubOpenFile(t *testing.T) *[]string {
	t.Helper()
	opened := []string{}
	oldOpenFile := openFile
	openFile = func(path string) error {
		opened = append(opened, path)
		return nil
	}
	t.Cleanup(func() { openFile = oldOpenFile })
	return &opened
}

func TestBoardManagedFiles_OpenLocalFiles(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	opened := stubOpenFile(t)

	b := &Board{notifier: NewNotifier("none")}
	runManagedFileCmd(t, b.managedFiles().openLocalConfig())
	runManagedFileCmd(t, b.managedFiles().openLocalCommands())

	wantConfig := filepath.Join(cwd, config.FolderConfigFile)
	wantCommands := filepath.Join(cwd, config.FolderCommandsFile)
	if _, err := os.Stat(wantConfig); err != nil {
		t.Fatalf("local config not created: %v", err)
	}
	if _, err := os.Stat(wantCommands); err != nil {
		t.Fatalf("local commands not created: %v", err)
	}
	if got := *opened; len(got) != 2 || got[0] != wantConfig || got[1] != wantCommands {
		t.Fatalf("opened paths = %v, want [%q %q]", got, wantConfig, wantCommands)
	}
}

func TestBoardManagedFiles_CreateBoardScopedFiles(t *testing.T) {
	boardDir := t.TempDir()
	opened := stubOpenFile(t)

	b := &Board{
		cfg:      config.Config{Path: boardDir, MCP: config.MCPConfig{Addr: "127.0.0.1:7777"}},
		notifier: NewNotifier("none"),
	}
	runManagedFileCmd(t, b.managedFiles().createLocalMCP())
	runManagedFileCmd(t, b.managedFiles().createLocalAgents())

	mcpPath := filepath.Join(boardDir, config.FolderMCPFile)
	agentsPath := filepath.Join(boardDir, config.FolderAgentsFile)
	mcpData, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("read mcp file: %v", err)
	}
	if !strings.Contains(string(mcpData), `"http://127.0.0.1:7777"`) {
		t.Fatalf("mcp file did not use board config address: %s", mcpData)
	}
	if _, err := os.Stat(agentsPath); err != nil {
		t.Fatalf("agents file not created: %v", err)
	}
	if got := *opened; len(got) != 2 || got[0] != mcpPath || got[1] != agentsPath {
		t.Fatalf("opened paths = %v, want [%q %q]", got, mcpPath, agentsPath)
	}
}

func runManagedFileCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected command, got nil")
	}
	_ = cmd()
}
