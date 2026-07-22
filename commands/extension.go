package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"kbrd/extension"
	"kbrd/model"

	"github.com/spf13/cobra"
)

func newExtensionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extension",
		Short: "Manage the bundled browser extension",
	}
	cmd.AddCommand(newExtensionInstallCmd())
	return cmd
}

func newExtensionInstallCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Extract the Chrome extension for loading as an unpacked extension",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				var err error
				dir, err = extension.DefaultDir()
				if err != nil {
					return err
				}
			}
			abs, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve extension directory: %w", err)
			}
			written, err := extension.Install(abs, model.Version)
			if err != nil {
				return err
			}
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locate kbrd executable: %w", err)
			}
			hostManifest, err := extension.InstallNativeHost(executable)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "installed %d browser extension files in %s\n", len(written), abs)
			fmt.Fprintf(out, "registered native messaging host in %s\n", hostManifest)
			fmt.Fprintln(out, "Chrome setup:")
			fmt.Fprintln(out, "  1. Open chrome://extensions")
			fmt.Fprintln(out, "  2. Enable Developer mode")
			fmt.Fprintf(out, "  3. Click Load unpacked and select %s\n", abs)
			if runtime.GOOS == "darwin" {
				fmt.Fprintln(out, "     In the folder chooser, press Command-Shift-G and paste that path.")
			}
			fmt.Fprintln(out, "  4. Pin kbrd Capture to the toolbar and start capturing")
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "extraction directory (default ~/kbrd-chrome-extension)")
	return cmd
}
