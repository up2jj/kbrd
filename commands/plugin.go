package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"kbrd/plugin"

	"github.com/spf13/cobra"
)

func newPluginCmd() *cobra.Command {
	var boardDir string
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage board-local Lua plugins",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if boardDir == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("determine board directory: %w", err)
				}
				boardDir = cwd
			}
			abs, err := filepath.Abs(boardDir)
			if err != nil {
				return fmt.Errorf("resolve board directory: %w", err)
			}
			boardDir = abs
			return nil
		},
	}
	cmd.PersistentFlags().StringVar(&boardDir, "board", "", "board directory (default current directory)")
	cmd.AddCommand(
		newPluginMarketplaceCmd(),
		newPluginSearchCmd(),
		newPluginInfoCmd(&boardDir),
		newPluginAddCmd(&boardDir),
		newPluginListCmd(&boardDir),
		newPluginRemoveCmd(&boardDir),
		newPluginSyncCmd(&boardDir),
		newPluginUpdateCmd(&boardDir),
		newPluginValidateCmd(),
	)
	return cmd
}

func newPluginInfoCmd(boardDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "info <marketplace/plugin>",
		Short: "Show declarative plugin metadata without executing it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			info, err := manager.Info(*boardDir, args[0])
			if err != nil {
				return err
			}
			printPluginInfo(cmd, info)
			return nil
		},
	}
}

func printPluginInfo(cmd *cobra.Command, info plugin.PluginInfo) {
	manifest := info.Manifest
	fmt.Fprintf(cmd.OutOrStdout(), "Plugin: %s\n", info.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "Description: %s\n", manifest.Description)
	fmt.Fprintf(cmd.OutOrStdout(), "Author: %s\n", formatOwner(manifest.Author))
	fmt.Fprintf(cmd.OutOrStdout(), "License: %s\n", valueOr(manifest.License, "not declared"))
	fmt.Fprintf(cmd.OutOrStdout(), "Homepage: %s\n", valueOr(manifest.Homepage, "not declared"))
	installedVersion := "not installed"
	if info.Installed != nil {
		installedVersion = valueOr(info.Installed.Version, "unspecified")
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Installed version: %s\n", installedVersion)
	fmt.Fprintf(cmd.OutOrStdout(), "Available version: %s\n", valueOr(manifest.Version, "unspecified"))
	fmt.Fprintf(cmd.OutOrStdout(), "Marketplace URL: %s\n", info.Marketplace.URL)
	fmt.Fprintf(cmd.OutOrStdout(), "Marketplace commit: %s\n", info.Marketplace.Commit)
	fmt.Fprintf(cmd.OutOrStdout(), "Commands: %s\n", formatDeclarations(manifest.Commands))
	fmt.Fprintf(cmd.OutOrStdout(), "Hooks: %s\n", formatDeclarations(manifest.Hooks))
	fmt.Fprintf(cmd.OutOrStdout(), "Layers: %s\n", formatDeclarations(manifest.Layers))
	fmt.Fprintf(cmd.OutOrStdout(), "Timers: %s\n", formatDeclarations(manifest.Timers))
	fmt.Fprintf(cmd.OutOrStdout(), "Network access: %t\n", manifest.NetworkAccess)
	fmt.Fprintf(cmd.OutOrStdout(), "Shell access: %t\n", manifest.ShellAccess)
	fmt.Fprintf(cmd.OutOrStdout(), "README: %s\n", valueOr(manifest.README, "not declared"))
	fmt.Fprintf(cmd.OutOrStdout(), "Changelog: %s\n", valueOr(manifest.Changelog, "not declared"))
}

func formatOwner(owner plugin.Owner) string {
	if owner.Name == "" {
		return "not declared"
	}
	if owner.URL == "" {
		return owner.Name
	}
	return owner.Name + " (" + owner.URL + ")"
}

func formatDeclarations(values []string) string {
	if len(values) == 0 {
		return "none declared"
	}
	return strings.Join(values, ", ")
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func pluginManager() (*plugin.Manager, error) { return plugin.DefaultManager() }

func newPluginMarketplaceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "marketplace", Aliases: []string{"market"}, Short: "Manage Git plugin marketplaces"}
	cmd.AddCommand(newPluginMarketplaceAddCmd(), newPluginMarketplaceListCmd(), newPluginMarketplaceUpdateCmd(), newPluginMarketplaceRemoveCmd())
	return cmd
}

