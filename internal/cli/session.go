package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/datadir"
	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/netutil"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/storage/sqlite"
	"github.com/lisk0vian/assess/internal/version"
)

// dataDirFlag is the persistent --data-dir flag shared by session commands.
var dataDirFlag string

func dataDir() (string, error) { return datadir.Resolve(dataDirFlag) }

type serveFlags struct {
	host        string
	port        int
	joinWindow  time.Duration
	allowPublic bool
}

func (f *serveFlags) register(cmd *cobra.Command) {
	d := defaultServeFlags()
	cmd.Flags().StringVar(&f.host, "host", d.host, T("cli.flag.host"))
	cmd.Flags().IntVar(&f.port, "port", d.port, T("cli.flag.port"))
	cmd.Flags().DurationVar(&f.joinWindow, "join-window", d.joinWindow, T("cli.flag.join_window"))
	cmd.Flags().BoolVar(&f.allowPublic, "allow-public", d.allowPublic, T("cli.flag.allow_public"))
}

// defaultServeFlags are the flag defaults (from the settings), for actions
// started from the menu.
func defaultServeFlags() *serveFlags {
	return &serveFlags{host: cfg.Str("server.host"), port: cfg.Int("server.port"),
		joinWindow: cfg.Dur("server.join_window"), allowPublic: cfg.Bool("server.allow_public")}
}

func (f *serveFlags) daemonArgs(cmd *cobra.Command) []string {
	args := []string{"--port", strconv.Itoa(f.port), "--join-window", f.joinWindow.String()}
	if f.allowPublic {
		args = append(args, "--allow-public")
	}
	if f.host != "" {
		args = append(args, "--host", f.host)
	}
	if !cmd.Flags().Changed("port") {
		args = append(args, "--port-fallback")
	}
	return args
}

// withService opens the data directory's database for a short operation.
func withService(ctx context.Context, dir string, fn func(*session.Service) error) error {
	db, err := sqlite.Open(ctx, datadir.DB(dir))
	if err != nil {
		return err
	}
	defer db.Close()
	return fn(session.NewService(db))
}

// ---------------- start ----------------

func newStartCmd() *cobra.Command {
	var sf serveFlags
	var duration time.Duration
	var noAttach bool
	cmd := &cobra.Command{
		Use:   "start <exam-folder>",
		Short: T("cli.start.short"),
		Long:  T("cli.start.long"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return startExam(cmd, &sf, args[0], duration, noAttach)
		},
	}
	sf.register(cmd)
	cmd.Flags().DurationVar(&duration, "duration", 0, T("cli.start.flag_duration"))
	cmd.Flags().BoolVar(&noAttach, "no-attach", false, T("cli.flag.no_attach"))
	return cmd
}

// startExam publishes the exam at examDir, opens its session and launches
// the server in the background, then opens the panel unless noAttach.
func startExam(cmd *cobra.Command, sf *serveFlags, examDir string, duration time.Duration, noAttach bool) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	dir, err := dataDir()
	if err != nil {
		return err
	}
	if h, st, _ := daemon.Check(dir); h == daemon.Running {
		return errors.New(T("cli.start.already_running", "title", st.ExamTitle))
	}
	pub, probs, err := exam.Publish(examDir)
	if errors.Is(err, exam.ErrInvalidExam) {
		printReport(cmd.ErrOrStderr(), validateReport{Path: examDir, Errors: probs.Count(exam.SeverityError), Warnings: probs.Count(exam.SeverityWarning), Problems: probs})
		return errSilent
	}
	if err != nil {
		return err
	}
	grace := cfg.Dur("server.late_answers")
	if grace == 0 {
		grace = -1 // none
	}
	err = withService(ctx, dir, func(svc *session.Service) error {
		if open, err := svc.OpenSession(ctx); err == nil {
			return errors.New(T("cli.start.still_open", "code", open.Code))
		} else if !errors.Is(err, session.ErrNotFound) {
			return err
		}
		_, err := svc.CreateSession(ctx, pub, session.CreateOptions{Duration: duration, Grace: grace})
		return err
	})
	if err != nil {
		return err
	}
	st, err := daemon.Spawn(dir, sf.daemonArgs(cmd))
	if err != nil {
		return err
	}
	printStarted(out, st, false)
	if noAttach {
		return nil
	}
	return attach(cmd, dir)
}

