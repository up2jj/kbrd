// Package commands defines the kbrd CLI command tree (cobra commands) and the
// run logic behind each. main() does nothing but call NewRootCmd().Execute().
package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// cliFlags holds the parsed command-line options.
type cliFlags struct {
	mcp      bool   // start the built-in MCP server
	mcpAddr  string // address override; does not by itself enable the server
	safe     bool   // disable all board-supplied code: scripting, hooks, template exec
	instance string // machine-local instance name for routing instance-scoped timers
}

// NewRootCmd builds the kbrd command tree. The root, run with no subcommand,
// opens the board in the current directory.
func NewRootCmd() *cobra.Command {
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
	root.PersistentFlags().BoolVar(&flags.safe, "safe", false, "disable all board-supplied code: Lua scripting, event hooks, and template shell exec (overrides config)")
	root.PersistentFlags().StringVar(&flags.instance, "name", "", "instance name for routing instance-scoped Lua timers (env KBRD_INSTANCE, default hostname)")

	root.AddCommand(newInitCmd(), newCloneCmd(&flags), newServeCmd(), newCacheCmd(), newMCPCmd())
	return root
}
