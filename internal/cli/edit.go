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
		Use:   "edit <exam-folder>",
		Short: "Edit an exam in the terminal: settings, questions and answers",
		Long: "Edit opens the exam folder in an interactive editor. Forms change the\n" +
			"settings, add, edit, reorder and delete questions together with their\n" +
			"answers; E opens the file in $VISUAL or $EDITOR (notepad or vi by default)\n" +
			"for anything else. Every change is saved and validated at once, and your\n" +
			"comments and layout in the YAML files are kept. S starts the exam.",
		Example: "  ax edit examples/algebra-01\n  EDITOR=\"code -w\" ax edit ./mi-examen",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isTerminal() {
				return errors.New("the editor needs a terminal; edit the YAML files and run `ax validate`")
			}
			return editExam(cmd, &sf, args[0], duration)
		},
	}
	sf.register(cmd)
	cmd.Flags().DurationVar(&duration, "duration", 0, "exam duration when started with S (default: the manifest's)")
	return cmd
}

// editExam runs the editor on dir, and starts the exam if asked to.
func editExam(cmd *cobra.Command, sf *serveFlags, dir string, duration time.Duration) error {
	d, err := exam.OpenDraft(dir)
	if err != nil {
		return err
	}
	m := tui.NewEditor(d)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return err
	}
	if !m.Start {
		fmt.Fprintf(cmd.OutOrStdout(), "Saved. `ax start %s` runs it.\n", shellQuote(dir))
		return nil
	}
	return startExam(cmd, sf, dir, duration, false)
}
