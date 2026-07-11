package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"kbrd/board"

	"github.com/spf13/cobra"
)

type ingestFlags struct {
	board   string
	column  string
	name    string
	content string
	file    string
}

// newIngestCmd builds `kbrd ingest`, a headless way for scripts to create a
// card from text without starting the TUI or evaluating board-supplied code.
func newIngestCmd() *cobra.Command {
	var f ingestFlags
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest text as a card in a board",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIngest(cmd, f)
		},
	}
	cmd.Flags().StringVar(&f.board, "board", "", "target board name from recents or an existing directory path")
	cmd.Flags().StringVar(&f.column, "column", "", "target column name or 1-based column number (default first column)")
	cmd.Flags().StringVar(&f.name, "name", "", "external card name; normalized to a safe filename")
	cmd.Flags().StringVar(&f.content, "content", "", "card content (mutually exclusive with --file)")
	cmd.Flags().StringVar(&f.file, "file", "", "read card content from this file; use - for stdin")
	return cmd
}

func runIngest(cmd *cobra.Command, f ingestFlags) error {
	if strings.TrimSpace(f.board) == "" {
		return fmt.Errorf("--board is required")
	}
	if strings.TrimSpace(f.name) == "" {
		return fmt.Errorf("--name is required")
	}

	content, err := ingestContent(cmd, f)
	if err != nil {
		return err
	}
	ref, err := board.ResolveExisting(f.board)
	if err != nil {
		return err
	}
	columnPath, err := resolveIngestColumn(ref.Path, f.column)
	if err != nil {
		return err
	}
	name, err := board.SanitizeGeneratedName(f.name)
	if err != nil {
		return fmt.Errorf("sanitize card name: %w", err)
	}
	path, err := board.CreateItem(columnPath, name, content)
	if err != nil {
		return fmt.Errorf("create card in %s: %w", filepath.Base(columnPath), err)
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "ingested %s in [%s] %s\n", filepath.Base(path), ref.Label(), filepath.Base(columnPath))
	return err
}

func ingestContent(cmd *cobra.Command, f ingestFlags) (string, error) {
	contentSet := cmd.Flags().Changed("content")
	fileSet := cmd.Flags().Changed("file")
	if contentSet && fileSet {
		return "", fmt.Errorf("--content and --file cannot be used together")
	}
	if contentSet {
		return f.content, nil
	}
	if fileSet && f.file != "-" {
		if strings.TrimSpace(f.file) == "" {
			return "", fmt.Errorf("--file cannot be empty")
		}
		data, err := os.ReadFile(f.file)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", f.file, err)
		}
		return string(data), nil
	}

	in := cmd.InOrStdin()
	if isTerminal(in) {
		return "", fmt.Errorf("provide --content, --file, or pipe content on stdin")
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return string(data), nil
}

func isTerminal(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func resolveIngestColumn(boardPath, selector string) (string, error) {
	columns, err := board.Columns(boardPath)
	if err != nil {
		return "", err
	}
	selector = strings.TrimSpace(selector)
	if selector == "" {
		if len(columns) == 0 {
			return "", fmt.Errorf("%w: %s", board.ErrNoColumns, boardPath)
		}
		return filepath.Join(boardPath, columns[0]), nil
	}

	if index, err := strconv.Atoi(selector); err == nil {
		if index < 1 || index > len(columns) {
			return "", fmt.Errorf("column number %d is out of range; board has %d column(s)", index, len(columns))
		}
		return filepath.Join(boardPath, columns[index-1]), nil
	}
	return board.ResolveColumn(boardPath, selector, false)
}
