package cli

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/config"
	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/datadir"
	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/firewall"
	"github.com/lisk0vian/assess/internal/netutil"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/tui"
	"github.com/lisk0vian/assess/internal/vault"
)

// firstVault is where the first vault goes when there is none yet; ok is
// false when a vault exists, another was chosen, or an older ax left data
// to move (activeVault does that, as before).
func firstVault(dir string) (path string, ok bool) {
	cwd, _ := os.Getwd()
	p, src, err := vault.Locate(vault.Choice{Flag: vaultFlag, Setting: cfg.Str("vault.path"), Cwd: cwd, Base: cfg.Dir(),
		DataDir: dir, Registry: loadRegistry(dir)})
	if err != nil || src != vault.FromDefault || vault.IsVault(p) {
		return "", false
	}
	if _, err := os.Stat(datadir.DB(dir)); err == nil {
		return "", false
	}
	return p, true
}

// firstRun welcomes a teacher with no vault and creates it, encrypted unless
// they say otherwise. It reports false when they left before.
func firstRun(dir string) (bool, error) {
	p, ok := firstVault(dir)
	if !ok {
		return true, nil
	}
	m := tui.NewFirstRun(vault.MinPassword)
	m.SetLanguage = func(lang string) error {
		if err := ensureConfigFile(); err != nil {
			return err
		}
		if err := config.Set(cfg.Path, "language", lang); err != nil {
			return err
		}
		return reloadConfig()
	}
	m.Create = func(name, password string) (string, error) {
		v, key, err := vault.Init(p, name, password)
		if err != nil {
			return "", err
		}
		reg := loadRegistry(dir)
		reg.Add(name, v.Dir)
		_ = reg.Save()
		if v.Encrypted() {
			unlocked[v.Meta.ID] = v.Identity()
			startAgent(dir, v)
		}
		return key, nil
	}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return false, err
	}
	return m.Done, nil
}

// firewallState is the firewall check in the words of the Prepare screen.
func firewallState() tui.FirewallState {
	port := cfg.Int("server.port")
	s := firewall.Check(port)
	if !s.Supported || s.Err != nil {
		return tui.FirewallState{}
	}
	if s.Ready() {
		return tui.FirewallState{Supported: true, Ready: true, Text: T("prepare.fw_ok", "ports", firewall.Ports(port))}
	}
	return tui.FirewallState{Supported: true, Text: T("prepare.fw_closed", "ports", firewall.Ports(port))}
}

// openPort opens the server port in the firewall, asking Windows for
// permission when this process may not.
func openPort() tui.FirewallState {
	o := firewall.Options{Port: cfg.Int("server.port")}
	if firewall.IsAdmin() {
		_ = firewall.Apply(o, false)
	} else {
		_ = firewall.Elevate([]string{"firewall", "setup", "--elevated", "--config", cfg.Path})
	}
	return firewallState()
}

// prepareAndStart shows Preparar sesión for an exam and opens its room.
func prepareAndStart(cmd *cobra.Command, dir, ref string, sf *serveFlags) error {
	if h, _, _ := daemon.Check(dir); h == daemon.Running {
		return errors.New(T("cli.resume.already_running"))
	}
	examDir, cleanup, err := examFolder(cmd, dir, ref)
	if err != nil {
		return err
	}
	defer cleanup()
	d := &tui.PrepareData{Firewall: firewallState(), OpenPort: openPort}
	if ex, _, err := exam.Load(examDir); err == nil {
		d.ExamTitle, d.Duration = ex.Manifest.Title, ex.Manifest.Duration.D()
	}
	if _, probs, err := exam.Publish(examDir); err != nil {
		for _, p := range probs {
			if p.Severity == exam.SeverityError {
				d.Problems = append(d.Problems, p.String())
			}
		}
		if len(d.Problems) == 0 {
			d.Problems = []string{err.Error()}
		}
	}
	_ = withService(cmd.Context(), dir, func(svc *session.Service) error {
		p, err := svc.People(cmd.Context())
		if err != nil {
			return err
		}
		for _, c := range p.Classes {
			d.Classes = append(d.Classes, c.Name)
			d.Members = append(d.Members, len(p.Members[c.ID]))
		}
		return nil
	})
	for _, ip := range netutil.LANAddrs() {
		d.Addrs = append(d.Addrs, ip.String())
	}
	m := tui.NewPrepare(d)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return err
	}
	if !m.Open {
		return errBack
	}
	sf.class, sf.rosterOnly = m.Class, m.RosterOnly
	return startExam(cmd, sf, ref, m.Duration, false)
}

// recoverSession shows what happened to a session whose server stopped, and
// resumes it, giving the lost time back, or ends it. leave closes the hub.
func recoverSession(cmd *cobra.Command, dir string) (leave bool, err error) {
	ctx := cmd.Context()
	d := &tui.RecoverData{Now: time.Now()}
	if st, err := daemon.ReadState(dir); err == nil && !st.Heartbeat.IsZero() {
		d.Down = time.Since(st.Heartbeat)
	}
	var open *session.Session
	err = withStore(ctx, dir, func(s *store) error {
		var err error
		if open, err = s.svc.OpenSession(ctx); err != nil {
			return err
		}
		all, err := s.svc.Sessions(ctx)
		if err != nil {
			return err
		}
		for _, x := range all {
			if x.ID == open.ID {
				d.ExamTitle, d.Joined, d.Submitted = x.ExamTitle, x.Joined, x.Submitted
			}
		}
		return nil
	})
	if errors.Is(err, session.ErrNotFound) {
		return false, errors.New(T("cli.resume.nothing_open"))
	}
	if err != nil {
		return false, err
	}
	d.Code, d.Running, d.EndsAt = open.Code, open.State == session.StateRunning, open.EndsAt
	d.Waiting = d.Joined - d.Submitted
	if data, err := os.ReadFile(datadir.Log(dir)); err == nil {
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		d.Log = lines[max(0, len(lines)-12):]
	}
	m := tui.NewRecover(d)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return false, err
	}
	switch {
	case m.End:
		if err := endOpenSession(ctx, dir); err != nil {
			return false, err
		}
		return false, errBack
	case m.Resume:
		if m.Extra > 0 && d.Running {
			if err := withService(ctx, dir, func(svc *session.Service) error {
				_, err := svc.ExtendSession(ctx, open.ID, m.Extra)
				return err
			}); err != nil {
				return false, err
			}
		}
		return true, resumeSession(cmd, defaultServeFlags(), false)
	}
	return false, errBack
}

// endOpenSession ends the open session while no server runs, and archives it.
func endOpenSession(ctx context.Context, dir string) error {
	return withStore(ctx, dir, func(s *store) error {
		open, err := s.svc.OpenSession(ctx)
		if errors.Is(err, session.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := s.svc.End(ctx, open.ID); err != nil {
			return err
		}
		closeArchive(ctx, s, open.ID)
		return nil
	})
}