func newPluginMarketplaceAddCmd() *cobra.Command {
	var ref string
	cmd := &cobra.Command{
		Use:   "add <git-url>",
		Short: "Register a Git repository as a marketplace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			marketplace, err := manager.AddMarketplace(cmd.Context(), args[0], ref)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added marketplace %s at %s\n", marketplace.Name, shortCommit(marketplace.Commit))
			return nil
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "branch or tag to track (default remote HEAD)")
	return cmd
}

func newPluginMarketplaceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered marketplaces",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			marketplaces, err := manager.Marketplaces()
			if err != nil {
				return err
			}
			if len(marketplaces) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no plugin marketplaces")
				return nil
			}
			for _, marketplace := range marketplaces {
				ref := marketplace.Ref
				if ref == "" {
					ref = "HEAD"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-12s %-10s %s\n", marketplace.Name, ref, shortCommit(marketplace.Commit), marketplace.URL)
			}
			return nil
		},
	}
}

func newPluginMarketplaceUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update [name]",
		Short: "Refresh one or all marketplace catalogs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			updated, err := manager.UpdateMarketplaces(cmd.Context(), name)
			if err != nil {
				return err
			}
			for _, marketplace := range updated {
				fmt.Fprintf(cmd.OutOrStdout(), "updated %s to %s\n", marketplace.Name, shortCommit(marketplace.Commit))
			}
			return nil
		},
	}
}

func newPluginMarketplaceRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Remove a marketplace registry entry and checkout",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			if err := manager.RemoveMarketplace(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed marketplace %s\n", args[0])
			return nil
		},
	}
}

func newPluginSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search [query]",
		Short: "Search available plugins",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			query := ""
			if len(args) == 1 {
				query = args[0]
			}
			found, err := manager.Search(query)
			if err != nil {
				return err
			}
			if len(found) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no matching plugins")
				return nil
			}
			for _, available := range found {
				fmt.Fprintf(cmd.OutOrStdout(), "%-32s %-10s %s\n", available.ID, available.Version, available.Description)
			}
			return nil
		},
	}
}

func newPluginAddCmd(boardDir *string) *cobra.Command {
	return &cobra.Command{
		Use:     "add <marketplace/plugin>",
		Aliases: []string{"install"},
		Short:   "Add a plugin to this board's lock and cache it",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			locked, err := manager.AddPlugin(cmd.Context(), *boardDir, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "locked %s %s at %s\n", locked.ID, locked.Version, shortCommit(locked.MarketplaceCommit))
			return nil
		},
	}
}

func newPluginListCmd(boardDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List plugins locked by this board",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lock, err := plugin.LoadBoardLock(*boardDir)
			if err != nil {
				return err
			}
			if len(lock.Plugins) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no plugins locked for this board")
				return nil
			}
			for _, locked := range lock.Plugins {
				fmt.Fprintf(cmd.OutOrStdout(), "%-32s %-10s %s\n", locked.ID, locked.Version, shortCommit(locked.MarketplaceCommit))
			}
			return nil
		},
	}
}

func newPluginRemoveCmd(boardDir *string) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <marketplace/plugin>",
		Aliases: []string{"rm", "uninstall"},
		Short:   "Remove a plugin from this board's lock",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			if err := manager.RemovePlugin(*boardDir, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s from %s\n", args[0], plugin.LockFile)
			return nil
		},
	}
}

func newPluginSyncCmd(boardDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Download and verify every plugin in the board lock",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			plugins, err := manager.Sync(cmd.Context(), *boardDir)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "synchronized %d plugin(s)\n", len(plugins))
			return nil
		},
	}
}

func newPluginUpdateCmd(boardDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "update [marketplace/plugin]",
		Short: "Resolve newer marketplace revisions into the board lock",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			var ids []string
			if len(args) == 1 {
				ids = args
			} else {
				lock, err := plugin.LoadBoardLock(*boardDir)
				if err != nil {
					return err
				}
				for _, locked := range lock.Plugins {
					ids = append(ids, locked.ID)
				}
			}
			for _, id := range ids {
				locked, err := manager.UpdatePlugin(cmd.Context(), *boardDir, id)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "updated %s to %s (%s)\n", id, locked.Version, shortCommit(locked.MarketplaceCommit))
			}
			return nil
		},
	}
}

func newPluginValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [marketplace-directory]",
		Short: "Validate marketplace and plugin manifests",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			manifest, err := plugin.LoadMarketplace(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "valid marketplace %s with %d plugin(s)\n", manifest.Name, len(manifest.Plugins))
			return nil
		},
	}
}

func shortCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 10 {
		return commit[:10]
	}
	return commit
}
