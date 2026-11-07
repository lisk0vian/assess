package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/i18n"
)

func view(m tea.Model) string { return ansi.Strip(m.View().Content) }

// Every example question goes through the form unchanged.
func TestQuestionFormRoundTrip(t *testing.T) {
	for _, name := range []string{"algebra-01", "programacion-go", "historia-ventana", "ml-basics-en", "quimica-secundaria", "quiz-rapido"} {
		ex, probs, err := exam.Load(filepath.Join("..", "..", "examples", name))
		if err != nil || probs.HasErrors() {
			t.Fatalf("%s: %v %v", name, err, probs)
		}
		for id, q := range ex.Questions {
			a := ex.Key.Answers[id]
			got, gotA, err := questionValuesOf(q, a).build()
			if err != nil {
				t.Fatalf("%s/%s: %v", name, id, err)
			}
			want := *q
			want.File = ""
			if !reflect.DeepEqual(*got, want) || !reflect.DeepEqual(gotA, a) {
				t.Errorf("%s/%s changed:\n got %+v %+v\nwant %+v %+v", name, id, *got, gotA, want, a)
			}
		}
		m := ex.Manifest
		got, err := manifestValuesOf(m).build()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, m) {
			t.Errorf("%s manifest changed:\n got %+v\nwant %+v", name, got, m)
		}
	}
}

func TestManifestReadingSettingsRoundTrip(t *testing.T) {
	ex, _, err := exam.Load(filepath.Join("..", "..", "examples", "quimica-secundaria"))
	if err != nil {
		t.Fatal(err)
	}
	no := false
	m := ex.Manifest
	m.Proctoring.AutoRead, m.Proctoring.ReadSpeed = &no, "fast"
	v := manifestValuesOf(m)
	if v.AutoRead || v.ReadSpeed != "fast" {
		t.Fatalf("form values: %+v", v)
	}
	got, err := v.build()
	if err != nil {
		t.Fatal(err)
	}
	if p := got.Proctoring; p.AutoRead == nil || *p.AutoRead || p.ReadSpeed != "fast" {
		t.Fatalf("reading settings changed: %+v", p)
	}
}

func TestQuestionFormKeepsOptionIDs(t *testing.T) {
	q := &exam.Question{ID: "q1", Type: exam.TypeSingle, Prompt: "x", Options: []exam.Option{{ID: "a", Text: "uno"}, {ID: "z", Text: "dos", Pin: true}}}
	v := questionValuesOf(q, exam.Answer{Option: "z", Points: 1})
	v.Options = "uno\ndos\n\n  tres  \n"
	got, a, err := v.build()
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{got.Options[0].ID, got.Options[1].ID, got.Options[2].ID}
	if strings.Join(ids, ",") != "a,z,b" || !got.Options[1].Pin || got.Options[2].Text != "tres" || a.Option != "z" {
		t.Fatalf("options %+v answer %+v", got.Options, a)
	}
	v.Points = "2,5"
	if _, a, _ = v.build(); a.Points != 2.5 {
		t.Fatalf("decimal comma: %v", a.Points)
	}
}

// drive feeds msgs to m and runs the commands they return, dropping the
// ones that wait (cursor blinks and the like).
func drive(m tea.Model, msgs ...tea.Msg) {
	for _, msg := range msgs {
		_, cmd := m.Update(msg)
		runCmd(m, cmd)
	}
}

// runCmd runs cmd and feeds what it returns back to m, recursively.
func runCmd(m tea.Model, first tea.Cmd) {
	var run func(cmd tea.Cmd, depth int)
	feed := func(msg tea.Msg, depth int) {
		_, cmd := m.Update(msg)
		run(cmd, depth+1)
	}
	run = func(cmd tea.Cmd, depth int) {
		if cmd == nil || depth > 20 {
			return
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- cmd() }()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(30 * time.Millisecond):
			return
		}
		if msg == nil {
			return
		}
		if rv := reflect.ValueOf(msg); rv.Kind() == reflect.Slice && rv.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
			for i := range rv.Len() {
				run(rv.Index(i).Interface().(tea.Cmd), depth+1)
			}
			return
		}
		if _, ok := msg.(tea.QuitMsg); ok {
			return
		}
		feed(msg, depth)
	}
	run(first, 0)
}

