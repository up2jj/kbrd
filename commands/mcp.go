package commands

import (
	"fmt"
	"text/tabwriter"

	"kbrd/mcpsetup"

	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage kbrd MCP integration",
	}
	cmd.AddCommand(newMCPSetupCmd())
	return cmd
}

func newMCPSetupCmd() *cobra.Command {
	var opts mcpsetup.Options
	opts.EnableKBRD = true

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Register kbrd's built-in MCP server with agent tools",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			results, err := mcpsetup.Run(opts)
			if err != nil {
				return err
			}
			printMCPSetupResults(cmd, results)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Addr, "addr", "", "MCP server address (default from global config or 127.0.0.1:7777)")
	cmd.Flags().StringArrayVar(&opts.Clients, "client", nil, "client to configure: codex or claude (repeatable; default discovers installed clients)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "show intended changes without writing config")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "replace existing kbrd MCP entries")
	cmd.Flags().BoolVar(&opts.EnableKBRD, "enable-kbrd", true, "enable kbrd's built-in MCP server in global config")
	cmd.Flags().BoolFunc("no-enable-kbrd", "skip writing kbrd's global MCP startup config", func(string) error {
		opts.EnableKBRD = false
		return nil
	})
	return cmd
}

func printMCPSetupResults(cmd *cobra.Command, results []mcpsetup.Result) {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "target\tstatus\tpath\tdetail")
	fmt.Fprintln(w, "------\t------\t----\t------")
	for _, r := range results {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Target, r.Status, displayPath(r.Path), r.Detail)
	}
	_ = w.Flush()
}

func displayPath(path string) string {
	if path == "" {
		return "-"
	}
	return path
}
