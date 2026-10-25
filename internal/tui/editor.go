package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/lisk0vian/assess/internal/exam"
)

// Editor edits an exam folder: settings, questions and their answers.
// Every confirmed form is written to disk at once and the folder is
// validated again.
type Editor struct {
	d             *exam.Draft
	sel           int
	width, height int
	notice        string
	isErr         bool

	form     *huh.Form
	onSubmit func() error // applies the form when it completes
	confirm  string       // a yes/no question in progress
	onYes    func() error

	// Start reports that the editor closed asking to start the exam.
	Start bool
}

// editorRow is one line of the editor list.
type editorRow struct {
	kind  int // rowSettings, rowQuestion, rowPool
	id    string
	item  int // index in the manifest items; -1 for loose questions
	inner bool
}

const (
	rowSettings = iota
	rowQuestion
	rowPool
)

type editedMsg struct{ err error }

// NewEditor opens the editor on a draft.
func NewEditor(d *exam.Draft) *Editor { return &Editor{d: d, width: 100, height: 40} }

// Init does nothing; the draft is already loaded.
func (m *Editor) Init() tea.Cmd { return nil }

func (m *Editor) rows() []editorRow {
	rows := []editorRow{{kind: rowSettings, item: -1}}
	seen := map[string]bool{}
	for i, it := range m.d.Exam.Manifest.Items {
		if it.Pool != nil {
			rows = append(rows, editorRow{kind: rowPool, id: it.Pool.ID, item: i})
			for _, id := range it.Pool.From {
				seen[id] = true
				rows = append(rows, editorRow{kind: rowQuestion, id: id, item: i, inner: true})
			}
			continue
		}
		seen[it.Question] = true
		rows = append(rows, editorRow{kind: rowQuestion, id: it.Question, item: i})
	}
	for _, id := range m.d.QuestionOrder() {
		if !seen[id] {
			rows = append(rows, editorRow{kind: rowQuestion, id: id, item: -1})
		}
	}
	return rows
}

func (m *Editor) selected() editorRow {
	rows := m.rows()
	m.sel = min(max(0, m.sel), len(rows)-1)
	return rows[m.sel]
}

func (m *Editor) say(err error, ok string) {
	if err != nil {
		m.notice, m.isErr = err.Error(), true
		if errors.Is(err, exam.ErrBrokenFile) {
			m.notice += " (E abre el archivo)"
		}
		return
	}
	m.notice, m.isErr = ok, false
}

// Update handles forms, confirmations and the list keys.
func (m *Editor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = ws.Width, ws.Height
	}
	if ed, ok := msg.(editedMsg); ok {
		err := ed.err
		if err == nil {
			err = m.d.Reload()
		}
		m.say(err, "Archivo editado; el examen se ha vuelto a validar.")
		return m, nil
	}
	if m.form != nil {
		return m, m.updateForm(msg)
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	k := key.String()
	if m.confirm != "" {
		yes := m.onYes
		m.confirm, m.onYes = "", nil
		if k == "y" || k == "Y" || k == "s" || k == "S" {
			m.say(yes(), "Hecho.")
		} else {
			m.say(nil, "Cancelado.")
		}
		return m, nil
	}
	row := m.selected()
	switch k {
	case "q", "Q", "esc", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.sel--
	case "down", "j":
		m.sel++
	case "home", "g":
		m.sel = 0
	case "end", "G":
		m.sel = len(m.rows()) - 1
	case "enter":
		return m, m.edit(row)
	case "n", "N":
		return m, m.newQuestion()
	case "d", "D", "delete":
		if row.kind == rowQuestion {
			id := row.id
			m.confirm = fmt.Sprintf("¿Borrar la pregunta %s, su archivo y su respuesta?", id)
			m.onYes = func() error { return m.d.DeleteQuestion(id) }
		}
	case "shift+up", "K":
		m.move(row, -1)
	case "shift+down", "J":
		m.move(row, 1)
	case "e", "E":
		return m, m.external(row)
	case "c", "C":
		return m, m.openFile(exam.AnswerKeyFile)
	case "r", "R":
		m.say(m.d.Reload(), "Carpeta vuelta a leer y validar.")
	case "s", "S":
		if m.d.Problems.HasErrors() {
			m.say(errors.New("corrige los errores antes de iniciar el examen"), "")
			return m, nil
		}
		m.Start = true
		return m, tea.Quit
	}
	return m, nil
}

func (m *Editor) updateForm(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "esc" {
		m.form = nil
		m.say(nil, "Cambios descartados.")
		return nil
	}
	f, cmd := m.form.Update(msg)
	m.form = f.(*huh.Form)
	switch m.form.State {
	case huh.StateCompleted:
		apply := m.onSubmit
		m.form, m.onSubmit = nil, nil
		m.say(apply(), "Guardado.")
		return nil
	case huh.StateAborted:
		m.form = nil
		m.say(nil, "Cambios descartados.")
		return nil
	}
	return cmd
}

