package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"kbrd/config"
	"kbrd/mcp"
	"kbrd/model"
	"kbrd/recents"

	tea "charm.land/bubbletea/v2"
)

// runBoard validates the working directory, loads config, brings up the optional
// MCP server, and runs the TUI to completion.
func runBoard(cwd string, flags cliFlags) error {
	info, err := os.Stat(cwd)
	if err != nil {
		return fmt.Errorf("cannot access path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory")
	}

	cfg, err := config.Load(cwd)
	if err != nil {
		return err
	}
	cfg.InstanceName = config.ResolveInstanceName(flags.instance)

	// --safe neuters every board-supplied code path. Applied after config load
	// so it overrides config — including a folder-local kbrd.toml that tried to
	// enable any of these — which is the one layer a board cannot ship around.
	if flags.safe {
		cfg.Scripting.Enabled = false
		cfg.Hooks.Enabled = false
		cfg.Template.Exec = false
	}

	if abs, absErr := filepath.Abs(cwd); absErr == nil {
		store, _ := recents.Load()
		store.Touch(abs, cfg.BoardName)
		_ = store.Save()
	}

	mcpCloser, mcpStatus := startMCP(cfg, flags)
	if mcpCloser != nil {
		defer mcpCloser.Close()
	}

	m := model.NewBoardWithOptions(cfg, model.BoardOptions{Safe: flags.safe})
	m.SetMCPStatus(mcpStatus)

	p := tea.NewProgram(m)
	finalModel, runErr := p.Run()
	if bd, ok := finalModel.(*model.Board); ok {
		bd.Close()
	}
	return runErr
}

// startMCP applies the opt-in policy — start when --mcp is passed or the config
// enables it — then delegates the mechanics to mcp.Start. Returns the closer
// (nil unless the listener bound) and the status the header chip reflects:
// off when not requested, running on success, failed when the bind was
// attempted but could not bind (e.g. the port is already in use).
func startMCP(cfg config.Config, flags cliFlags) (io.Closer, model.MCPStatus) {
	if !flags.mcp && !cfg.MCP.Enabled {
		return nil, model.MCPOff
	}
	addr := cfg.MCP.Addr
	if flags.mcpAddr != "" {
		addr = flags.mcpAddr
	}
	c, ok := mcp.Start(model.Version, addr)
	if !ok {
		return nil, model.MCPFailed
	}
	return c, model.MCPRunning
}
