// Package cli wires the ax command tree.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/version"
)

// Execute runs the root command and returns the process exit code.
func Execute() int {
	if err := newRootCmd().Execute(); err != nil {
		if !errors.Is(err, errSilent) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
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
	root.PersistentFlags().StringVar(&dataDirFlag, "data-dir", "", "data directory (default: $AX_DATA_DIR or the OS user data folder)")
	// Bare `ax` opens the panel of a running exam, or the menu in a
	// terminal, or shows help.
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		dir, err := dataDir()
		if err != nil {
			return cmd.Help()
		}
		if h, _, _ := daemon.Check(dir); h == daemon.Running {
			return attach(cmd, dir)
		}
		if isTerminal() {
			return home(cmd, dir)
		}
		return cmd.Help()
	}
	root.AddCommand(newVersionCmd(), newNewCmd(), newValidateCmd(), newFormatCmd(),
		newEditCmd(), newStartCmd(), newAttachCmd(), newStatusCmd(), newStopCmd(), newResumeCmd(), newLogsCmd(), newResultsCmd(), newReportCmd(), newAdminCmd(), newDaemonCmd())
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
