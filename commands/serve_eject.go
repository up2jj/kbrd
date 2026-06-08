package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"kbrd/web"

	"github.com/spf13/cobra"
)

// newServeEjectCmd builds `kbrd serve eject`, which writes the embedded web
// templates and static assets into <dir>/.kbrd_web_templates so they can be
// customized. Existing files are never overwritten.
func newServeEjectCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "eject",
		Short: "Write the default web templates and static assets to .kbrd_web_templates for customizing",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			d := dir
			var err error
			if d == "" {
				if d, err = os.Getwd(); err != nil {
					return fmt.Errorf("cannot determine working directory: %w", err)
				}
			}
			if d, err = filepath.Abs(d); err != nil {
				return err
			}
			written, skipped, err := web.EjectAssets(d)
			if err != nil {
				return err
			}
			for _, p := range written {
				fmt.Printf("wrote  %s\n", p)
			}
			for _, p := range skipped {
				fmt.Printf("kept   %s (already exists)\n", p)
			}
			fmt.Printf("\n%d written, %d kept. Edit files under %s, then restart or save to hot-reload.\n",
				len(written), len(skipped), filepath.Join(d, web.WebDir))
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "board directory (default current directory)")
	return cmd
}