func printStarted(w io.Writer, st *daemon.State, resumed bool) {
	fmt.Fprintln(w, T("cli.start.running", "pid", st.PID))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %-9s %s\n", T("cli.start.exam"), st.ExamTitle)
	fmt.Fprintf(w, "  %-9s %s\n", T("cli.start.students"), st.StudentURL)
	for _, u := range st.OtherURLs {
		fmt.Fprintf(w, "  %-9s %s\n", "", u)
	}
	fmt.Fprintf(w, "  %-9s %s\n\n", T("cli.start.code"), st.Code)
	if resumed {
		fmt.Fprintln(w, T("cli.start.resumed_hint"))
		return
	}
	fmt.Fprintln(w, T("cli.start.lobby_hint"))
}

// ---------------- status ----------------

func newStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: T("cli.status.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			dir, err := dataDir()
			if err != nil {
				return err
			}
			health, st, _ := daemon.Check(dir)
			var sv *daemon.StatusView
			if health == daemon.Running {
				if c, _, err := daemon.Dial(dir); err == nil {
					sv, _ = c.Status(cmd.Context())
				}
			}
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{"health": health, "status": sv})
			}
			switch health {
			case daemon.Running:
				fmt.Fprintln(out, T("cli.status.running", "title", st.ExamTitle))
				if sv != nil {
					printStatus(out, sv)
				}
			case daemon.Crashed:
				fmt.Fprintln(out, T("cli.status.crashed"))
				printLogTail(out, dir, 10)
				fmt.Fprintln(out, "\n"+T("cli.status.resume_hint"))
				return errSilent
			default:
				fmt.Fprintln(out, T("cli.status.not_running"))
				_ = withService(cmd.Context(), dir, func(svc *session.Service) error {
					if open, err := svc.OpenSession(cmd.Context()); err == nil {
						fmt.Fprintln(out, "  "+T("cli.status.still_open", "code", open.Code))
					}
					return nil
				})
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, T("cli.flag.json"))
	return cmd
}

func printStatus(w io.Writer, sv *daemon.StatusView) {
	state := T("state." + string(sv.State))
	if sv.State == session.StateRunning && sv.EndsAt > 0 {
		left := max(time.Duration(sv.EndsAt-sv.ServerNow)*time.Millisecond, 0)
		state = T("cli.status.running_left", "left", clock(left))
	}
	fmt.Fprintf(w, "  %-9s %s (%s %s)\n", T("cli.status.session"), state, T("cli.status.code"), sv.Code)
	fmt.Fprintf(w, "  %-9s %s\n", T("cli.start.students"), sv.StudentURL)
	if sv.State == session.StateLobby {
		fmt.Fprintln(w, "  "+T("cli.status.lobby_counts", "joined", sv.Joined))
	} else {
		fmt.Fprintln(w, "  "+T("cli.status.counts", "joined", sv.Joined, "online", sv.Online, "active", sv.Active, "submitted", sv.Submitted))
	}
	fmt.Fprintln(w, "  "+T("cli.status.process", "pid", sv.PID, "up", (time.Duration(sv.UptimeMs)*time.Millisecond).Truncate(time.Second).String()))
	if sv.LastError != "" {
		fmt.Fprintln(w, "  "+T("cli.status.last_error", "err", sv.LastError))
	}
}

