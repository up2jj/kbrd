package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"

	"kbrd/companion"
	"kbrd/notifyroute"

	"github.com/spf13/cobra"
)

func newCompanionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "companion",
		Short: "Manage the macOS menu-bar quick capture app",
	}
	cmd.AddCommand(newCompanionInstallCmd(), newCompanionRunCmd(), newCompanionHotKeyCmd(), newCompanionSnapshotCmd(), newCompanionScratchpadCmd(), newCompanionNotificationActionCmd())
	return cmd
}

func newCompanionHotKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "hotkey",
		Short:  "Print the native companion hot-key configuration",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			hotKey, err := companion.LoadHotKey()
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(hotKey)
		},
	}
}

func newCompanionNotificationActionCmd() *cobra.Command {
	var route, action, boardPath, cardPath, syncKind string
	cmd := &cobra.Command{
		Use:    "notification-action",
		Short:  "Route a Notification Center action to a running board",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return notifyroute.Send(route, notifyroute.Command{
				Action: notifyroute.Action(action), BoardPath: boardPath,
				CardPath: cardPath, SyncKind: syncKind,
			})
		},
	}
	cmd.Flags().StringVar(&route, "route", "", "notification route socket")
	cmd.Flags().StringVar(&action, "action", "", "action identifier")
	cmd.Flags().StringVar(&boardPath, "board", "", "board path")
	cmd.Flags().StringVar(&cardPath, "card", "", "card path")
	cmd.Flags().StringVar(&syncKind, "sync", "", "sync kind")
	_ = cmd.MarkFlagRequired("route")
	_ = cmd.MarkFlagRequired("action")
	_ = cmd.MarkFlagRequired("board")
	return cmd
}

func newCompanionRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Start the installed menu-bar companion",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := companion.Run()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "started kbrd Companion from %s\n", path)
			return nil
		},
	}
}

func newCompanionInstallCmd() *cobra.Command {
	var noLaunch bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the menu-bar companion in ~/Applications",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runtime.GOOS != "darwin" {
				return fmt.Errorf("the menu-bar companion is only available on macOS")
			}
			path, err := companion.Install(!noLaunch)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed kbrd Companion in %s\n", path)
			fmt.Fprintln(cmd.OutOrStdout(), "enabled kbrd Companion at login")
			if !noLaunch {
				label := "Command-Shift-K"
				if hotKey, hotKeyErr := companion.LoadHotKey(); hotKeyErr == nil {
					label = hotKey.Label
				}
				fmt.Fprintf(cmd.OutOrStdout(), "started kbrd Companion · quick capture: %s\n", label)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&noLaunch, "no-launch", false, "install without starting the companion")
	return cmd
}

func newCompanionSnapshotCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "snapshot",
		Short:  "Print companion data as JSON",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			snapshot, err := companion.LoadSnapshot()
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetEscapeHTML(false)
			return encoder.Encode(snapshot)
		},
	}
}

func newCompanionScratchpadCmd() *cobra.Command {
	var board string
	cmd := &cobra.Command{
		Use:    "scratchpad",
		Short:  "Append text to a board scratchpad",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(board) == "" {
				return fmt.Errorf("--board is required")
			}
			if isTerminal(cmd.InOrStdin()) {
				return fmt.Errorf("pipe scratchpad text on stdin")
			}
			text, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("read scratchpad text: %w", err)
			}
			return companion.AppendScratchpad(board, string(text))
		},
	}
	cmd.Flags().StringVar(&board, "board", "", "target board name or path")
	return cmd
}
