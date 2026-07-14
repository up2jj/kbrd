package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"kbrd/config"
	"kbrd/reminders"
)

type remindersSyncer interface {
	Sync(context.Context, string, config.RemindersConfig, reminders.Options) (reminders.Report, error)
}

var newRemindersSyncer = func() remindersSyncer { return reminders.NewService() }

func newRemindersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reminders",
		Short: "Synchronize due-bearing cards with Apple Reminders",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newRemindersSyncCmd())
	return cmd
}

func newRemindersSyncCmd() *cobra.Command {
	var opts reminders.Options
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Synchronize this board with its configured Reminders list",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("cannot determine working directory: %w", err)
			}
			cfg, err := config.Load(cwd)
			if err != nil {
				return err
			}
			opts.Progress = remindersProgressWriter(cmd.ErrOrStderr())
			report, err := newRemindersSyncer().Sync(cmd.Context(), cwd, cfg.Reminders, opts)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			for _, operation := range report.Operations {
				if operation.Detail == "" {
					fmt.Fprintf(w, "%s\t%s\n", operation.Kind, operation.Target)
				} else {
					fmt.Fprintf(w, "%s\t%s\t%s\n", operation.Kind, operation.Target, operation.Detail)
				}
			}
			_ = w.Flush()
			fmt.Fprintln(cmd.OutOrStdout(), report.Summary())
			if report.Conflicts > 0 {
				return fmt.Errorf("Reminders sync left %d unresolved conflict(s)", report.Conflicts)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "show the sync plan without writing cards or reminders")
	cmd.Flags().BoolVar(&opts.CreateList, "create-list", false, "create the configured Reminders list when it is missing")
	cmd.Flags().BoolVar(&opts.ImportExisting, "import-existing", false, "import unmarked reminders during the first sync")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "create-list")
	return cmd
}

func remindersProgressWriter(w io.Writer) func(reminders.Progress) {
	lastStage := ""
	return func(progress reminders.Progress) {
		if progress.Stage == lastStage && progress.Total > 0 && progress.Current < progress.Total {
			return
		}
		status := progress.Stage
		if progress.Total > 0 {
			status = fmt.Sprintf("%s %d/%d", status, progress.Current, progress.Total)
		}
		fmt.Fprintf(w, "reminders: %s\n", status)
		lastStage = progress.Stage
	}
}
