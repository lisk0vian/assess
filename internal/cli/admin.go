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
		Short: "Manage the teacher panel password",
		Long: "The password protects `ax attach`, `ax stop` and the panel (which locks itself\n" +
			"after 5 idle minutes or with L). It does not encrypt the database: anyone using\n" +
			"your OS account can read it, so lock your computer too.",
	}
	set := &cobra.Command{
		Use:   "set-password",
		Short: "Set or change the panel password",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			if !isTerminal() {
				return errors.New("run this in a terminal")
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
			pw, err := read("New password: ")
			if err != nil {
				return err
			}
			again, err := read("Repeat it: ")
			if err != nil {
				return err
			}
			if pw != again {
				return errors.New("the passwords do not match")
			}
			if err := security.SetPassword(dir, pw); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Password set. The panel now locks after 5 idle minutes.")
			return nil
		},
	}
	clearCmd := &cobra.Command{
		Use:   "clear-password",
		Short: "Remove the panel password",
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
			fmt.Fprintln(cmd.OutOrStdout(), "Password removed.")
			return nil
		},
	}
	cmd.AddCommand(set, clearCmd)
	return cmd
}
