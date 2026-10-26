package cli

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/i18n"
	"github.com/lisk0vian/assess/internal/tui"
)

func newNewCmd() *cobra.Command {
	var opts exam.ScaffoldOptions
	cmd := &cobra.Command{
		Use:     "new [exam-folder]",
		Short:   T("cli.new.short"),
		Long:    T("cli.new.long"),
		Example: T("cli.new.example"),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				if !isTerminal() {
					return errors.New(T("cli.new.no_terminal"))
				}
				return newExamWizard(cmd)
			}
			if !i18n.ValidLang(opts.Language) {
				return errors.New(T("cli.new.bad_lang", "lang", opts.Language))
			}
			dir := args[0]
			if err := exam.Scaffold(dir, opts); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, T("cli.new.created", "dir", dir))
			fmt.Fprintln(out, T("cli.new.files"))
			fmt.Fprintln(out, T("cli.new.next", "dir", shellQuote(dir)))
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.ID, "id", "", T("cli.new.flag_id"))
	cmd.Flags().StringVar(&opts.Title, "title", "", T("cli.new.flag_title"))
	cmd.Flags().StringVar(&opts.Language, "lang", cfg.StudentLanguage(), T("cli.new.flag_lang"))
	return cmd
}

// shellQuote quotes a path for display when it contains spaces.
func shellQuote(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}
	return s
}

// newExamDefaults are the wizard's starting values, from the settings.
func newExamDefaults() tui.WizardDefaults {
	return tui.WizardDefaults{
		Language:         cfg.StudentLanguage(),
		Duration:         cfg.Dur("new_exam.duration"),
		Visibility:       cfg.Str("new_exam.result_visibility"),
		Profile:          cfg.Str("new_exam.proctoring"),
		ShuffleQuestions: cfg.Bool("new_exam.shuffle_questions"),
		ShuffleOptions:   cfg.Bool("new_exam.shuffle_options"),
		Instructions:     cfg.Str("new_exam.instructions"),
	}
}

// newExamWizard runs the new-exam wizard and what the teacher picks after.
func newExamWizard(cmd *cobra.Command) error {
	w := tui.NewWizard(newExamDefaults())
	if _, err := tea.NewProgram(w).Run(); err != nil {
		return err
	}
	if w.Dir == "" {
		return nil
	}
	if dir, err := dataDir(); err == nil {
		if v, _, err := activeVault(dir); err == nil {
			if _, err := storeExam(v, w.Dir, false); err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), T("vault.new.saved", "name", v.Meta.Name))
			}
		}
	}
	switch w.Next {
	case tui.NextStart:
		return startExam(cmd, defaultServeFlags(), w.Dir, 0, false)
	case tui.NextEdit:
		return editExam(cmd, defaultServeFlags(), w.Dir, 0)
	}
	fmt.Fprintln(cmd.OutOrStdout(), T("cli.new.wizard_done", "dir", w.Dir, "q", shellQuote(w.Dir)))
	return nil
}
