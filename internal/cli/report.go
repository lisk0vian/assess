package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/report"
	"github.com/lisk0vian/assess/internal/security"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/tui"
)

// historyLimit bounds how many earlier sessions feed the history and the
// leak check.
const historyLimit = 20

func newReportCmd() *cobra.Command {
	var sessionID string
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "report",
		Short:   T("cli.report.short"),
		Long:    T("cli.report.long"),
		Example: T("cli.report.example"),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			if err := requirePassword(cmd, dir); err != nil {
				return err
			}
			rep, err := loadReport(cmd.Context(), dir, sessionID)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			switch {
			case asJSON:
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(rep)
			case !isTerminal():
				_, err := fmt.Fprint(w, tui.ReportText(rep, 110))
				return err
			}
			return runReport(cmd, dir, rep)
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", T("cli.flag.session"))
	cmd.Flags().BoolVar(&asJSON, "json", false, T("cli.flag.json"))
	return cmd
}

// showReport opens the report of sessionID; the password was already asked.
func showReport(cmd *cobra.Command, dir, sessionID string) error {
	rep, err := loadReport(cmd.Context(), dir, sessionID)
	if err != nil {
		return err
	}
	return runReport(cmd, dir, rep)
}

// runReport shows rep in the interactive panel.
func runReport(cmd *cobra.Command, dir string, rep *report.Report) error {
	idle := time.Duration(cfg.Int("panel.lock_minutes")) * time.Minute
	if !security.HasPassword(dir) {
		idle = 0
	}
	m := tui.NewReport(rep, idle)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return err
	}
	if m.TimedOut {
		fmt.Fprintln(cmd.OutOrStdout(), T("cli.report.timed_out", "n", int(idle.Minutes())))
	}
	return nil
}

// loadReport analyses sessionID (or the most recent session) together with
// the earlier finished sessions of the same exam.
func loadReport(ctx context.Context, dir, sessionID string) (*report.Report, error) {
	var rep *report.Report
	err := withService(ctx, dir, func(svc *session.Service) error {
		if sessionID == "" {
			latest, err := svc.LatestSession(ctx)
			if errors.Is(err, session.ErrNotFound) {
				return errors.New(T("cli.no_sessions"))
			}
			if err != nil {
				return err
			}
			sessionID = latest.ID
		}
		cur, err := svc.Archive(ctx, sessionID)
		if errors.Is(err, session.ErrNotFound) {
			return errors.New(T("cli.no_session", "id", sessionID))
		}
		if err != nil {
			return err
		}
		if cur.Session.State != session.StateEnded {
			return errors.New(T("cli.report.not_ended"))
		}
		all, err := svc.ExamSessions(ctx, cur.Published.Exam.Manifest.ID)
		if err != nil {
			return err
		}
		var earlier []*session.Archive
		for _, s := range all {
			if s.ID == cur.Session.ID || s.State != session.StateEnded || s.StartedAt.IsZero() || !s.StartedAt.Before(cur.Session.StartedAt) {
				continue
			}
			arc, err := svc.Archive(ctx, s.ID)
			if err != nil {
				return err
			}
			earlier = append(earlier, arc)
		}
		if len(earlier) > historyLimit {
			earlier = earlier[len(earlier)-historyLimit:]
		}
		rep = report.Build(cur, earlier)
		return nil
	})
	return rep, err
}
