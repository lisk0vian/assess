package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/i18n"
	"github.com/lisk0vian/assess/internal/server/student"
	"github.com/lisk0vian/assess/internal/server/web"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/storage/sqlite"
)

func newTryCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "try [exam]",
		Short:   T("cli.try.short"),
		Long:    T("cli.try.long"),
		Example: "ax exam try\nax exam try historia-tema-3",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 { // the example exam, in the terminal's language
				tmp, err := os.MkdirTemp("", "ax-example-")
				if err != nil {
					return err
				}
				defer os.RemoveAll(tmp)
				dir := filepath.Join(tmp, "example")
				if err := exam.Scaffold(dir, exam.ScaffoldOptions{Language: i18n.Language()}); err != nil {
					return err
				}
				return tryExam(cmd, dir, nil)
			}
			dataDir, err := dataDir()
			if err != nil {
				return err
			}
			dir, cleanup, err := examFolder(cmd, dataDir, args[0])
			if err != nil {
				return err
			}
			defer cleanup()
			return tryExam(cmd, dir, nil)
		},
	}
}

// tryExam lets the teacher sit an exam as a student would, in the browser of
// this computer. The session is a rehearsal in a throwaway database: nothing
// reaches the vault or a student's history. It ends on Enter or Ctrl+C, or
// when done is closed.
func tryExam(cmd *cobra.Command, examDir string, done <-chan struct{}) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	out := cmd.OutOrStdout()
	pub, probs, err := exam.Publish(examDir)
	if errors.Is(err, exam.ErrInvalidExam) {
		printReport(cmd.ErrOrStderr(), validateReport{Path: examDir, Errors: probs.Count(exam.SeverityError), Warnings: probs.Count(exam.SeverityWarning), Problems: probs})
		return errSilent
	}
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "ax-try-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	db, err := sqlite.Open(ctx, filepath.Join(tmp, "try.db"))
	if err != nil {
		return err
	}
	defer db.Close()

	var students *student.Handler
	svc := session.NewService(db, session.WithNotify(func(id string) {
		if students != nil {
			students.Notify(id)
		}
	}))
	sess, err := svc.CreateSession(ctx, pub, session.CreateOptions{})
	if err != nil {
		return err
	}
	if _, err := svc.Start(ctx, sess.ID); err != nil {
		return err
	}
	students = student.New(svc, sess.ID, student.Options{})
	mux := http.NewServeMux()
	lang := pub.Exam.Manifest.Lang()
	mux.Handle("/api/", students)
	mux.Handle("GET /api/i18n", web.StringsHandler(lang))
	mux.Handle("/", web.Handler(lang))
	// Only this computer: 127.0.0.1 is also a secure context for the browser.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	url := fmt.Sprintf("http://%s/?c=%s", ln.Addr(), sess.Code)
	fmt.Fprintln(out, T("cli.try.open", "title", pub.Exam.Manifest.Title, "url", url, "code", sess.Code))
	if err := openBrowser(url); err != nil {
		fmt.Fprintln(out, T("cli.try.open_yourself"))
	}
	fmt.Fprintln(out, T("cli.try.wait"))
	waitForEnd(ctx, cmd, done)
	return tryVerdict(context.WithoutCancel(ctx), cmd, svc, sess.ID)
}

// waitForEnd returns on Enter, Ctrl+C or done.
func waitForEnd(ctx context.Context, cmd *cobra.Command, done <-chan struct{}) {
	enter := make(chan struct{})
	go func() {
		_, _ = bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		close(enter)
	}()
	select {
	case <-enter:
	case <-ctx.Done():
	case <-done:
	}
}

// tryVerdict says how the rehearsal went, then forgets it.
func tryVerdict(ctx context.Context, cmd *cobra.Command, svc *session.Service, sessionID string) error {
	out := cmd.OutOrStdout()
	res, err := svc.Results(ctx, sessionID)
	if err != nil {
		return err
	}
	said := false
	for _, r := range res.Rows {
		if r.Score != nil {
			fmt.Fprintln(out, T("cli.try.scored", "score", exam.FormatPoints(*r.Score), "max", exam.FormatPoints(r.MaxScore), "pct", exam.FormatPoints(pct100(*r.Score, r.MaxScore))))
			said = true
		}
	}
	if !said {
		fmt.Fprintln(out, T("cli.try.unsubmitted"))
	}
	fmt.Fprintln(out, T("cli.try.forgotten"))
	return nil
}

func pct100(score, maxScore float64) float64 {
	if maxScore <= 0 {
		return 0
	}
	return round1(score / maxScore * 100)
}

// openBrowser opens url in the default browser of this computer; tests replace it.
var openBrowser = func(url string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		c = exec.Command("open", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	return c.Start()
}
