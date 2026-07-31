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
		Short: "Manage board-local Lua and static-content plugins",
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
		newPluginDisableCmd(&boardDir),
		newPluginEnableCmd(&boardDir),
		newPluginRemoveCmd(&boardDir),
		newPluginSyncCmd(&boardDir),
		newPluginOutdatedCmd(&boardDir),
		newPluginUpdateCmd(&boardDir),
		newPluginRollbackCmd(&boardDir),
		newPluginDiffCmd(&boardDir),
		newPluginStarterCmd(&boardDir),
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
	if info.Installed != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", pluginStatus(*info.Installed))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Available version: %s\n", valueOr(manifest.Version, "unspecified"))
	fmt.Fprintf(cmd.OutOrStdout(), "Marketplace URL: %s\n", info.Marketplace.URL)
	fmt.Fprintf(cmd.OutOrStdout(), "Marketplace commit: %s\n", info.Marketplace.Commit)
	fmt.Fprintf(cmd.OutOrStdout(), "Commands: %s\n", formatDeclarations(manifest.Commands))
	fmt.Fprintf(cmd.OutOrStdout(), "Hooks: %s\n", formatDeclarations(manifest.Hooks))
	fmt.Fprintf(cmd.OutOrStdout(), "Layers: %s\n", formatDeclarations(manifest.Layers))
	fmt.Fprintf(cmd.OutOrStdout(), "Timers: %s\n", formatDeclarations(manifest.Timers))
	fmt.Fprintf(cmd.OutOrStdout(), "Network access: %t\n", manifest.NetworkAccess)
	fmt.Fprintf(cmd.OutOrStdout(), "Shell access: %t\n", manifest.ShellAccess)
	fmt.Fprintf(cmd.OutOrStdout(), "Card templates: %s\n", valueOr(manifest.Assets.CardTemplates, "not declared"))
	fmt.Fprintf(cmd.OutOrStdout(), "Themes: %s\n", valueOr(manifest.Assets.Themes, "not declared"))
	fmt.Fprintf(cmd.OutOrStdout(), "Frontmatter presets: %s\n", valueOr(manifest.Assets.FrontmatterPresets, "not declared"))
	fmt.Fprintf(cmd.OutOrStdout(), "Custom commands: %s\n", valueOr(manifest.Assets.CustomCommands, "not declared"))
	fmt.Fprintf(cmd.OutOrStdout(), "Board starters: %s\n", valueOr(manifest.Assets.BoardStarters, "not declared"))
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
		Use:     "add <marketplace/plugin[@version]>",
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
				fmt.Fprintf(cmd.OutOrStdout(), "%-32s %-10s %s %s\n", locked.ID, locked.Version, shortCommit(locked.MarketplaceCommit), pluginStatus(locked))
			}
			return nil
		},
	}
}

func pluginStatus(locked plugin.LockedPlugin) string {
	if locked.Disabled {
		return "disabled"
	}
	return "enabled"
}

func newPluginDisableCmd(boardDir *string) *cobra.Command {
	return newPluginSetEnabledCmd(boardDir, false)
}

func newPluginEnableCmd(boardDir *string) *cobra.Command {
	return newPluginSetEnabledCmd(boardDir, true)
}

func newPluginSetEnabledCmd(boardDir *string, enabled bool) *cobra.Command {
	action := "disable"
	state := "disabled"
	short := "Disable a locked plugin without changing its pinned revision"
	if enabled {
		action = "enable"
		state = "enabled"
		short = "Enable a locked plugin without changing its pinned revision"
	}
	return &cobra.Command{
		Use:   action + " <marketplace/plugin>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			if err := manager.SetPluginEnabled(*boardDir, args[0], enabled); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s in %s\n", state, args[0], plugin.LockFile)
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
		Short: "Download and verify every plugin and static asset in the board lock",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			if _, err := manager.Sync(cmd.Context(), *boardDir); err != nil {
				return err
			}
			lock, err := plugin.LoadBoardLock(*boardDir)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "synchronized %d plugin(s)\n", len(lock.Plugins))
			return nil
		},
	}
}

