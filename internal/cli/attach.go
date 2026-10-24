package cli

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/tui"
)

// attach opens the live panel, or prints the status once when stdout is not
// a terminal (scripts, CI).
func attach(cmd *cobra.Command, dir string) error {
	c, st, err := daemon.Dial(dir)
	if err != nil {
		return fmt.Errorf("%w; start one with `ax start <exam-folder>` or `ax resume`", err)
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) { //nolint:gosec // G115: file descriptors fit in int
		sv, err := c.Status(cmd.Context())
		if err != nil {
			return err
		}
		printStatus(cmd.OutOrStdout(), sv)
		return nil
	}
	_, err = tea.NewProgram(tui.NewMonitor(c, st)).Run()
	if err == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "Panel closed. The exam keeps running; `ax attach` reopens it.")
	}
	return err
}
