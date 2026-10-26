package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lisk0vian/assess/internal/exam"
)

func view(m tea.Model) string { return ansi.Strip(m.View().Content) }

// A teacher creates an exam from nothing, only with the keyboard.
func TestWizardCreatesAnExam(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "historia-tema-3")
	m := NewWizard(testWizardDefaults)
	runCmd(m, m.Init())
	drive(m, tea.WindowSizeMsg{Width: 120, Height: 45})
	if v := view(m); !strings.Contains(v, "Nuevo examen") || !strings.Contains(v, "1 Datos") {
		t.Fatalf("the wizard should open on the details:\n%s", v)
	}

	// 1. Details: title, folder, keep 45 minutes, Spanish, score, default instructions.
	drive(m, keys("Historia — Tema 3", "enter", dir, "enter", "enter", "enter", "enter", "enter")...)
	if m.step != stepQuestions || m.form == nil || !strings.Contains(view(m), "Tipo de pregunta") {
		t.Fatalf("with no questions yet, the question form should open:\n%s", view(m))
	}

	// 2. A single-choice question: the second option is right.
	drive(m, keys("enter", "¿En qué año patentó Watt su máquina de vapor?", "enter", "enter",
		"1712", "ctrl+j", "1769", "ctrl+j", "1825", "enter", "down", "enter", "enter", "enter", "enter")...)
	v := view(m)
	if len(m.questions) != 1 || m.form != nil || !strings.Contains(v, "Pregunta 1 añadida") {
		t.Fatalf("question not added:\n%s", v)
	}
	if !strings.Contains(v, "ASÍ LO VERÁN") || !strings.Contains(v, "1769") || !strings.Contains(v, "✓") {
		t.Fatalf("the preview should show the question with its answer:\n%s", v)
	}

	// A true/false one, worth 2 points, true.
	drive(m, keys("a", "down", "down", "enter", "La máquina de vapor impulsó el ferrocarril.", "enter")...)
	drive(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	drive(m, keys("2", "enter", "left", "enter", "enter", "enter")...)
	if len(m.questions) != 2 || m.questions[1].q.Type != exam.TypeTrueFalse || m.questions[1].a.Points != 2 ||
		m.questions[1].a.Value == nil || !*m.questions[1].a.Value {
		t.Fatalf("second question: %+v %+v\n%s", m.questions[1].q, m.questions[1].a, view(m))
	}
	if !strings.Contains(view(m), "2 preguntas · 3 puntos") {
		t.Fatalf("the list should count questions and points:\n%s", view(m))
	}

	// Shift+↑ moves it first.
	drive(m, tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift})
	if m.questions[0].q.Type != exam.TypeTrueFalse || m.list.Index() != 0 {
		t.Fatal("shift+up should move the question up")
	}

	// 3. Proctoring: strict, keep shuffling and the lens, no stillness warning.
	drive(m, keys("tab")...)
	if m.step != stepProctoring || !strings.Contains(view(m), "Pantalla completa") {
		t.Fatalf("tab should go to proctoring, explained:\n%s", view(m))
	}
	drive(m, keys("down", "enter", "enter", "enter", "enter", "enter", "enter", "right", "enter")...)
	if m.step != stepReview || m.profile != "strict" {
		t.Fatalf("proctoring: step %d profile %s\n%s", m.step, m.profile, view(m))
	}

	// 4. Review: validated in a scratch folder, nothing written yet.
	m.Update(m.check()())
	v = view(m)
	for _, want := range []string{"Historia — Tema 3", "45 minutos", "2 (1 una opción, 1 V/F) · 3 puntos", "estricta", "lupa", "✓ Todo correcto"} {
		if !strings.Contains(v, want) {
			t.Fatalf("review missing %q:\n%s", want, v)
		}
	}
	if err := checkFolder(dir); err != nil {
		t.Fatal("nothing should be written before confirming")
	}
	m.busy = "Creando el examen…"
	_, cmd := m.Update(m.create()())
	runCmd(m, cmd)
	if m.step != stepDone || m.Dir != dir || !strings.Contains(view(m), "Examen creado") {
		t.Fatalf("not created: %q\n%s", m.notice, view(m))
	}
	drive(m, keys("enter")...)
	if m.Next != NextStart {
		t.Fatalf("next = %q", m.Next)
	}

	d, err := exam.OpenDraft(dir)
	if err != nil || len(d.Problems) != 0 {
		t.Fatalf("created exam: %v %v", err, d.Problems)
	}
	ex := d.Exam
	q1, q2 := ex.Questions["q001"], ex.Questions["q002"]
	if ex.Manifest.ID != "historia-tema-3" || ex.Manifest.Profile() != "strict" || q1.Type != exam.TypeTrueFalse ||
		q2.Options[1].Text != "1769" || ex.Key.Answers["q002"].Option != q2.Options[1].ID || !strings.Contains(ex.Manifest.Instructions, "Lee cada pregunta") {
		t.Fatalf("exam on disk: %+v\n%+v %+v\n%+v", ex.Manifest, q1, q2, ex.Key)
	}
	if p := ex.Manifest.Proctoring; p.Lens != nil || p.FrozenPointer == nil || *p.FrozenPointer ||
		p.AutoRead != nil || p.ReadSpeed != "" {
		t.Fatalf("only the stillness warning should be written, switched off: %+v", p)
	}
}

func TestWizardAsksBeforeLosingWork(t *testing.T) {
	m := NewWizard(testWizardDefaults)
	runCmd(m, m.Init())
	drive(m, keys("Algo", "esc")...)
	if !strings.Contains(view(m), "¿Salir sin crear el examen?") {
		t.Fatalf("esc with something written should ask:\n%s", view(m))
	}
	drive(m, keys("n")...)
	if m.form == nil || m.title != "Algo" {
		t.Fatal("answering no should go back to the form, keeping the title")
	}
}

func TestWizardFolderChecks(t *testing.T) {
	if err := checkFolder(filepath.Join("..", "..", "examples", "algebra-01")); err == nil {
		t.Fatal("an existing exam folder must be refused")
	}
	if err := checkFolder(filepath.Join(t.TempDir(), "!!!")); err == nil {
		t.Fatal("a folder name without letters must be refused")
	}
	if got := defaultFolder("Álgebra 1: Ecuaciones"); got != "."+string(filepath.Separator)+"algebra-1-ecuaciones" {
		t.Fatalf("default folder %q", got)
	}
	if humanDuration("1h30m") != "1 hora y 30 minutos" || humanDuration("45m") != "45 minutos" {
		t.Fatal("durations should read naturally")
	}
}
