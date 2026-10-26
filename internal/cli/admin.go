package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/lisk0vian/assess/internal/security"
)

func newAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: T("cli.admin.short"),
		Long:  T("cli.admin.long"),
	}
	set := &cobra.Command{
		Use:   "set-password",
		Short: T("cli.admin.set_short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			if !isTerminal() {
				return errors.New(T("cli.need_terminal"))
			}
			if err := requirePassword(cmd, dir); err != nil {
				return err
			}
			read := func(prompt string) (string, error) {
				fmt.Fprint(cmd.OutOrStdout(), prompt)
				b, err := term.ReadPassword(int(os.Stdin.Fd())) //nolint:gosec // G115: fds fit in int
				fmt.Fprintln(cmd.OutOrStdout())
				return string(b), err
			}
			pw, err := read(T("cli.admin.new_password"))
			if err != nil {
				return err
			}
			again, err := read(T("cli.admin.repeat"))
			if err != nil {
				return err
			}
			if pw != again {
				return errors.New(T("cli.admin.mismatch"))
			}
			if err := security.SetPassword(dir, pw); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), T("cli.admin.set_done", "n", cfg.Int("panel.lock_minutes")))
			return nil
		},
	}
	clearCmd := &cobra.Command{
		Use:   "clear-password",
		Short: T("cli.admin.clear_short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			if err := requirePassword(cmd, dir); err != nil {
				return err
			}
			if err := security.ClearPassword(dir); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), T("cli.admin.cleared"))
			return nil
		},
	}
	cmd.AddCommand(set, clearCmd)
	return cmd
}
