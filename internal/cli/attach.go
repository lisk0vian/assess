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

// attach opens the live panel, or prints the status once when stdout is not
// a terminal (scripts, CI).
func attach(cmd *cobra.Command, dir string) error {
	c, st, err := daemon.Dial(dir)
	if err != nil {
		return fmt.Errorf("%w; %s", err, T("cli.attach.start_hint"))
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
		opts.LockAfter = time.Duration(cfg.Int("panel.lock_minutes")) * time.Minute
	}
	mon := tui.NewMonitor(c, st, opts)
	if _, err = tea.NewProgram(mon).Run(); err != nil {
		return err
	}
	if mon.WantsReport() {
		return showReport(cmd, dir, st.SessionID)
	}
	fmt.Fprintln(cmd.OutOrStdout(), T("cli.attach.closed"))
	return nil
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
		return errors.New(T("cli.attach.protected"))
	}
	for range 3 {
		fmt.Fprint(cmd.OutOrStdout(), T("cli.attach.password_prompt"))
		pw, err := term.ReadPassword(int(os.Stdin.Fd())) //nolint:gosec // G115: fds fit in int
		fmt.Fprintln(cmd.OutOrStdout())
		if err != nil {
			return err
		}
		if security.CheckPassword(dir, string(pw)) {
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), T("cli.attach.wrong_password"))
	}
	return errors.New(T("cli.attach.wrong_password_err"))
}
