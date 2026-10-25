package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lisk0vian/assess/internal/exam"
)

// Every example question goes through the form unchanged.
func TestQuestionFormRoundTrip(t *testing.T) {
	for _, name := range []string{"algebra-01", "programacion-go", "historia-ventana", "ml-basics-en", "quiz-rapido"} {
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

func TestEditorAddsAQuestion(t *testing.T) {
	dir := copyExam(t, "quiz-rapido")
	d, err := exam.OpenDraft(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := NewEditor(d)
	drive(m, tea.WindowSizeMsg{Width: 110, Height: 40})
	before := len(d.Exam.Questions)
	next := d.NextQuestionID()

	// N, keep "single" and the proposed id, write the prompt and the points,
	// two options, pick the second one, keep the defaults after that.
	drive(m, keys("n")...)
	if m.form == nil || !strings.Contains(m.View().Content, "Tipo de pregunta") {
		t.Fatalf("N should open the question form:\n%s", m.View().Content)
	}
	drive(m, keys("enter", "enter", "¿Capital de Francia?", "enter", "enter", "Roma", "ctrl+j", "París", "enter", "down", "enter", "enter", "enter")...)
	if m.form != nil {
		t.Fatalf("the form should be done:\n%s", m.View().Content)
	}
	if m.isErr || len(d.Exam.Questions) != before+1 {
		t.Fatalf("question not saved: %q, %d questions", m.notice, len(d.Exam.Questions))
	}
	q := d.Exam.Questions[next]
	a := d.Exam.Key.Answers[next]
	if q == nil || q.Prompt != "¿Capital de Francia?" || len(q.Options) != 2 || q.Options[1].Text != "París" || a.Option != q.Options[1].ID || a.Points != 1 {
		t.Fatalf("saved question %+v answer %+v", q, a)
	}
	if d.Problems.HasErrors() {
		t.Fatalf("problems: %v", d.Problems)
	}
	if !strings.Contains(m.View().Content, "¿Capital de Francia?") || m.selected().id != next {
		t.Fatal("the new question should be listed and selected")
	}

	// Esc in a form discards it.
	drive(m, keys("enter")...)
	drive(m, keys("esc")...)
	if m.form != nil || !strings.Contains(m.notice, "descartados") {
		t.Fatal("Esc should discard the form")
	}

	// D asks, Y deletes.
	drive(m, keys("d")...)
	if !strings.Contains(m.View().Content, "¿Borrar la pregunta "+next) {
		t.Fatal("delete should ask first")
	}
	drive(m, keys("y")...)
	if d.Exam.Questions[next] != nil {
		t.Fatal("the question should be gone")
	}
}

func TestEditorSettingsAndMove(t *testing.T) {
	dir := copyExam(t, "quiz-rapido")
	d, _ := exam.OpenDraft(dir)
	m := NewEditor(d)
	drive(m, tea.WindowSizeMsg{Width: 110, Height: 40})
	first := d.Exam.Manifest.Items[0].Question

	// Settings: change the title, keep everything else.
	drive(m, keys("enter")...)
	if !strings.Contains(m.View().Content, "Ajustes del examen") {
		t.Fatalf("enter on the first row edits the settings:\n%s", m.View().Content)
	}
	drive(m, keys(" (v2)", "enter", "enter", "enter", "enter", "enter", "enter", "enter", "enter", "enter")...)
	if m.form != nil || !strings.HasSuffix(d.Exam.Manifest.Title, "(v2)") || m.isErr {
		t.Fatalf("title not saved: %q %q\n%s", d.Exam.Manifest.Title, m.notice, m.View().Content)
	}

	// Shift+↓ on the first question moves it down.
	drive(m, keys("down", "shift+down")...)
	if d.Exam.Manifest.Items[1].Question != first || m.selected().id != first {
		t.Fatalf("move: items %+v, selected %+v", d.Exam.Manifest.Items, m.selected())
	}

	// S refuses to start an exam with errors.
	if err := os.WriteFile(filepath.Join(dir, "questions", "zz.yaml"), []byte("id: zz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	drive(m, keys("r", "s")...)
	if m.Start || !strings.Contains(m.View().Content, "corrige los errores") {
		t.Fatal("S must not start an exam with errors")
	}
}

// Opening each question and confirming every field leaves the files as
// they were: nothing is lost on the way through the form.
func TestEditorConfirmWithoutChanges(t *testing.T) {
	for _, name := range []string{"algebra-01", "programacion-go"} {
		dir := copyExam(t, name)
		d, _ := exam.OpenDraft(dir)
		m := NewEditor(d)
		drive(m, tea.WindowSizeMsg{Width: 110, Height: 40})
		snapshot := func() map[string]string {
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
		before := snapshot()
		for i, r := range m.rows() {
			if r.kind == rowPool { // opens the manifest in $EDITOR
				continue
			}
			m.sel = i
			drive(m, keys("enter")...)
			for n := 0; m.form != nil && n < 20; n++ {
				drive(m, keys("enter")...)
			}
			if m.form != nil || m.isErr {
				t.Fatalf("%s/%s: form stuck or failed: %q\n%s", name, r.id, m.notice, m.View().Content)
			}
		}
		for p, want := range snapshot() {
			if before[p] != want {
				t.Errorf("%s changed:\n--- before\n%s\n--- after\n%s", p, before[p], want)
			}
		}
	}
}
