package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/security"
	"github.com/lisk0vian/assess/internal/tui"
)

// lockAfter locks an idle panel when a password is set.
const lockAfter = 5 * time.Minute

// attach opens the live panel, or prints the status once when stdout is not
// a terminal (scripts, CI).
func attach(cmd *cobra.Command, dir string) error {
	c, st, err := daemon.Dial(dir)
	if err != nil {
		return fmt.Errorf("%w; start one with `ax start <exam-folder>` or `ax resume`", err)
	}
	if !isTerminal() {
		sv, err := c.Status(cmd.Context())
		if err != nil {
			return err
		}
		printStatus(cmd.OutOrStdout(), sv)
		return nil
	}
	if err := requirePassword(cmd, dir); err != nil {
		return err
	}
	opts := tui.Options{}
	if security.HasPassword(dir) {
		opts.CheckPassword = func(pw string) bool { return security.CheckPassword(dir, pw) }
		opts.LockAfter = lockAfter
	}
	_, err = tea.NewProgram(tui.NewMonitor(c, st, opts)).Run()
	if err == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "Panel closed. The exam keeps running; `ax attach` reopens it.")
	}
	return err
}

func isTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd())) //nolint:gosec // G115: fds fit in int
}

// requirePassword asks for the panel password when one is set.
func requirePassword(cmd *cobra.Command, dir string) error {
	if !security.HasPassword(dir) {
		return nil
	}
	if !isTerminal() {
		return errors.New("this panel is password protected; run the command in a terminal")
	}
	for range 3 {
		fmt.Fprint(cmd.OutOrStdout(), "Panel password: ")
		pw, err := term.ReadPassword(int(os.Stdin.Fd())) //nolint:gosec // G115: fds fit in int
		fmt.Fprintln(cmd.OutOrStdout())
		if err != nil {
			return err
		}
		if security.CheckPassword(dir, string(pw)) {
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Wrong password.")
	}
	return errors.New("wrong password")
}
