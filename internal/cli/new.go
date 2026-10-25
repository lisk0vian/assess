package cli

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/tui"
)

func newNewCmd() *cobra.Command {
	var opts exam.ScaffoldOptions
	cmd := &cobra.Command{
		Use:   "new [exam-folder]",
		Short: "Create a new exam: a guided wizard, or a folder from a template",
		Long: "Without a folder, new opens a wizard in the terminal that asks for the\n" +
			"details, the questions (with a live preview and the answer marked), how it\n" +
			"is proctored, and checks everything before writing the folder.\n\n" +
			"With a folder, it writes a valid exam with one sample question of each type,\n" +
			"ready to edit by hand, with `ax edit`, or by an AI (see `ax format`).",
		Example: "  ax new                                   # wizard\n  ax new ./exams/algebra-01 --title \"Álgebra 1\"\n  ax new quiz --lang en",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				if !isTerminal() {
					return errors.New("the wizard needs a terminal; pass a folder to create one from the template")
				}
				return newExamWizard(cmd)
			}
			dir := args[0]
			if err := exam.Scaffold(dir, opts); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Created %s\n\n", dir)
			fmt.Fprintln(out, "  manifest.yaml            exam settings and question order")
			fmt.Fprintln(out, "  questions/q00N.yaml      one file per question")
			fmt.Fprintln(out, "  private/answer-key.yaml  correct answers and points (never sent to students)")
			fmt.Fprintln(out, "  assets/                  images used in questions")
			fmt.Fprintf(out, "\nNext: `ax edit %s` to write your questions (or edit the files and run `ax validate`)\n", shellQuote(dir))
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.ID, "id", "", "exam id (default: derived from the folder name)")
	cmd.Flags().StringVar(&opts.Title, "title", "", "exam title")
	cmd.Flags().StringVar(&opts.Language, "lang", "es", "student UI language: es or en")
	return cmd
}

// shellQuote quotes a path for display when it contains spaces.
func shellQuote(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}
	return s
}

// newExamWizard runs the new-exam wizard and what the teacher picks after.
func newExamWizard(cmd *cobra.Command) error {
	w := tui.NewWizard()
	if _, err := tea.NewProgram(w).Run(); err != nil {
		return err
	}
	if w.Dir == "" {
		return nil
	}
	switch w.Next {
	case tui.NextStart:
		return startExam(cmd, defaultServeFlags(), w.Dir, 0, false)
	case tui.NextEdit:
		return editExam(cmd, defaultServeFlags(), w.Dir, 0)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Created %s. `ax start %s` runs it; `ax edit %s` changes it.\n", w.Dir, shellQuote(w.Dir), shellQuote(w.Dir))
	return nil
}