func keys(s ...string) []tea.Msg {
	var out []tea.Msg
	for _, k := range s {
		switch k {
		case "enter":
			out = append(out, tea.KeyPressMsg{Code: tea.KeyEnter})
		case "ctrl+j":
			out = append(out, tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
		case "esc":
			out = append(out, tea.KeyPressMsg{Code: tea.KeyEscape})
		case "down":
			out = append(out, tea.KeyPressMsg{Code: tea.KeyDown})
		case "up":
			out = append(out, tea.KeyPressMsg{Code: tea.KeyUp})
		case "tab":
			out = append(out, tea.KeyPressMsg{Code: tea.KeyTab})
		case "left":
			out = append(out, tea.KeyPressMsg{Code: tea.KeyLeft})
		case "right":
			out = append(out, tea.KeyPressMsg{Code: tea.KeyRight})
		case "space":
			out = append(out, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
		case "shift+down":
			out = append(out, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
		case "shift+tab":
			out = append(out, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		case "backspace":
			out = append(out, tea.KeyPressMsg{Code: tea.KeyBackspace})
		default:
			for _, r := range k {
				out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
	}
	return out
}

func copyExam(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("..", "..", "examples", name)
	dst := filepath.Join(t.TempDir(), name)
	err := filepath.WalkDir(src, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if e.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

var testDefaults = ExamDefaults{Language: "es", Visibility: "score", Profile: "standard", ShuffleOptions: true}

func newTestExam(t *testing.T) *Editor {
	t.Helper()
	m := NewExam(testDefaults, "")
	m.base = t.TempDir()
	drive(m, tea.WindowSizeMsg{Width: 120, Height: 36})
	return m
}

func snapshot(dir string) map[string]string {
	out := map[string]string{}
	_ = filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			b, _ := os.ReadFile(p)
			out[p] = string(b)
		}
		return nil
	})
	return out
}

// A teacher creates and starts an exam from nothing, only with the keyboard.
func TestExamFromNothing(t *testing.T) {
	m := newTestExam(t)
	if v := view(m); !strings.Contains(v, "Nuevo examen") || !strings.Contains(v, "1 DATOS") || !strings.Contains(v, "ASÍ LO VE EL ALUMNO") {
		t.Fatalf("opens on the details:\n%s", v)
	}
	drive(m, keys("tab")...)
	if m.step != stepDetails || m.d != nil || !strings.Contains(view(m), "título") {
		t.Fatal("without a title there is no folder to save in")
	}
	drive(m, keys("Historia — Tema 3", "tab")...)
	if m.step != stepQuestions || m.Dir == "" || filepath.Base(m.Dir) != "historia-tema-3" {
		t.Fatalf("the first save creates the folder: step %d, dir %q, %q", m.step, m.Dir, m.notice)
	}
	if !strings.Contains(view(m), "Aún no hay preguntas") {
		t.Fatalf("an empty list says what to do:\n%s", view(m))
	}

	// One right option, marked with Space.
	drive(m, keys("n", "¿En qué año patentó Watt la máquina?", "tab", "1712", "enter", "1769", "enter", "1825", "up", "space", "esc")...)
	id := m.d.Exam.Manifest.Items[0].Question
	q, a := m.d.Exam.Questions[id], m.d.Exam.Key.Answers[id]
	if m.q != nil || len(q.Options) != 3 || q.Options[1].Text != "1769" || a.Option != q.Options[1].ID || a.Points != 1 {
		t.Fatalf("saved %+v %+v: %q", q, a, m.notice)
	}
	if v := view(m); !strings.Contains(v, "1769") || !strings.Contains(v, "correcta") || !strings.Contains(v, "✓ guardado") {
		t.Fatalf("the sheet shows the question as written:\n%s", v)
	}

	// True or false: the type is a chip, and the answer two halves.
	drive(m, keys("n", "La máquina impulsó el ferrocarril.", "up", "right", "right")...)
	if m.q.v.Type != exam.TypeTrueFalse {
		t.Fatalf("type %s", m.q.v.Type)
	}
	drive(m, keys("down", "down", "right", "esc")...)
	id = m.d.Exam.Manifest.Items[1].Question
	if a := m.d.Exam.Key.Answers[id]; a.Value == nil || !*a.Value {
		t.Fatalf("true or false: %+v, %q", a, m.notice)
	}

	// Shift+↓ moves the first one down.
	first := m.d.Exam.Manifest.Items[0].Question
	drive(m, keys("up", "shift+down")...)
	if m.d.Exam.Manifest.Items[1].Question != first {
		t.Fatal("shift+down should move the question")
	}

	// Rules, then review, and Enter starts it.
	drive(m, keys("tab", "down", "space", "tab")...)
	if m.d.Exam.Manifest.Profile() != "strict" || len(m.d.Exam.Manifest.Items) != 2 {
		t.Fatalf("rules saved over the questions: %+v", m.d.Exam.Manifest)
	}
	if v := view(m); m.step != stepReview || !strings.Contains(v, "INICIAR EXAMEN") || !strings.Contains(v, "Todo correcto") {
		t.Fatalf("review:\n%s", v)
	}
	drive(m, keys("enter")...)
	if !m.Start {
		t.Fatal("Enter starts a finished exam")
	}
	if d, err := exam.OpenDraft(m.Dir); err != nil || d.Problems.HasErrors() {
		t.Fatalf("exam on disk: %v %v", err, d.Problems)
	}
}

func TestExamEmptyWritesNothing(t *testing.T) {
	m := newTestExam(t)
	drive(m, keys("esc")...)
	if entries, _ := os.ReadDir(m.base); m.Dir != "" || len(entries) != 0 {
		t.Fatal("leaving an empty exam leaves nothing behind")
	}
}

func TestExamTitleIsSavedOnExit(t *testing.T) {
	m := newTestExam(t)
	drive(m, keys("Algo", "esc")...)
	if d, err := exam.OpenDraft(m.Dir); err != nil || d.Exam.Manifest.Title != "Algo" {
		t.Fatalf("nothing is lost: %q %v", m.Dir, err)
	}
	again := newTestExam(t)
	again.base = m.base
	drive(again, keys("Algo", "esc")...)
	if again.Dir == m.Dir {
		t.Fatal("a second exam with the same title gets its own folder")
	}
}

// Incomplete questions are allowed; they only stop the exam from starting.
func TestExamIncompleteQuestion(t *testing.T) {
	m := newTestExam(t)
	drive(m, keys("Examen", "tab", "n", "Sin respuesta", "tab", "Una", "enter", "Otra", "esc")...)
	if m.q != nil || m.isErr {
		t.Fatalf("Esc is done, never discard: %q", m.notice)
	}
	id := m.d.Exam.Manifest.Items[0].Question
	if !m.incomplete(id) {
		t.Fatalf("no right answer yet: %v", m.d.Problems)
	}
	if v := view(m); !strings.Contains(v, "··") || !strings.Contains(v, "!") {
		t.Fatalf("the rail marks it:\n%s", v)
	}
	drive(m, keys("tab", "tab")...)
	if v := view(m); !strings.Contains(v, "Primero resuelve") {
		t.Fatalf("the start block gives the reason:\n%s", v)
	}
	for range len(m.d.Problems) {
		if m.problemQuestion(m.d.Problems[m.sel]) == id {
			break
		}
		drive(m, keys("down")...)
	}
	drive(m, keys("enter")...)
	if m.Start || m.step != stepQuestions {
		t.Fatalf("Enter goes to the problem: step %d", m.step)
	}
}

// The free duration shows its field, takes bare minutes and survives a reload.
func TestExamOtherDuration(t *testing.T) {
	m := newTestExam(t)
	drive(m, keys("Examen", "down", "right", "right", "right")...)
	if m.durChoice != len(durationPresets) || !strings.Contains(view(m), "Minutos, o 1h15m") {
		t.Fatalf("Other shows where to type:\n%s", view(m))
	}
	drive(m, keys("50")...)
	if v := view(m); !strings.Contains(v, "50▌") || !strings.Contains(v, "50 minutos") {
		t.Fatalf("what is typed shows, in the field and in the student view:\n%s", v)
	}
	drive(m, keys("tab")...)
	if m.isErr || m.d.Exam.Manifest.Duration.D().Minutes() != 50 {
		t.Fatalf("duration %v: %q", m.d.Exam.Manifest.Duration, m.notice)
	}
	d, _ := exam.OpenDraft(m.Dir)
	again := NewEditor(d, "")
	again.step = stepDetails
	if again.durChoice != len(durationPresets) || again.other.value() != "50m" {
		t.Fatalf("reopened: %d %q", again.durChoice, again.other.value())
	}
}

// Review lists each problem whole, in the teacher's words.
func TestExamReviewTodo(t *testing.T) {
	m := newTestExam(t)
	drive(m, tea.WindowSizeMsg{Width: 150, Height: 36})
	drive(m, keys("Examen", "tab", "n", "Una pregunta larga sin respuesta marcada", "tab", "Sí", "enter", "No", "esc", "tab", "tab")...)
	v := view(m)
	for _, want := range []string{"Una pregunta larga sin respuesta marcada", "Marca la respuesta correcta.", "1 por resolver"} {
		if !strings.Contains(v, want) {
			t.Fatalf("review misses %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "answer-key.yaml") {
		t.Fatalf("no file paths for a missing answer:\n%s", v)
	}
}

func TestExamEditing(t *testing.T) {
	dir := copyExam(t, "quiz-rapido")
	d, _ := exam.OpenDraft(dir)
	m := NewEditor(d, "")
	drive(m, tea.WindowSizeMsg{Width: 110, Height: 40})
	if m.step != stepQuestions {
		t.Fatal("an existing exam opens on its questions")
	}
	before := len(d.Exam.Questions)

	// Details: change the title, the questions stay.
	drive(m, keys("shift+tab", " (v2)", "tab")...)
	if !strings.HasSuffix(d.Exam.Manifest.Title, "(v2)") || len(d.Exam.Questions) != before || m.isErr {
		t.Fatalf("title %q, %d questions: %q", d.Exam.Manifest.Title, len(d.Exam.Questions), m.notice)
	}

	// N then Esc without writing adds nothing.
	drive(m, keys("n", "esc")...)
	if len(d.Exam.Questions) != before {
		t.Fatal("an untouched new question is not saved")
	}

	// D asks, Y deletes.
	row, _ := m.selected()
	drive(m, keys("d")...)
	if !strings.Contains(view(m), "¿Borrar la pregunta "+row.id) {
		t.Fatal("delete should ask first")
	}
	drive(m, keys("y")...)
	if d.Exam.Questions[row.id] != nil {
		t.Fatal("the question should be gone")
	}

	// S refuses to start an exam with errors.
	if err := os.WriteFile(filepath.Join(dir, "questions", "zz.yaml"), []byte("id: zz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	drive(m, keys("r", "s")...)
	if m.Start || !strings.Contains(view(m), "corrige los errores") {
		t.Fatal("S must not start an exam with errors")
	}
}

func TestExamTypeChangeLeavesNoStaleFields(t *testing.T) {
	dir := copyExam(t, "quiz-rapido")
	d, _ := exam.OpenDraft(dir)
	var id string
	for _, q := range d.Exam.Questions {
		if q.Type == exam.TypeSingle {
			id = q.ID
		}
	}
	m := NewEditor(d, "")
	m.selectQuestion(id)
	drive(m, keys("enter", "up", "right", "right")...) // single → true or false
	drive(m, keys("down", "down", "right", "esc")...)
	got, probs, _ := exam.Load(dir)
	if q := got.Questions[id]; q.Type != exam.TypeTrueFalse || len(q.Options) != 0 {
		t.Fatalf("question %+v", q)
	}
	if probs.HasErrors() {
		t.Fatalf("problems: %v", probs)
	}
}

// Walking through every question and step leaves the files as they were.
func TestExamWalkingThroughChangesNothing(t *testing.T) {
	for _, name := range []string{"algebra-01", "programacion-go"} {
		dir := copyExam(t, name)
		d, _ := exam.OpenDraft(dir)
		m := NewEditor(d, "")
		drive(m, tea.WindowSizeMsg{Width: 110, Height: 40})
		before := snapshot(dir)
		for i, r := range m.rows() {
			if r.kind == rowPool { // opens the manifest in $EDITOR
				continue
			}
			m.sel = i
			drive(m, keys("enter")...)
			for range m.q.fields() {
				drive(m, keys("tab")...)
			}
			drive(m, keys("esc")...)
			if m.q != nil || m.isErr {
				t.Fatalf("%s/%s: %q", name, r.id, m.notice)
			}
		}
		drive(m, keys("shift+tab", "tab", "tab", "tab")...)
		for p, want := range snapshot(dir) {
			if before[p] != want {
				t.Errorf("%s changed:\n--- before\n%s\n--- after\n%s", p, before[p], want)
			}
		}
	}
}

// Every row fills the window, in both languages, at any size and step.
func TestExamFitsTheWindow(t *testing.T) {
	defer func() {
		if err := i18n.SetLanguage("es"); err != nil {
			t.Fatal(err)
		}
	}()
	for _, lang := range []string{"es", "en"} {
		if err := i18n.SetLanguage(lang); err != nil {
			t.Fatal(err)
		}
		for _, size := range [][2]int{{120, 36}, {80, 24}, {72, 16}, {160, 50}, {100, 30}} {
			d, _ := exam.OpenDraft(copyExam(t, "programacion-go"))
			m := NewEditor(d, "")
			check := func(what string) {
				t.Helper()
				lines := strings.Split(m.View().Content, "\n")
				if len(lines) != size[1] {
					t.Fatalf("%s %v %s: %d rows", lang, size, what, len(lines))
				}
				for i, l := range lines {
					if w := ansi.StringWidth(l); w != size[0] {
						t.Fatalf("%s %v %s: row %d is %d cells", lang, size, what, i, w)
					}
				}
			}
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for step := range stepCount {
				m.step = step
				check("step")
				for f := range detCount {
					m.field = f
					check("field")
				}
				m.durChoice, m.field = len(durationPresets), detDuration
				check("other duration")
				m.durChoice, m.field = 0, 0
			}
			m.step = stepQuestions
			for i := range m.rows() {
				m.sel = i
				check("reading")
				drive(m, keys("enter")...)
				if m.q != nil {
					for range m.q.fields() {
						check("writing")
						m.q.move(1)
					}
					m.q = nil
				}
			}
			drive(m, keys("?")...)
			check("help")
			drive(m, keys("esc", "d")...)
			check("confirm")
			m.confirm = ""
			m.notice, m.isErr = strings.Repeat("error ", 40), true
			check("notice")
		}
	}
}

// Hints and labels are for reading: they wrap, and nothing but the rail is cut.
func TestExamTextIsNeverCut(t *testing.T) {
	defer func() {
		if err := i18n.SetLanguage("es"); err != nil {
			t.Fatal(err)
		}
	}()
	for _, lang := range []string{"es", "en"} {
		if err := i18n.SetLanguage(lang); err != nil {
			t.Fatal(err)
		}
		for _, size := range [][2]int{{150, 40}, {120, 36}, {100, 30}, {80, 24}, {72, 16}} {
			d, _ := exam.OpenDraft(copyExam(t, "quiz-rapido"))
			m := NewEditor(d, "")
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			check := func(what string) {
				t.Helper()
				for _, l := range strings.Split(view(m), "\n") {
					if _, rest, rail := strings.Cut(l, "│"); rail {
						l = rest
					}
					if strings.Contains(l, "…") {
						t.Fatalf("%s %v %s cuts text:\n%s", lang, size, what, view(m))
					}
				}
			}
			m.step = stepDetails
			for f := range detCount {
				m.field = f
				check("details")
			}
			m.step = stepRules
			for _, f := range m.ruleRows() {
				m.field = f
				check("rules")
			}
			m.step = stepReview
			check("review")
			m.step, m.field = stepQuestions, 0
			for _, typ := range questionTypes {
				for _, input := range []string{"number", "text"} {
					m.q = newQuestionEdit(newQuestionValues("q900"), true)
					m.q.v.Type, m.q.v.Input = typ, input
					for _, f := range m.q.fields() {
						m.q.field = f
						check("writing " + typ)
					}
				}
			}
			m.q = nil
		}
	}
}

func TestTextField(t *testing.T) {
	f := newField("hola\nmundo")
	if f.vertical(1) || !f.vertical(-1) || f.pos != 4 {
		t.Fatalf("up keeps the column: %d", f.pos)
	}
	f.key(tea.KeyPressMsg{Code: tea.KeyBackspace}, true)
	f.insert("ñ")
	if f.value() != "holñ\nmundo" {
		t.Fatalf("%q", f.value())
	}
	if f.key(tea.KeyPressMsg{Code: tea.KeyEnter}, false) {
		t.Fatal("Enter leaves a one-line field")
	}
	wide := newField("ab日本")
	rows, row, col := wide.rows(4)
	if strings.Join(rows, "|") != "ab日|本" || row != 1 || col != 2 {
		t.Fatalf("wide glyphs: %v %d %d", rows, row, col)
	}
}
