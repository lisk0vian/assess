//go:build windows

package firewall

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/lisk0vian/assess/internal/netutil"
)

// powershell runs a short read-only PowerShell command without a window.
func powershell(script string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script) //nolint:gosec // G204: fixed scripts
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// ruleKeys hold the firewall rules: the local ones and those of group
// policies. Any user can read them, and they are the same in every
// Windows language.
var ruleKeys = []string{
	`SYSTEM\CurrentControlSet\Services\SharedAccess\Parameters\FirewallPolicy\FirewallRules`,
	`SOFTWARE\Policies\Microsoft\WindowsFirewall\FirewallRules`,
}

// Check reads the network categories and the rules; it needs no rights.
func Check(port int) *Status {
	s := &Status{Supported: true, Admin: IsAdmin(), Port: port}
	out, err := powershell(`Get-NetConnectionProfile | ForEach-Object { $_.InterfaceAlias + '|' + $_.NetworkCategory }`)
	if err != nil {
		s.Err = fmt.Errorf("reading the network profile: %w", err)
		return s
	}
	for _, l := range strings.Split(out, "\n") {
		if name, cat, ok := strings.Cut(strings.TrimSpace(l), "|"); ok && !netutil.IsVirtualName(name) {
			s.Networks = append(s.Networks, Network{Interface: name, Profile: Profile(cat)})
		}
	}
	exe, _ := os.Executable()
	for _, key := range ruleKeys {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, key, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		names, _ := k.ReadValueNames(-1)
		for _, n := range names {
			if v, _, err := k.GetStringValue(n); err == nil {
				s.add(ParseRule(v), exe)
			}
		}
		_ = k.Close()
	}
	return s
}

// IsAdmin reports whether this process runs elevated.
func IsAdmin() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// Apply opens (or, with remove, closes) the port. It must run elevated.
func Apply(o Options, remove bool) error {
	_ = netsh(removeArgs()) // a rule that is not there is fine
	if remove {
		if Check(o.Port).Rule {
			return errors.New("the rule is still there")
		}
		return nil
	}
	return netsh(addArgs(o))
}

func netsh(args []string) error {
	// The command line is written exactly as in the script for IT.
	cmd := exec.Command("netsh.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CmdLine: line(args)}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", line(args), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Elevate runs ax again as administrator with args (Windows asks for the
// password in its own window) and waits for it.
func Elevate(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", "''") + "'"
	}
	script := fmt.Sprintf(`$p = Start-Process -FilePath '%s' -ArgumentList %s -Verb RunAs -Wait -PassThru -WindowStyle Hidden; exit $p.ExitCode`,
		strings.ReplaceAll(exe, "'", "''"), strings.Join(quoted, ","))
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script) //nolint:gosec // G204: re-executes ourselves
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
