package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/docs"
)

func newFormatCmd() *cobra.Command {
	var reference, guide bool
	cmd := &cobra.Command{
		Use:   "format",
		Short: "Print the exam format: a guide to writing exams as files, and the reference",
		Long: "Format prints how to write an exam as YAML files: a step-by-step guide with a\n" +
			"complete, valid example and a checklist (made for AI assistants, readable by\n" +
			"anyone), followed by the full reference. Teachers can use `ax new` instead.",
		Example: "  ax format > formato.md\n  ax format --guide",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if !reference {
				fmt.Fprint(out, docs.Authoring)
			}
			if !reference && !guide {
				fmt.Fprint(out, "\n---\n\n")
			}
			if !guide {
				fmt.Fprint(out, docs.Format)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&guide, "guide", false, "print only the authoring guide")
	cmd.Flags().BoolVar(&reference, "reference", false, "print only the format reference")
	cmd.MarkFlagsMutuallyExclusive("guide", "reference")
	return cmd
}
