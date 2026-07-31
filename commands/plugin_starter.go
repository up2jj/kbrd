package commands

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newPluginStarterCmd(boardDir *string) *cobra.Command {
	cmd := &cobra.Command{Use: "starter", Short: "List and apply board starter kits from locked plugins"}
	cmd.AddCommand(newPluginStarterListCmd(boardDir), newPluginStarterApplyCmd(boardDir))
	return cmd
}

func newPluginStarterListCmd(boardDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list [marketplace/plugin]",
		Short: "List board starter kits from enabled locked plugins",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pluginID := ""
			if len(args) == 1 {
				pluginID = args[0]
			}
			return listPluginStarters(cmd.OutOrStdout(), *boardDir, pluginID)
		},
	}
}

func listPluginStarters(out io.Writer, boardDir, pluginID string) error {
	manager, err := pluginManager()
	if err != nil {
		return err
	}
	starters, err := manager.BoardStarters(boardDir)
	if err != nil {
		return err
	}
	printed := false
	for _, starter := range starters {
		if pluginID != "" && starter.PluginID != pluginID {
			continue
		}
		fmt.Fprintf(out, "%s %s\n", starter.PluginID, starter.Name)
		printed = true
	}
	if !printed {
		fmt.Fprintln(out, "no board starter kits")
	}
	return nil
}

func newPluginStarterApplyCmd(boardDir *string) *cobra.Command {
	var target string
	var force bool
	cmd := &cobra.Command{
		Use:   "apply <marketplace/plugin> <starter>",
		Short: "Copy a verified starter kit into a board without changing its lock",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return applyPluginStarter(cmd.OutOrStdout(), *boardDir, args[0], args[1], target, force)
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "target directory (default board directory)")
	cmd.Flags().BoolVar(&force, "force", false, "replace conflicting files; never replaces the board lock or .git")
	return cmd
}

func applyPluginStarter(out io.Writer, boardDir, pluginID, name, target string, force bool) error {
	manager, err := pluginManager()
	if err != nil {
		return err
	}
	if err := manager.ApplyBoardStarter(boardDir, pluginID, name, target, force); err != nil {
		return err
	}
	destination := target
	if destination == "" {
		destination = boardDir
	}
	fmt.Fprintf(out, "applied %s starter %s to %s\n", pluginID, name, destination)
	return nil
}
