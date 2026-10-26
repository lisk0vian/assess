package exam

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// copyExampleNamed copies an example exam into a temporary folder.
func copyExampleNamed(t *testing.T, name string) string {
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
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func openDraft(t *testing.T, dir string) *Draft {
	t.Helper()
	d, err := OpenDraft(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Problems.HasErrors() {
		t.Fatalf("draft has errors: %v", d.Problems)
	}
	return d
}

func TestDraftSaveWithoutChangesKeepsFiles(t *testing.T) {
	for _, name := range []string{"algebra-01", "programacion-go", "historia-ventana", "ml-basics-en", "quimica-secundaria", "quiz-rapido"} {
		t.Run(name, func(t *testing.T) {
			dir := copyExampleNamed(t, name)
			d := openDraft(t, dir)
			before := map[string]string{ManifestFile: read(t, dir, ManifestFile), AnswerKeyFile: read(t, dir, AnswerKeyFile)}
			for _, q := range d.Exam.Questions {
				before[q.File] = read(t, dir, q.File)
			}
			if err := d.SaveManifest(d.Exam.Manifest); err != nil {
				t.Fatal(err)
			}
			for _, id := range d.QuestionOrder() {
				if err := d.SaveQuestion(d.Exam.Questions[id], d.Exam.Key.Answers[id]); err != nil {
					t.Fatal(err)
				}
			}
			for rel, want := range before {
				if got := read(t, dir, rel); got != want {
					t.Errorf("%s changed:\n--- before\n%s\n--- after\n%s", rel, want, got)
				}
			}
		})
	}
}

func TestDraftEdits(t *testing.T) {
	dir := copyExampleNamed(t, "algebra-01")
	d := openDraft(t, dir)

	// A title change touches only that line.
	m := d.Exam.Manifest
	before := read(t, dir, ManifestFile)
	m.Title = "Álgebra 1 — Repaso"
	if err := d.SaveManifest(m); err != nil {
		t.Fatal(err)
	}
	after := read(t, dir, ManifestFile)
	if want := strings.Replace(before, "Ecuaciones lineales", "Repaso", 1); after != want {
		t.Fatalf("manifest:\n%s\nwant:\n%s", after, want)
	}

	// Switching one photo protection off writes only that key.
	off := false
	m = d.Exam.Manifest
	m.Proctoring.Lens = &off
	if err := d.SaveManifest(m); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, ManifestFile); !strings.Contains(got, "  lens: false\n") || strings.Contains(got, "frozen_pointer:") {
		t.Fatalf("manifest with the lens off:\n%s", got)
	}
	if p := openDraft(t, dir).Exam.Manifest.Proctoring; p.Lens == nil || *p.Lens || p.FrozenPointer != nil {
		t.Fatalf("read back: %+v", p)
	}

	// The reading settings are written and read back too.
	noAuto := false
	m = d.Exam.Manifest
	m.Proctoring.AutoRead, m.Proctoring.ReadSpeed = &noAuto, "fast"
	if err := d.SaveManifest(m); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, ManifestFile); !strings.Contains(got, "  auto_read: false\n") || !strings.Contains(got, "  read_speed: fast\n") {
		t.Fatalf("manifest with reading settings:\n%s", got)
	}
	if p := openDraft(t, dir).Exam.Manifest.Proctoring; p.AutoRead == nil || *p.AutoRead || p.ReadSpeed != "fast" {
		t.Fatalf("reading settings read back: %+v", p)
	}

	// A new question: file, answer and item.
	id := d.NextQuestionID()
	if id != "q008" {
		t.Fatalf("next id = %q", id)
	}
	q := &Question{ID: id, Type: TypeSingle, Prompt: "¿Cuánto es $2 + 2$?\n\nElige una.", Options: []Option{{ID: "a", Text: "4"}, {ID: "b", Text: "true"}}}
	if err := d.SaveQuestion(q, Answer{Option: "a", Points: 1.5}); err != nil {
		t.Fatal(err)
	}
	if d.Problems.HasErrors() {
		t.Fatalf("problems after adding: %v", d.Problems)
	}
	got := d.Exam.Questions[id]
	if got == nil || got.Options[0].Text != "4" || got.Options[1].Text != "true" || got.Prompt != "¿Cuánto es $2 + 2$?\n\nElige una.\n" {
		t.Fatalf("question read back: %+v", got)
	}
	if d.Exam.Key.Answers[id].Points != 1.5 || d.Exam.Manifest.Items[len(d.Exam.Manifest.Items)-1].Question != id {
		t.Fatal("answer or item missing")
	}
	if !strings.Contains(read(t, dir, "questions/q008.yaml"), "prompt: |\n  ¿Cuánto es $2 + 2$?\n\n  Elige una.\n") {
		t.Fatalf("prompt should be a literal block:\n%s", read(t, dir, "questions/q008.yaml"))
	}
	if !strings.HasPrefix(read(t, dir, AnswerKeyFile), "# Answer key") {
		t.Fatal("the key lost its comment")
	}

	// Deleting a pooled question shrinks the pool.
	if err := d.DeleteQuestion("q005"); err != nil {
		t.Fatal(err)
	}
	pool := d.Exam.Manifest.Items[3].Pool
	if pool == nil || strings.Join(pool.From, ",") != "q004,q007" || pool.Pick != 1 {
		t.Fatalf("pool after delete: %+v", pool)
	}
	if _, err := os.Stat(filepath.Join(dir, "questions", "q005.yaml")); !os.IsNotExist(err) {
		t.Fatal("the question file should be gone")
	}
	if _, ok := d.Exam.Key.Answers["q005"]; ok || d.Problems.HasErrors() {
		t.Fatalf("key still has q005 or problems: %v", d.Problems)
	}

	// Moving an item.
	if err := d.MoveItem(0, 1); err != nil {
		t.Fatal(err)
	}
	if d.Exam.Manifest.Items[0].Question != "q002" || d.Exam.Manifest.Items[1].Question != "q001" {
		t.Fatalf("items after move: %+v", d.Exam.Manifest.Items[:2])
	}
	if !strings.Contains(read(t, dir, ManifestFile), "\n\nitems:\n") {
		t.Fatal("blank line before items was lost")
	}
}

