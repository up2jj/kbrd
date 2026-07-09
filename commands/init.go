package commands

import (
	"errors"
	"fmt"
	stdfs "io/fs"
	"os"
	"path/filepath"

	"kbrd/config"
	kbrdfs "kbrd/fs"

	"github.com/spf13/cobra"
)

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
	} else if !errors.Is(err, stdfs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", target, err)
	}
	if err := kbrdfs.WriteNewFileNoClobberDurable(target, config.Template, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	fmt.Printf("wrote %s\n", target)
	return nil
}
