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
	cmd.Flags().StringVar(&f.host, "host", "", "address students connect to (default: all interfaces, LAN address advertised)")
	cmd.Flags().IntVar(&f.port, "port", netutil.DefaultPort, "port for students")
	cmd.Flags().DurationVar(&f.joinWindow, "join-window", 15*time.Minute, "close joining this long after the start (0 keeps it open)")
	cmd.Flags().BoolVar(&f.allowPublic, "allow-public", false, "accept students from outside private networks (behind your own tunnel)")
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
		Short: "Publish an exam and start its session server in the background",
		Long: "Start validates and publishes the exam, opens a session in the lobby and\n" +
			"launches the exam server in the background. Closing the terminal or the\n" +
			"TUI never stops the exam; use `ax stop` for that.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			dir, err := dataDir()
			if err != nil {
				return err
			}
			if h, st, _ := daemon.Check(dir); h == daemon.Running {
				return fmt.Errorf("an exam is already running (%s); use `ax attach` or `ax stop`", st.ExamTitle)
			}
			pub, probs, err := exam.Publish(args[0])
			if errors.Is(err, exam.ErrInvalidExam) {
				printReport(cmd.ErrOrStderr(), validateReport{Path: args[0], Errors: probs.Count(exam.SeverityError), Warnings: probs.Count(exam.SeverityWarning), Problems: probs})
				return errSilent
			}
			if err != nil {
				return err
			}
			err = withService(ctx, dir, func(svc *session.Service) error {
				if open, err := svc.OpenSession(ctx); err == nil {
					return fmt.Errorf("session %s is still open without a server; use `ax resume` to continue it or `ax stop` to end it", open.Code)
				} else if !errors.Is(err, session.ErrNotFound) {
					return err
				}
				_, err := svc.CreateSession(ctx, pub, session.CreateOptions{Duration: duration})
				return err
			})
			if err != nil {
				return err
			}
			st, err := daemon.Spawn(dir, sf.daemonArgs(cmd))
			if err != nil {
				return err
			}
			printStarted(out, st)
			if noAttach {
				return nil
			}
			return attach(cmd, dir)
		},
	}
	sf.register(cmd)
	cmd.Flags().DurationVar(&duration, "duration", 0, "exam duration (default: the manifest's)")
	cmd.Flags().BoolVar(&noAttach, "no-attach", false, "do not open the session panel")
	return cmd
}

func printStarted(w io.Writer, st *daemon.State) {
	fmt.Fprintf(w, "Exam server running in the background (pid %d)\n\n", st.PID)
	fmt.Fprintf(w, "  Exam      %s\n", st.ExamTitle)
	fmt.Fprintf(w, "  Students  %s\n", st.StudentURL)
	for _, u := range st.OtherURLs {
		fmt.Fprintf(w, "            %s\n", u)
	}
	fmt.Fprintf(w, "  Code      %s\n\n", st.Code)
	fmt.Fprintln(w, "Students wait in the lobby until you start the exam (`ax attach`, then S).")
}

// ---------------- status ----------------

func newStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show whether the exam server is running",
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
				fmt.Fprintf(out, "● Running  %s\n", st.ExamTitle)
				if sv != nil {
					printStatus(out, sv)
				}
			case daemon.Crashed:
				fmt.Fprintln(out, "✕ Crashed  the exam server stopped unexpectedly; answers saved so far are safe")
				printLogTail(out, dir, 10)
				fmt.Fprintln(out, "\nRun `ax resume` to continue the session.")
				return errSilent
			default:
				fmt.Fprintln(out, "○ Not running")
				_ = withService(cmd.Context(), dir, func(svc *session.Service) error {
					if open, err := svc.OpenSession(cmd.Context()); err == nil {
						fmt.Fprintf(out, "  Session %s is still open: `ax resume` continues it, `ax stop` ends it.\n", open.Code)
					}
					return nil
				})
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable status")
	return cmd
}

func printStatus(w io.Writer, sv *daemon.StatusView) {
	state := string(sv.State)
	if sv.State == session.StateRunning && sv.EndsAt > 0 {
		left := time.Duration(sv.EndsAt-sv.ServerNow) * time.Millisecond
		if left < 0 {
			left = 0
		}
		state = fmt.Sprintf("running · %s left", clock(left))
	}
	fmt.Fprintf(w, "  Session   %s (code %s)\n", state, sv.Code)
	fmt.Fprintf(w, "  Students  %s\n", sv.StudentURL)
	if sv.State == session.StateLobby {
		fmt.Fprintf(w, "  Joined %d · waiting in the lobby\n", sv.Joined)
	} else {
		fmt.Fprintf(w, "  Joined %d · online %d · answering %d · submitted %d\n", sv.Joined, sv.Online, sv.Active, sv.Submitted)
	}
	fmt.Fprintf(w, "  PID %d · up %s\n", sv.PID, (time.Duration(sv.UptimeMs) * time.Millisecond).Truncate(time.Second))
	if sv.LastError != "" {
		fmt.Fprintf(w, "  Last error: %s\n", sv.LastError)
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
		Short: "End the exam and stop the server (or --suspend to keep the session)",
		Long: "Stop ends the exam for everyone, submits every attempt with what is saved\n" +
			"and stops the server. With --suspend the server stops but the session stays\n" +
			"open, the clock keeps running, and `ax resume` continues it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			dir, err := dataDir()
			if err != nil {
				return err
			}
			if !suspend && !yes && !confirm(cmd, "End the exam for everyone and submit all attempts?") {
				return errors.New("cancelled")
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
					return errors.New("the server did not stop in time; check `ax status`")
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
				fmt.Fprintln(out, "Server stopped; the session stays open. `ax resume` continues it.")
			} else {
				fmt.Fprintln(out, "Exam ended and server stopped. Results are saved.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&suspend, "suspend", false, "stop the server but keep the session open")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
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
	fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N] ", question)
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
		Short: "Restart the server for a session that is still open",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			dir, err := dataDir()
			if err != nil {
				return err
			}
			if h, _, _ := daemon.Check(dir); h == daemon.Running {
				return errors.New("the exam server is already running; use `ax attach`")
			}
			err = withService(ctx, dir, func(svc *session.Service) error {
				_, err := svc.OpenSession(ctx)
				if errors.Is(err, session.ErrNotFound) {
					return errors.New("there is no open session to resume; use `ax start <exam-folder>`")
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
			printStarted(cmd.OutOrStdout(), st)
			if noAttach {
				return nil
			}
			return attach(cmd, dir)
		},
	}
	sf.register(cmd)
	cmd.Flags().BoolVar(&noAttach, "no-attach", false, "do not open the session panel")
	return cmd
}

// ---------------- logs ----------------

func newLogsCmd() *cobra.Command {
	var follow bool
	var lines int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show the exam server log",
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
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new lines")
	cmd.Flags().IntVarP(&lines, "lines", "n", 40, "number of lines to show")
	return cmd
}

func printLogTail(w io.Writer, dir string, n int) {
	data, err := os.ReadFile(datadir.Log(dir))
	if err != nil {
		fmt.Fprintln(w, "  (no log yet)")
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
		Short: "Open the panel of the running session",
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
