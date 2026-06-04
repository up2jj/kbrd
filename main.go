package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"kbrd/config"
	"kbrd/mcp"
	"kbrd/model"
	"kbrd/recents"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"
)

// cliFlags holds the parsed command-line options.
type cliFlags struct {
	initGlobal bool
	initLocal  bool
	mcp        bool   // start the built-in MCP server
	mcpAddr    string // address override; does not by itself enable the server
}

func main() {
	flags := parseFlags(os.Args[1:])

	cwd, err := os.Getwd()
	if err != nil {
		fatal("cannot determine working directory: %v", err)
	}

	// Template scaffolding subcommands write and exit; they never open a board.
	switch {
	case flags.initGlobal:
		if err := writeGlobalTemplate(); err != nil {
			fatal("%v", err)
		}
		return
	case flags.initLocal:
		if err := writeLocalTemplate(cwd); err != nil {
			fatal("%v", err)
		}
		return
	}

	if err := runBoard(cwd, flags); err != nil {
		fatal("%v", err)
	}
}

// parseFlags parses argv into cliFlags. pflag.ExitOnError handles parse errors.
func parseFlags(args []string) cliFlags {
	fs := pflag.NewFlagSet("kbrd", pflag.ExitOnError)
	var f cliFlags
	fs.BoolVar(&f.initGlobal, "init-config", false, "write a TOML config template to the user config dir and exit")
	fs.BoolVar(&f.initLocal, "init-local-config", false, "write a TOML config template to the current directory and exit")
	fs.BoolVar(&f.mcp, "mcp", false, "start the built-in MCP server")
	fs.StringVar(&f.mcpAddr, "mcp-addr", "", "MCP server listen address (overrides config; default 127.0.0.1:7777)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	return f
}

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

	if abs, absErr := filepath.Abs(cwd); absErr == nil {
		store, _ := recents.Load()
		store.Touch(abs, cfg.BoardName)
		_ = store.Save()
	}

	mcpCloser, mcpStatus := startMCP(cfg, flags)
	if mcpCloser != nil {
		defer mcpCloser.Close()
	}

	m := model.NewBoard(cfg)
	m.SetMCPStatus(mcpStatus)

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
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

// fatal prints an error to stderr and exits with status 1.
func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func writeGlobalTemplate() error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("user config dir: %w", err)
	}
	appDir := filepath.Join(dir, config.AppDirName)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", appDir, err)
	}
	target := filepath.Join(appDir, config.GlobalConfigFile)
	return writeTemplate(target)
}

func writeLocalTemplate(cwd string) error {
	return writeTemplate(filepath.Join(cwd, config.FolderConfigFile))
}

func writeTemplate(target string) error {
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("refusing to overwrite existing file: %s", target)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", target, err)
	}
	if err := os.WriteFile(target, config.Template, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	fmt.Printf("wrote %s\n", target)
	return nil
}
