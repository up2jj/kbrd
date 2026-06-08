package commands

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	fsutil "kbrd/fs"
	"kbrd/recents"

	"github.com/spf13/cobra"
)

// newCloneCmd builds `kbrd clone <repo-url> [dir]`, which git-clones a board
// repo and, unless --no-open is set, opens it in the TUI.
func newCloneCmd(flags *cliFlags) *cobra.Command {
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "clone <repo-url> [dir]",
		Short: "Clone a board repository and open it",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ""
			if len(args) == 2 {
				dir = args[1]
			}
			abs, err := runClone(args[0], dir)
			if err != nil {
				return err
			}
			if noOpen {
				return nil
			}
			return runBoard(abs, *flags)
		},
	}
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "clone only; do not open the board")
	return cmd
}

// runClone clones url into dir (derived from the URL when empty), registers the
// result in recents, and returns the absolute path to the cloned board.
func runClone(url, dir string) (string, error) {
	if dir == "" { // derive from URL: foo/bar.git -> bar
		dir = strings.TrimSuffix(filepath.Base(url), ".git")
	}
	if dir == "" || dir == "." || dir == "/" {
		return "", fmt.Errorf("cannot determine target directory from %q; pass an explicit dir", url)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err == nil {
		return "", fmt.Errorf("target directory already exists: %s", abs)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("cannot access target: %w", err)
	}
	if err := fsutil.GitClone(url, abs); err != nil {
		return "", err
	}
	store, _ := recents.Load()
	store.Touch(abs, "") // basename label, matching runBoard
	_ = store.Save()
	fmt.Printf("cloned %s into %s\n", url, abs)
	return abs, nil
}
