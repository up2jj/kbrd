package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"kbrd/ingest"

	"github.com/spf13/cobra"
)

type ingestFlags struct {
	board   string
	column  string
	name    string
	content string
	file    string
	source  string
}

var ingestSourcePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// newIngestCmd builds `kbrd ingest`, a headless way for scripts to create a
// card from text without starting the TUI. Declarative item_created hooks run
// after the card is written unless the root --safe flag is set.
func newIngestCmd(flags *cliFlags) *cobra.Command {
	var f ingestFlags
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest text as a card in a board",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIngest(cmd, f, flags.safe)
		},
	}
	cmd.Flags().StringVar(&f.board, "board", "", "target board name from recents or an existing directory path")
	cmd.Flags().StringVar(&f.column, "column", "", "target column name or 1-based column number (default first column)")
	cmd.Flags().StringVar(&f.name, "name", "", "external card name; normalized to a safe filename")
	cmd.Flags().StringVar(&f.content, "content", "", "card content (mutually exclusive with --file)")
	cmd.Flags().StringVar(&f.file, "file", "", "read card content from this file; use - for stdin")
	cmd.Flags().StringVar(&f.source, "source", "", "record a machine-readable source identifier in frontmatter")
	return cmd
}

func runIngest(cmd *cobra.Command, f ingestFlags, safe bool) error {
	if strings.TrimSpace(f.board) == "" {
		return fmt.Errorf("--board is required")
	}
	if strings.TrimSpace(f.name) == "" {
		return fmt.Errorf("--name is required")
	}
	if f.source != "" && !ingestSourcePattern.MatchString(f.source) {
		return fmt.Errorf("--source must contain only letters, digits, dots, underscores, or hyphens")
	}

	content, err := ingestContent(cmd, f)
	if err != nil {
		return err
	}
	result, err := ingest.Create(cmd.Context(), ingest.Request{
		Board: f.board, Column: f.column, Name: f.name,
		Content: content, Source: f.source, Safe: safe,
	})
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		if warning.Source != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: hook %s: %s\n", warning.Source, warning.Message)
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning.Message)
		}
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "ingested %s in [%s] %s\n", filepath.Base(result.Path), result.Board, result.Column)
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
