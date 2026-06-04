package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"kbrd/config"
	fsutil "kbrd/fs"
	"kbrd/mcp"
	"kbrd/model"
	"kbrd/recents"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// cliFlags holds the parsed command-line options.
type cliFlags struct {
	mcp     bool   // start the built-in MCP server
	mcpAddr string // address override; does not by itself enable the server
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// newRootCmd builds the kbrd command tree. The root, run with no subcommand,
// opens the board in the current directory.
func newRootCmd() *cobra.Command {
	var flags cliFlags

	root := &cobra.Command{
		Use:           "kbrd",
		Short:         "Keyboard-driven, file-based Kanban board for the terminal",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("cannot determine working directory: %w", err)
			}
			return runBoard(cwd, flags)
		},
	}

	// Persistent so subcommands that open a board (e.g. clone) honor them too.
	root.PersistentFlags().BoolVar(&flags.mcp, "mcp", false, "start the built-in MCP server")
	root.PersistentFlags().StringVar(&flags.mcpAddr, "mcp-addr", "", "MCP server listen address (overrides config; default 127.0.0.1:7777)")

	root.AddCommand(newInitCmd(), newCloneCmd(&flags))
	return root
}

// newInitCmd builds `kbrd init`, which scaffolds a config template — a local
// kbrd.toml in the current directory by default, or the user config dir with
// --global.
func newInitCmd() *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a config template (local kbrd.toml by default)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if global {
				return writeGlobalTemplate()
			}
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("cannot determine working directory: %w", err)
			}
			return writeLocalTemplate(cwd)
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "write to the user config dir instead of the current directory")
	return cmd
}

// newCloneCmd builds `kbrd clone <repo-url> [dir]`, which git-clones a board
// repo and, unless --no-open is set, opens it in the TUI.
func newCloneCmd(flags *cliFlags) *cobra.Command {
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "clone <repo-url> [dir]",
		Short: "Clone a board repository and open it",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ""
			if len(args) == 2 {
				dir = args[1]
			}
			abs, err := runClone(args[0], dir)
			if err != nil {
				return err
			}
			if noOpen {
				return nil
			}
			return runBoard(abs, *flags)
		},
	}
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "clone only; do not open the board")
	return cmd
}

// runClone clones url into dir (derived from the URL when empty), registers the
// result in recents, and returns the absolute path to the cloned board.
func runClone(url, dir string) (string, error) {
	if dir == "" { // derive from URL: foo/bar.git -> bar
		dir = strings.TrimSuffix(filepath.Base(url), ".git")
	}
	if dir == "" || dir == "." || dir == "/" {
		return "", fmt.Errorf("cannot determine target directory from %q; pass an explicit dir", url)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err == nil {
		return "", fmt.Errorf("target directory already exists: %s", abs)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("cannot access target: %w", err)
	}
	if err := fsutil.GitClone(url, abs); err != nil {
		return "", err
	}
	store, _ := recents.Load()
	store.Touch(abs, "") // basename label, matching runBoard
	_ = store.Save()
	fmt.Printf("cloned %s into %s\n", url, abs)
	return abs, nil
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