func clock(d time.Duration) string {
	d = d.Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// ---------------- stop ----------------

func newStopCmd() *cobra.Command {
	var suspend, yes bool
	cmd := &cobra.Command{
		Use:   "stop",
		Short: T("cli.stop.short"),
		Long:  T("cli.stop.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			dir, err := dataDir()
			if err != nil {
				return err
			}
			if err := requirePassword(cmd, dir); err != nil {
				return err
			}
			if !suspend && !yes && !confirm(cmd, T("cli.stop.confirm")) {
				return errors.New(T("cli.cancelled"))
			}
			health, _, _ := daemon.Check(dir)
			if health == daemon.Running {
				c, _, err := daemon.Dial(dir)
				if err != nil {
					return err
				}
				if err := c.Shutdown(ctx, !suspend); err != nil {
					return err
				}
				if !waitStopped(dir, 10*time.Second) {
					return errors.New(T("cli.stop.timeout"))
				}
			} else if !suspend {
				err := withService(ctx, dir, func(svc *session.Service) error {
					open, err := svc.OpenSession(ctx)
					if errors.Is(err, session.ErrNotFound) {
						return nil
					}
					if err != nil {
						return err
					}
					return svc.End(ctx, open.ID)
				})
				if err != nil {
					return err
				}
			}
			if suspend {
				fmt.Fprintln(out, T("cli.stop.suspended"))
			} else {
				fmt.Fprintln(out, T("cli.stop.ended"))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&suspend, "suspend", false, T("cli.stop.flag_suspend"))
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, T("cli.flag.yes"))
	return cmd
}

func waitStopped(dir string, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if h, _, _ := daemon.Check(dir); h != daemon.Running {
			return true
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false
}

func confirm(cmd *cobra.Command, question string) bool {
	fmt.Fprintf(cmd.OutOrStdout(), "%s %s ", question, T("cli.yes_no"))
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "s", "si", "sí":
		return true
	}
	return false
}

// ---------------- resume ----------------

func newResumeCmd() *cobra.Command {
	var sf serveFlags
	var noAttach bool
	cmd := &cobra.Command{
		Use:   "resume",
		Short: T("cli.resume.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return resumeSession(cmd, &sf, noAttach)
		},
	}
	sf.register(cmd)
	cmd.Flags().BoolVar(&noAttach, "no-attach", false, T("cli.flag.no_attach"))
	return cmd
}

// resumeSession restarts the server of the open session.
func resumeSession(cmd *cobra.Command, sf *serveFlags, noAttach bool) error {
	ctx := cmd.Context()
	dir, err := dataDir()
	if err != nil {
		return err
	}
	if h, _, _ := daemon.Check(dir); h == daemon.Running {
		return errors.New(T("cli.resume.already_running"))
	}
	err = withService(ctx, dir, func(svc *session.Service) error {
		_, err := svc.OpenSession(ctx)
		if errors.Is(err, session.ErrNotFound) {
			return errors.New(T("cli.resume.nothing_open"))
		}
		return err
	})
	if err != nil {
		return err
	}
	st, err := daemon.Spawn(dir, sf.daemonArgs(cmd))
	if err != nil {
		return err
	}
	printStarted(cmd.OutOrStdout(), st, true)
	if noAttach {
		return nil
	}
	return attach(cmd, dir)
}

// ---------------- logs ----------------

func newLogsCmd() *cobra.Command {
	var follow bool
	var lines int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: T("cli.logs.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			printLogTail(out, dir, lines)
			if !follow {
				return nil
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			return followLog(ctx, out, datadir.Log(dir))
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, T("cli.logs.flag_follow"))
	cmd.Flags().IntVarP(&lines, "lines", "n", 40, T("cli.logs.flag_lines"))
	return cmd
}

func printLogTail(w io.Writer, dir string, n int) {
	data, err := os.ReadFile(datadir.Log(dir))
	if err != nil {
		fmt.Fprintln(w, "  "+T("cli.logs.empty"))
		return
	}
	all := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(all) > n {
		all = all[len(all)-n:]
	}
	for _, l := range all {
		fmt.Fprintln(w, "  "+l)
	}
}

func followLog(ctx context.Context, w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	buf := make([]byte, 32<<10)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if n == 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(400 * time.Millisecond):
			}
		}
	}
}

// ---------------- daemon (hidden) ----------------

func newDaemonCmd() *cobra.Command {
	var host string
	var port int
	var fallback, allowPublic bool
	var joinWindow time.Duration
	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Run the exam server in the foreground (used by start/resume)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
			for _, p := range cfg.Problems {
				log.Warn("settings", "problem", p.Error())
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			err = daemon.Run(ctx, daemon.Config{DataDir: dir, Host: host, Port: port, PortFallback: fallback,
				JoinWindow: joinWindow, AllowPublic: allowPublic, Version: version.String(), Log: log})
			if err != nil {
				log.Error("daemon stopped with an error", "err", err)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "listen address")
	cmd.Flags().IntVar(&port, "port", netutil.DefaultPort, "student port")
	cmd.Flags().BoolVar(&fallback, "port-fallback", false, "use the next free port when busy")
	cmd.Flags().DurationVar(&joinWindow, "join-window", 0, "close joining this long after the start")
	cmd.Flags().BoolVar(&allowPublic, "allow-public", false, "accept non-private addresses")
	return cmd
}

// ---------------- attach ----------------

func newAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach",
		Short: T("cli.attach.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			return attach(cmd, dir)
		},
	}
}
