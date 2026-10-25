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
	actEdit   = "edit"
	actNew    = "new"
	actStart  = "start"
	actResume = "resume"
	actReport = "report"
	actQuit   = "quit"
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
		out = append(out, huh.NewOption(fmt.Sprintf("Reanudar «%s» (sesión %s, sin servidor)", st.lastTitles[st.open.ID], st.open.Code), actResume))
	}
	out = append(out,
		huh.NewOption("Editar un examen", actEdit),
		huh.NewOption("Crear un examen nuevo", actNew))
	if st.open == nil {
		out = append(out, huh.NewOption("Iniciar un examen", actStart))
	}
	if st.lastEnded != nil {
		out = append(out, huh.NewOption(fmt.Sprintf("Informe de la última sesión («%s», %s)", st.lastTitles[st.lastEnded.ID], st.lastEnded.Code), actReport))
	}
	return append(out, huh.NewOption("Salir", actQuit))
}

// home is the menu of a bare `ax` when no exam is running.
func home(cmd *cobra.Command, dir string) error {
	for {
		st := readHomeState(cmd, dir)
		note := "No hay ningún examen en marcha."
		switch {
		case st.health == daemon.Crashed:
			note = "El servidor se detuvo sin terminar el examen: puedes reanudarlo y nadie pierde sus respuestas."
		case st.open != nil:
			note = "Hay una sesión abierta sin servidor."
		}
		choice := actQuit
		err := runForm(huh.NewForm(huh.NewGroup(
			huh.NewNote().Title("ASSESS").Description(note),
			huh.NewSelect[string]().Title("¿Qué quieres hacer?").Options(homeOptions(st)...).Value(&choice),
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
			folder, err := askNewExam()
			if errors.Is(err, huh.ErrUserAborted) {
				continue
			}
			if err != nil {
				return err
			}
			if err := editExam(cmd, defaultServeFlags(), folder, 0); err != nil {
				return err
			}
			return nil
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
	title := "¿Qué examen quieres editar?"
	if starting {
		title = "¿Qué examen quieres iniciar?"
	}
	const other = "\x00other"
	found := findExams(".", 3)
	choice := other
	if len(found) > 0 {
		var opts []huh.Option[string]
		for _, e := range found {
			opts = append(opts, huh.NewOption(fmt.Sprintf("%s  ·  %s", e.title, e.dir), e.dir))
		}
		opts = append(opts, huh.NewOption("Otra carpeta…", other))
		choice = found[0].dir
		if err := runForm(huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title(title).Options(opts...).Value(&choice)))); err != nil {
			return "", err
		}
	}
	if choice != other {
		return choice, nil
	}
	var path string
	err := runForm(huh.NewForm(huh.NewGroup(huh.NewInput().Title(title).Description("Ruta de la carpeta del examen (la que tiene manifest.yaml).").
		Value(&path).Validate(func(s string) error {
		if _, err := os.Stat(filepath.Join(strings.TrimSpace(s), exam.ManifestFile)); err != nil {
			return errors.New("ahí no hay un manifest.yaml")
		}
		return nil
	}))))
	return strings.TrimSpace(path), err
}

// askNewExam creates an exam folder from the template and returns it.
func askNewExam() (string, error) {
	var folder, title string
	lang := "es"
	err := runForm(huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Carpeta del examen nuevo").Description("Se crea con una pregunta de ejemplo de cada tipo.").
			Placeholder("examenes/algebra-01").Value(&folder).Validate(func(s string) error {
			s = strings.TrimSpace(s)
			if s == "" {
				return errors.New("escribe una carpeta")
			}
			if entries, err := os.ReadDir(s); err == nil && len(entries) > 0 {
				return errors.New("esa carpeta ya existe y no está vacía")
			}
			if id := exam.Slug(filepath.Base(filepath.Clean(s))); !exam.ValidID(id) {
				return errors.New("usa un nombre con letras o números")
			}
			return nil
		}),
		huh.NewInput().Title("Título").Placeholder("Álgebra 1 — Ecuaciones").Value(&title),
		huh.NewSelect[string]().Title("Idioma de la página del alumno").Options(
			huh.NewOption("Español", "es"), huh.NewOption("English", "en")).Value(&lang),
	)))
	if err != nil {
		return "", err
	}
	folder = strings.TrimSpace(folder)
	return folder, exam.Scaffold(folder, exam.ScaffoldOptions{Title: strings.TrimSpace(title), Language: lang})
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