func newPluginUpdateCmd(boardDir *string) *cobra.Command {
	var dryRun bool
	var channel string
	cmd := &cobra.Command{
		Use:   "update [marketplace/plugin]",
		Short: "Resolve newer marketplace revisions into the board lock",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			ids, err := pluginUpdateIDs(*boardDir, args)
			if err != nil {
				return err
			}
			var previews []plugin.UpdatePreview
			if dryRun {
				previews, err = manager.PreviewUpdates(cmd.Context(), *boardDir, ids, plugin.UpdateOptions{Channel: channel})
				if err != nil {
					return err
				}
			}
			if dryRun {
				for i := range ids {
					printUpdatePreview(cmd, previews[i], false)
				}
				return nil
			}
			before, err := plugin.LoadBoardLock(*boardDir)
			if err != nil {
				return err
			}
			currentByID := make(map[string]plugin.LockedPlugin, len(before.Plugins))
			for _, locked := range before.Plugins {
				currentByID[locked.ID] = locked
			}
			updated, err := manager.UpdatePlugins(cmd.Context(), *boardDir, ids, plugin.UpdateOptions{Channel: channel})
			if err != nil {
				return err
			}
			for i, locked := range updated {
				id := ids[i]
				current := currentByID[id]
				if current == locked {
					if current.RequestedVersion != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "%s is pinned at %s; use --channel or add %s@<version> to change it\n", id, current.RequestedVersion, id)
					} else {
						fmt.Fprintf(cmd.OutOrStdout(), "%s is already up to date at %s (%s)\n", id, valueOr(locked.Version, "unspecified"), shortCommit(locked.MarketplaceCommit))
					}
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "updated %s to %s (%s)\n", id, locked.Version, shortCommit(locked.MarketplaceCommit))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show available changes without updating the lock or activating content")
	cmd.Flags().StringVar(&channel, "channel", "", "select a published version channel (for example stable or beta)")
	return cmd
}

func newPluginRollbackCmd(boardDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "rollback <marketplace/plugin>",
		Short: "Restore the plugin's previous exact lock entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			locked, err := manager.RollbackPlugin(cmd.Context(), *boardDir, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "rolled back %s to %s (%s)\n", locked.ID, locked.Version, shortCommit(locked.MarketplaceCommit))
			return nil
		},
	}
}

func newPluginOutdatedCmd(boardDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "outdated",
		Short: "Check locked plugins for available updates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			ids, err := pluginUpdateIDs(*boardDir, nil)
			if err != nil {
				return err
			}
			previews, err := manager.PreviewUpdates(cmd.Context(), *boardDir, ids)
			if err != nil {
				return err
			}
			outdated := 0
			for _, preview := range previews {
				if !preview.Outdated() {
					continue
				}
				outdated++
				printUpdatePreview(cmd, preview, false)
			}
			if outdated == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "all plugins are up to date")
			}
			return nil
		},
	}
}

func newPluginDiffCmd(boardDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "diff <marketplace/plugin>",
		Short: "Show the available plugin update and its content diff",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := pluginManager()
			if err != nil {
				return err
			}
			preview, err := manager.PreviewUpdate(cmd.Context(), *boardDir, args[0])
			if err != nil {
				return err
			}
			printUpdatePreview(cmd, preview, true)
			return nil
		},
	}
}

func pluginUpdateIDs(boardDir string, args []string) ([]string, error) {
	if len(args) == 1 {
		return args, nil
	}
	lock, err := plugin.LoadBoardLock(boardDir)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(lock.Plugins))
	for _, locked := range lock.Plugins {
		ids = append(ids, locked.ID)
	}
	return ids, nil
}

func printUpdatePreview(cmd *cobra.Command, preview plugin.UpdatePreview, includePatch bool) {
	currentVersion := valueOr(preview.Current.Version, "unspecified")
	candidateVersion := valueOr(preview.Candidate.Version, "unspecified")
	state := "update available"
	if !preview.Outdated() {
		state = "up to date"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s (%s)\n", preview.ID, currentVersion, candidateVersion, state)
	if preview.Current.MarketplaceCommit != preview.Candidate.MarketplaceCommit {
		fmt.Fprintf(cmd.OutOrStdout(), "  marketplace: %s -> %s\n", shortCommit(preview.Current.MarketplaceCommit), shortCommit(preview.Candidate.MarketplaceCommit))
	}
	if len(preview.ManifestChanges) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "  manifest:")
		for _, change := range preview.ManifestChanges {
			fmt.Fprintf(cmd.OutOrStdout(), "    %s: %s -> %s\n", change.Field, change.Before, change.After)
		}
	}
	if len(preview.Files) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "  files:")
		for _, file := range preview.Files {
			fmt.Fprintf(cmd.OutOrStdout(), "    %s %s\n", fileStatus(file.Status), file.Path)
		}
	}
	if includePatch && preview.Patch != "" {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), preview.Patch)
	}
}

func fileStatus(status string) string {
	switch status {
	case "added":
		return "A"
	case "removed":
		return "D"
	default:
		return "M"
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
