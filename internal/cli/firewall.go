package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/firewall"
)

func newFirewallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "firewall",
		Short: T("firewall.short"),
		Long:  T("firewall.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return firewallCheck(cmd.OutOrStdout())
		},
	}
	cmd.AddCommand(newFirewallSetupCmd(false), newFirewallSetupCmd(true), newFirewallScriptCmd(),
		&cobra.Command{
			Use:   "check",
			Short: T("firewall.check_short"),
			Args:  cobra.NoArgs,
			RunE:  func(cmd *cobra.Command, _ []string) error { return firewallCheck(cmd.OutOrStdout()) },
		})
	return cmd
}

func profileName(p firewall.Profile) string {
	switch p {
	case firewall.Public, firewall.Private, firewall.Domain:
		return T("firewall.profile." + string(p))
	}
	return string(p)
}

// firewallCheck explains whether students can reach this computer.
func firewallCheck(w io.Writer) error {
	s := firewall.Check(cfg.Int("server.port"))
	if !s.Supported {
		fmt.Fprintln(w, T("firewall.not_windows"))
		return nil
	}
	if s.Err != nil {
		return s.Err
	}
	for _, n := range s.Networks {
		state := T("firewall.covered")
		switch {
		case s.Blocked(n):
			state = T("firewall.blocked")
		case !s.Covers(n):
			state = T("firewall.not_covered")
		}
		fmt.Fprintf(w, "  %-22s %-12s %s\n", n.Interface, profileName(n.Profile), state)
	}
	if len(s.Networks) == 0 {
		fmt.Fprintln(w, "  "+T("firewall.no_network"))
	}
	port := cfg.Int("server.port")
	fmt.Fprintln(w)
	if s.Ready() {
		if s.Rule {
			fmt.Fprintln(w, T("firewall.ready", "ports", firewall.Ports(port)))
		} else {
			// Windows' own rule for this folder: another copy will ask again.
			fmt.Fprintln(w, T("firewall.app_only"))
		}
		return nil
	}
	if s.Rule {
		fmt.Fprintln(w, T("firewall.rule_partial", "ports", firewall.Ports(port)))
	} else {
		fmt.Fprintln(w, T("firewall.no_rule", "ports", firewall.Ports(port)))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, T("firewall.options"))
	return nil
}

// firewallHint starts checking whether Windows will keep students out;
// the returned function waits for the answer and warns.
func firewallHint(w io.Writer) func() {
	if runtime.GOOS != "windows" {
		return func() {}
	}
	done := make(chan *firewall.Status, 1)
	go func() { done <- firewall.Check(cfg.Int("server.port")) }()
	return func() {
		if s := <-done; s.Supported && s.Err == nil && !s.Ready() {
			fmt.Fprintln(w, T("firewall.start_hint"))
		}
	}
}

func newFirewallSetupCmd(remove bool) *cobra.Command {
	var public, elevated bool
	use, short := "setup", T("firewall.setup_short")
	if remove {
		use, short = "remove", T("firewall.remove_short")
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  T("firewall.setup_long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o := firewall.Options{Port: cfg.Int("server.port"), Public: public}
			if elevated || firewall.IsAdmin() {
				if err := firewall.Apply(o, remove); err != nil {
					return err
				}
				if elevated {
					return nil // the window closes; the parent reports
				}
			} else {
				if runtime.GOOS != "windows" {
					return errors.New(T("firewall.not_windows"))
				}
				fmt.Fprintln(cmd.OutOrStdout(), T("firewall.elevating"))
				args := []string{"firewall", use, "--elevated", "--config", cfg.Path}
				if public {
					args = append(args, "--public")
				}
				if err := firewall.Elevate(args); err != nil {
					return errors.New(T("firewall.elevate_failed", "err", err))
				}
			}
			s := firewall.Check(cfg.Int("server.port"))
			switch {
			case remove && !s.Rule:
				fmt.Fprintln(cmd.OutOrStdout(), T("firewall.removed"))
			case !remove && s.Rule:
				fmt.Fprintln(cmd.OutOrStdout(), T("firewall.done", "ports", firewall.Ports(o.Port)))
				if !s.Ready() {
					fmt.Fprintln(cmd.OutOrStdout(), T("firewall.public_hint"))
				}
			default:
				return errors.New(T("firewall.not_applied"))
			}
			return nil
		},
	}
	if !remove {
		cmd.Flags().BoolVar(&public, "public", false, T("firewall.flag_public"))
	}
	cmd.Flags().BoolVar(&elevated, "elevated", false, "already running as administrator")
	_ = cmd.Flags().MarkHidden("elevated")
	return cmd
}

func newFirewallScriptCmd() *cobra.Command {
	var public bool
	cmd := &cobra.Command{
		Use:   "script [folder]",
		Short: T("firewall.script_short"),
		Long:  T("firewall.script_long"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			o := firewall.Options{Port: cfg.Int("server.port"), Public: public}
			readme := T("firewall.readme", "ports", firewall.Ports(o.Port), "port", strconv.Itoa(o.Port))
			script := filepath.Join(dir, "assess-firewall.cmd")
			if err := os.WriteFile(script, []byte(firewall.Script(o, readme)), 0o644); err != nil { //nolint:gosec // G306: a script for IT to read
				return err
			}
			note := filepath.Join(dir, T("firewall.readme_file"))
			body := readme + "\n\n" + firewall.Commands(o)[1] + "\n\n" + T("firewall.readme_undo") + "\n" + firewall.RemoveCommand() + "\n"
			if err := os.WriteFile(note, []byte(body), 0o644); err != nil { //nolint:gosec // G306: a note for IT
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), T("firewall.script_done", "script", script, "note", note))
			return nil
		},
	}
	cmd.Flags().BoolVar(&public, "public", false, T("firewall.flag_public"))
	return cmd
}
