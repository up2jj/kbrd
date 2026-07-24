package commands

import (
	"fmt"

	"kbrd/script"

	"github.com/spf13/cobra"
)

// newCacheCmd builds `kbrd cache`, a parent for cache-management commands. It is
// nested as `cache` → `script` so other cache categories can be added later
// without crowding the top-level command list.
func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage kbrd's local caches",
	}
	cmd.AddCommand(newCacheScriptCmd())
	return cmd
}

// newCacheScriptCmd builds `kbrd cache script`, grouping operations on the
// remote Lua script cache (modules fetched via require("https://...") /
// require("github:...")).
func newCacheScriptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "script",
		Short: "Manage the remote Lua script cache",
	}
	cmd.AddCommand(newCacheScriptPurgeCmd(), newCacheScriptListCmd())
	return cmd
}

func newCacheScriptPurgeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "purge",
		Short: "Remove all cached remote Lua scripts (they re-fetch on next use)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := script.PurgeRemoteCache()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %d cached script(s)\n", n)
			return nil
		},
	}
}

func newCacheScriptListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List cached remote Lua scripts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := script.ListRemoteCache()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no cached remote scripts")
				return nil
			}
			for _, e := range entries {
				fmt.Fprintf(cmd.OutOrStdout(), "%-8d %s\n", e.Bytes, e.URL)
			}
			return nil
		},
	}
}
