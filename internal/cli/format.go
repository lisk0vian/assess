package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/docs"
)

func newFormatCmd() *cobra.Command {
	var reference, guide bool
	cmd := &cobra.Command{
		Use:     "format",
		Short:   T("cli.format.short"),
		Long:    T("cli.format.long"),
		Example: "  ax exam guide > format.md\n  ax exam guide --guide",
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
	cmd.Flags().BoolVar(&guide, "guide", false, T("cli.format.flag_guide"))
	cmd.Flags().BoolVar(&reference, "reference", false, T("cli.format.flag_reference"))
	cmd.MarkFlagsMutuallyExclusive("guide", "reference")
	return cmd
}