// open shows a form; apply runs when it completes.
func (m *Editor) open(f *huh.Form, apply func() error) tea.Cmd {
	m.form = StyleForm(f).WithShowHelp(true).WithWidth(min(m.w()-4, 100))
	m.onSubmit = apply
	m.notice = ""
	return m.form.Init()
}

func (m *Editor) edit(row editorRow) tea.Cmd {
	switch row.kind {
	case rowSettings:
		v := manifestValuesOf(m.d.Exam.Manifest)
		return m.open(v.form(), func() error {
			mf, err := v.build()
			if err != nil {
				return err
			}
			return m.d.SaveManifest(mf)
		})
	case rowPool:
		return m.openFile(exam.ManifestFile)
	}
	q := m.d.Exam.Questions[row.id]
	if q == nil {
		return nil
	}
	v := questionValuesOf(q, m.d.Exam.Key.Answers[row.id])
	return m.open(v.form(func(string) bool { return false }), func() error { return m.saveQuestion(v) })
}

func (m *Editor) newQuestion() tea.Cmd {
	v := newQuestionValues(m.d.NextQuestionID())
	taken := func(id string) bool { return m.d.Exam.Questions[id] != nil }
	return m.open(v.form(taken), func() error {
		if err := m.saveQuestion(v); err != nil {
			return err
		}
		for i, r := range m.rows() {
			if r.kind == rowQuestion && r.id == strings.TrimSpace(v.ID) {
				m.sel = i
			}
		}
		return nil
	})
}

func (m *Editor) saveQuestion(v *questionValues) error {
	q, a, err := v.build()
	if err != nil {
		return err
	}
	return m.d.SaveQuestion(q, a)
}

func (m *Editor) move(row editorRow, delta int) {
	if row.item < 0 {
		return
	}
	if err := m.d.MoveItem(row.item, delta); err != nil {
		m.say(err, "")
		return
	}
	// Follow the moved item.
	for i, r := range m.rows() {
		if r.item == row.item+delta && !r.inner {
			m.sel = i
			break
		}
	}
	m.notice = ""
}

// external opens the file behind row in the user's editor.
func (m *Editor) external(row editorRow) tea.Cmd {
	switch row.kind {
	case rowQuestion:
		if q := m.d.Exam.Questions[row.id]; q != nil && q.File != "" {
			return m.openFile(q.File)
		}
	}
	return m.openFile(exam.ManifestFile)
}

func (m *Editor) openFile(rel string) tea.Cmd {
	c := editorCommand(filepath.Join(m.d.Dir, filepath.FromSlash(rel)))
	return tea.ExecProcess(c, func(err error) tea.Msg { return editedMsg{err} })
}

// editorCommand runs $VISUAL or $EDITOR (which may carry arguments, such as
// "code -w"), or a sensible default for the system.
func editorCommand(path string) *exec.Cmd {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if strings.TrimSpace(ed) == "" {
		ed = "vi"
		if runtime.GOOS == "windows" {
			ed = "notepad"
		}
	}
	parts := strings.Fields(ed)
	return exec.Command(parts[0], append(parts[1:], path)...) //nolint:gosec // G204: the user's own editor
}

func (m *Editor) w() int { return max(72, min(m.width, 140)) }

// View draws the list, or the form in progress.
func (m *Editor) View() tea.View {
	w := m.w()
	var b strings.Builder
	man := m.d.Exam.Manifest
	b.WriteString(brandStyle.Render("ASSESS") + mutedStyle.Render("  ·  editor  ·  ") + man.Title +
		mutedStyle.Render("  ·  "+m.d.Dir) + "\n" + mutedStyle.Render(strings.Repeat("─", w)) + "\n")

	if m.form != nil {
		b.WriteString(m.form.View() + "\n\n")
		b.WriteString(mutedStyle.Render("esc descarta los cambios") + "\n")
		return m.finish(&b)
	}

	rows := m.rows()
	room := max(5, m.height-14)
	start := min(max(0, m.sel-room/2), max(0, len(rows)-room))
	end := min(len(rows), start+room)
	for i := start; i < end; i++ {
		b.WriteString(m.line(i, m.rowText(rows[i], w)) + "\n")
		if i == 0 {
			b.WriteString("\n" + headStyle.Render(fmt.Sprintf("  %-4s %-8s %-11s %6s   %s", "#", "PREG.", "TIPO", "PUNTOS", "ENUNCIADO")) + "\n")
		}
	}
	if len(rows) > room {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  … %d-%d de %d", start+1, end, len(rows))) + "\n")
	}
	b.WriteString(mutedStyle.Render(strings.Repeat("─", w)) + "\n")
	b.WriteString(m.problems(w))
	return m.finish(&b)
}

