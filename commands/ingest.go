package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kbrd/board"
	"kbrd/config"
	"kbrd/events"
	"kbrd/frontmatter"
	"kbrd/hook"

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
	return cmd
}

func runIngest(cmd *cobra.Command, f ingestFlags, safe bool) error {
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
	cfg, err := config.Load(ref.Path)
	if err != nil {
		return err
	}
	content = withIngestCreatedAt(content, time.Now(), cfg.Ingest.CreatedAtFormat)
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
	if !safe {
		runIngestHooks(cmd, cfg, filepath.Base(columnPath), name)
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "ingested %s in [%s] %s\n", filepath.Base(path), ref.Label(), filepath.Base(columnPath))
	return err
}

// runIngestHooks runs declarative item_created hooks after a successful write.
// It matches TUI hook semantics: errors are reported but do not undo a card or
// stop later hooks.
func runIngestHooks(cmd *cobra.Command, cfg config.Config, column, name string) {
	if !cfg.Hooks.Enabled {
		return
	}
	dispatcher, warnings, err := hook.Load(cfg)
	for _, warning := range warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: hook %s: %s\n", warning.Source, warning.Message)
	}
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: load hooks: %v\n", err)
		return
	}
	if dispatcher == nil {
		return
	}

	for _, result := range dispatcher.Dispatch(cmd.Context(), events.ItemCreated{Item: events.ItemRef{Column: column, Name: name}}) {
		switch {
		case result.Err != nil:
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: hook %q: %v\n", result.Name, result.Err)
		case result.ExitCode != 0:
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: hook %q exited %d\n", result.Name, result.ExitCode)
		}
	}
}

func withIngestCreatedAt(content string, now time.Time, layout string) string {
	return frontmatter.Set(content, "created_at", strconv.Quote(now.UTC().Format(layout)))
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
