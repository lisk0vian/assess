package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/tui"
)

// Menu actions.
const (
	actEdit     = "edit"
	actNew      = "new"
	actStart    = "start"
	actResume   = "resume"
	actReport   = "report"
	actSettings = "settings"
	actQuit     = "quit"
)

// homeState is what the menu needs to know about the data directory.
type homeState struct {
	health     daemon.Health
	open       *session.Session // lobby or running, without a server
	lastEnded  *session.Session
	lastTitles map[string]string // session id → exam title
}

func readHomeState(cmd *cobra.Command, dir string) homeState {
	st := homeState{lastTitles: map[string]string{}}
	st.health, _, _ = daemon.Check(dir)
	_ = withService(cmd.Context(), dir, func(svc *session.Service) error {
		if open, err := svc.OpenSession(cmd.Context()); err == nil {
			st.open = open
		}
		if last, err := svc.LatestSession(cmd.Context()); err == nil && last.State == session.StateEnded {
			st.lastEnded = last
		}
		for _, s := range []*session.Session{st.open, st.lastEnded} {
			if s != nil {
				if _, pub, err := svc.Session(cmd.Context(), s.ID); err == nil {
					st.lastTitles[s.ID] = pub.Exam.Manifest.Title
				}
			}
		}
		return nil
	})
	return st
}

// homeOptions lists what can be done now, the likeliest first.
func homeOptions(st homeState) []huh.Option[string] {
	var out []huh.Option[string]
	if st.open != nil {
		out = append(out, huh.NewOption(T("home.resume", "title", st.lastTitles[st.open.ID], "code", st.open.Code), actResume))
	}
	out = append(out,
		huh.NewOption(T("home.edit"), actEdit),
		huh.NewOption(T("home.new"), actNew))
	if st.open == nil {
		out = append(out, huh.NewOption(T("home.start"), actStart))
	}
	if st.lastEnded != nil {
		out = append(out, huh.NewOption(T("home.report", "title", st.lastTitles[st.lastEnded.ID], "code", st.lastEnded.Code), actReport))
	}
	return append(out, huh.NewOption(T("home.settings"), actSettings), huh.NewOption(T("home.quit"), actQuit))
}

// home is the menu of a bare `ax` when no exam is running.
func home(cmd *cobra.Command, dir string) error {
	for {
		st := readHomeState(cmd, dir)
		note := T("home.idle")
		switch {
		case st.health == daemon.Crashed:
			note = T("home.crashed")
		case st.open != nil:
			note = T("home.open")
		}
		choice := actQuit
		err := runForm(huh.NewForm(huh.NewGroup(
			huh.NewNote().Title("ASSESS").Description(note),
			huh.NewSelect[string]().Title(T("home.question")).Options(homeOptions(st)...).Value(&choice),
		)))
		if errors.Is(err, huh.ErrUserAborted) || choice == actQuit {
			return nil
		}
		if err != nil {
			return err
		}
		switch choice {
		case actResume:
			return resumeSession(cmd, defaultServeFlags(), false)
		case actReport:
			if err := requirePassword(cmd, dir); err != nil {
				return err
			}
			if err := showReport(cmd, dir, st.lastEnded.ID); err != nil {
				return err
			}
		case actNew:
			return newExamWizard(cmd)
		case actSettings:
			if err := editSettings(cmd); err != nil {
				return err
			}
		case actEdit, actStart:
			folder, err := askExamFolder(choice == actStart)
			if errors.Is(err, huh.ErrUserAborted) {
				continue
			}
			if err != nil {
				return err
			}
			if choice == actStart {
				return startExam(cmd, defaultServeFlags(), folder, 0, false)
			}
			return editExam(cmd, defaultServeFlags(), folder, 0)
		}
	}
}

func runForm(f *huh.Form) error { return tui.StyleForm(f).WithShowHelp(true).Run() }

// askExamFolder offers the exam folders found under the current folder,
// or any path.
func askExamFolder(starting bool) (string, error) {
	title := T("home.pick_edit")
	if starting {
		title = T("home.pick_start")
	}
	const other = "\x00other"
	found := findExams(".", 3)
	choice := other
	if len(found) > 0 {
		var opts []huh.Option[string]
		for _, e := range found {
			opts = append(opts, huh.NewOption(fmt.Sprintf("%s  ·  %s", e.title, e.dir), e.dir))
		}
		opts = append(opts, huh.NewOption(T("home.other_folder"), other))
		choice = found[0].dir
		if err := runForm(huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title(title).Options(opts...).Value(&choice)))); err != nil {
			return "", err
		}
	}
	if choice != other {
		return choice, nil
	}
	var path string
	err := runForm(huh.NewForm(huh.NewGroup(huh.NewInput().Title(title).Description(T("home.folder_hint")).
		Value(&path).Validate(func(s string) error {
		if _, err := os.Stat(filepath.Join(strings.TrimSpace(s), exam.ManifestFile)); err != nil {
			return errors.New(T("home.no_manifest"))
		}
		return nil
	}))))
	return strings.TrimSpace(path), err
}

type foundExam struct{ dir, title string }

// skipDirs are never searched for exams.
var skipDirs = []string{"node_modules", "vendor", "dist", "build", "invalid"}

// findExams lists the folders with a manifest under root, up to depth
// levels down, whether or not they validate.
func findExams(root string, depth int) []foundExam {
	var out []foundExam
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if rel != "." {
			if strings.HasPrefix(d.Name(), ".") || slices.Contains(skipDirs, d.Name()) || strings.Count(rel, string(filepath.Separator)) >= depth {
				return filepath.SkipDir
			}
		}
		if _, err := os.Stat(filepath.Join(p, exam.ManifestFile)); err != nil {
			return nil
		}
		title := filepath.Base(p)
		if ex, _, err := exam.Load(p); err == nil && ex.Manifest.Title != "" {
			title = ex.Manifest.Title
		}
		out = append(out, foundExam{dir: p, title: title})
		return filepath.SkipDir // an exam does not contain exams
	})
	return out
}