func (m *Editor) finish(b *strings.Builder) tea.View {
	b.WriteString(mutedStyle.Render(strings.Repeat("─", m.w())) + "\n")
	switch {
	case m.confirm != "":
		b.WriteString(accentStyle.Render(m.confirm) + "  " + keyStyle.Render("Y") + " sí  " + keyStyle.Render("N") + " no\n")
	case m.notice != "" && m.isErr:
		b.WriteString(accentStyle.Render(m.notice) + "\n")
	case m.notice != "":
		b.WriteString(m.notice + "\n")
	default:
		b.WriteString("\n")
	}
	if m.form == nil {
		k := func(key, label string) string { return keyStyle.Render(key) + " " + label }
		b.WriteString(strings.Join([]string{k("Enter", "editar"), k("N", "nueva"), k("D", "borrar"), k("⇧↑↓", "mover"),
			k("E", "abrir en el editor"), k("C", "clave"), k("S", "iniciar"), k("Q", "salir")}, "  "))
	}
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m *Editor) line(i int, s string) string {
	if i == m.sel {
		return selStyle.Render("▸ " + s)
	}
	return "  " + s
}

func (m *Editor) rowText(r editorRow, w int) string {
	man := m.d.Exam.Manifest
	switch r.kind {
	case rowSettings:
		shuffle := "sin barajar"
		switch {
		case man.ShuffleQuestions() && man.ShuffleOptions():
			shuffle = "baraja preguntas y opciones"
		case man.ShuffleQuestions():
			shuffle = "baraja preguntas"
		case man.ShuffleOptions():
			shuffle = "baraja opciones"
		}
		vis := map[string]string{"none": "sin nota visible", "score": "nota visible", "review": "nota y corrección"}[man.Visibility()]
		return fmt.Sprintf("⚙ Ajustes   %s · %s · %s · vigilancia %s · %s", exam.FormatDuration(man.Duration.D()), man.Lang(), vis,
			man.Profile(), shuffle)
	case rowPool:
		p := man.Items[r.item].Pool
		return fmt.Sprintf("%-4d ◇ pool %s · cada alumno recibe %d de %d", r.item+1, p.ID, p.Pick, len(p.From))
	}
	q := m.d.Exam.Questions[r.id]
	num := ""
	if r.item >= 0 && !r.inner {
		num = fmt.Sprint(r.item + 1)
	}
	if r.item < 0 {
		num = "—"
	}
	id := r.id
	if r.inner {
		id = "  " + id
	}
	if q == nil {
		return fmt.Sprintf("%-4s %-8s %s", num, id, accentStyle.Render("falta questions/"+r.id+".yaml"))
	}
	pts := "—"
	if a, ok := m.d.Exam.Key.Answers[r.id]; ok {
		pts = fmtNum(a.Points)
	}
	text := cut(plainPrompt(q.Prompt), max(20, w-40))
	if r.item < 0 {
		text = mutedStyle.Render("no está en items · ") + text
	}
	return fmt.Sprintf("%-4s %-8s %-11s %6s   %s", num, id, typeName(q.Type), pts, text)
}

// plainPrompt is the first meaningful line of a Markdown prompt.
func plainPrompt(md string) string {
	for l := range strings.SplitSeq(md, "\n") {
		l = strings.TrimSpace(strings.Trim(strings.TrimSpace(l), "#>*_`-"))
		if l != "" && !strings.HasPrefix(l, "![") && !strings.HasPrefix(l, "|") {
			return strings.NewReplacer("**", "", "__", "", "`", "").Replace(l)
		}
	}
	return ""
}

// problems summarises the validation of the folder.
func (m *Editor) problems(w int) string {
	ps := m.d.Problems
	errs, warns := ps.Count(exam.SeverityError), ps.Count(exam.SeverityWarning)
	if len(ps) == 0 {
		return lipgloss.NewStyle().Bold(true).Render("  ✓ Sin problemas") + mutedStyle.Render(": listo para iniciar (S, o `ax start "+m.d.Dir+"`).") + "\n" //nolint:misspell // Spanish
	}
	var b strings.Builder
	head := fmt.Sprintf("  %d %s · %d %s", errs, plural(errs, "error", "errores"), warns, plural(warns, "aviso", "avisos"))
	if errs > 0 {
		b.WriteString(accentStyle.Render("  ✕"+head[1:]) + mutedStyle.Render(" · se corrige al guardar el formulario o con E") + "\n")
	} else {
		b.WriteString(boldStyle.Render("  ⚠"+head[1:]) + "\n")
	}
	const shown = 6
	for i, p := range ps {
		if i == shown {
			b.WriteString(mutedStyle.Render(fmt.Sprintf("  … y %d más (`ax validate %s`)", len(ps)-shown, m.d.Dir)) + "\n")
			break
		}
		b.WriteString("  " + cut(p.String(), w-2) + "\n")
	}
	return b.String()
}

// formTheme is huh's base theme in the panel's colours.
func formTheme(isDark bool) *huh.Styles {
	t := huh.ThemeCharm(isDark)
	t.Focused.Title = t.Focused.Title.Foreground(accent)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(accent)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(accent)
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(accent)
	t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(accent)
	t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(accent)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Background(accent)
	t.Focused.Next = t.Focused.FocusedButton
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(accent)
	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()
	t.Group.Title = t.Focused.Title
	return t
}
