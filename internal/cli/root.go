// Package cli wires the ax command tree.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/version"
)

// Execute runs the root command and returns the process exit code.
func Execute() int {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ax",
		Short:         "Assess — create, run and proctor exams from the terminal",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("ax {{.Version}}\n")
	root.AddCommand(newVersionCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the ax version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "ax", version.String())
		},
	}
}