func TestDraftRefusesBrokenFile(t *testing.T) {
	dir := copyExampleNamed(t, "algebra-01")
	d := openDraft(t, dir)
	if err := os.WriteFile(filepath.Join(dir, AnswerKeyFile), []byte("answers: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := d.Exam.Questions["q001"]
	if err := d.SaveQuestion(q, Answer{Option: "b", Points: 1}); err == nil || !strings.Contains(err.Error(), "syntax") {
		t.Fatalf("want a broken-file error, got %v", err)
	}
}

func TestFormatDuration(t *testing.T) {
	for in, want := range map[string]string{"45m": "45m", "1h30m": "1h30m", "90s": "1m30s", "2h": "2h", "30s": "30s"} {
		d, _ := parseDur(in)
		if got := FormatDuration(d); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
}

func parseDur(s string) (d time.Duration, err error) { return time.ParseDuration(s) }

func TestCreate(t *testing.T) {
	src := openDraft(t, copyExampleNamed(t, "algebra-01")).Exam
	dir := filepath.Join(t.TempDir(), "algebra-01")
	if err := Create(dir, src); err != nil {
		t.Fatal(err)
	}
	d := openDraft(t, dir)
	if len(d.Exam.Questions) != len(src.Questions) || d.Exam.Manifest.Title != src.Manifest.Title {
		t.Fatalf("created exam differs: %+v", d.Exam.Manifest)
	}
	if !strings.HasPrefix(read(t, dir, ManifestFile), "# Assess exam manifest") || !strings.HasPrefix(read(t, dir, AnswerKeyFile), "# Answer key") {
		t.Fatal("files should start with their comment")
	}
	if err := Create(dir, src); err == nil {
		t.Fatal("Create must refuse a folder that is not empty")
	}
}
