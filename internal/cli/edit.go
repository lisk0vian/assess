package cli

import (
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/tui"
)

func newEditCmd() *cobra.Command {
	var sf serveFlags
	var duration time.Duration
	cmd := &cobra.Command{
		Use:     "edit <exam-folder>",
		Short:   T("cli.edit.short"),
		Long:    T("cli.edit.long"),
		Example: T("cli.edit.example"),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isTerminal() {
				return errors.New(T("cli.edit.no_terminal"))
			}
			return editExam(cmd, &sf, args[0], duration)
		},
	}
	sf.register(cmd)
	cmd.Flags().DurationVar(&duration, "duration", 0, T("cli.edit.flag_duration"))
	return cmd
}

// editExam runs the editor on dir, and starts the exam if asked to.
func editExam(cmd *cobra.Command, sf *serveFlags, dir string, duration time.Duration) error {
	d, err := exam.OpenDraft(dir)
	if err != nil {
		return err
	}
	m := tui.NewEditor(d, cfg.Str("editor"))
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return err
	}
	if !m.Start {
		fmt.Fprintln(cmd.OutOrStdout(), T("cli.edit.saved", "dir", shellQuote(dir)))
		return nil
	}
	return startExam(cmd, sf, dir, duration, false)
}
