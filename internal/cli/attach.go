package cli

import (
	"context"
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
	opts := panelOptions(dir)
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

// isTerminal reports whether ax talks to a person; tests replace it.
var isTerminal = func() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd())) //nolint:gosec // G115: fds fit in int
}

// panelOptions protects the panel: with the vault's password when the
// vault is encrypted (locking the panel locks the vault too), or with the
// panel password of `ax config password`.
func panelOptions(dir string) tui.Options {
	opts := tui.Options{}
	lockAfter := time.Duration(cfg.Int("panel.lock_minutes")) * time.Minute
	if v, _, err := activeVault(dir); err == nil && v.Encrypted() {
		opts.CheckPassword = func(pw string) bool {
			if !v.CheckPassword(pw) {
				return false
			}
			_ = v.Unlock(pw)
			unlocked[v.Meta.ID] = v.Identity()
			startAgent(dir, v)
			return true
		}
		opts.OnLock = func() { lockVaults(context.Background(), dir) }
		opts.LockAfter = lockAfter
		return opts
	}
	if security.HasPassword(dir) {
		opts.CheckPassword = func(pw string) bool { return security.CheckPassword(dir, pw) }
		opts.LockAfter = lockAfter
	}
	return opts
}

// protected reports whether opening the panel or a report needs a password.
func protected(dir string) bool {
	if v, _, err := activeVault(dir); err == nil && v.Encrypted() {
		return true
	}
	return security.HasPassword(dir)
}

// requirePassword asks for the password that protects the panel: the
// vault's (once per unlock) or the panel password.
func requirePassword(cmd *cobra.Command, dir string) error {
	if v, _, err := activeVault(dir); err == nil && v.Encrypted() {
		return unlock(cmd.Context(), dir, v)
	}
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
