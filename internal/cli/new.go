package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/exam"
)

func newNewCmd() *cobra.Command {
	var opts exam.ScaffoldOptions
	cmd := &cobra.Command{
		Use:     "new <exam-folder>",
		Short:   "Create a new exam folder from a template",
		Long:    "New creates a valid exam folder with one sample question of each type,\nready to edit and check with `ax validate`.",
		Example: "  ax new ./exams/algebra-01 --title \"Álgebra 1\"\n  ax new quiz --lang en",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			fmt.Fprintf(out, "\nNext: edit the files, then run `ax validate %s`\n", shellQuote(dir))
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
