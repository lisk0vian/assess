package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/lisk0vian/assess/internal/exam"
)

const pastedText = `1. ¿Qué devuelve sorted()?
   a) None
   *b) Una lista nueva
   c) El mismo objeto

2. Un set admite duplicados.
   *F

3. ¿Qué imprime print(2 ** 3)?
   R: 8

4. ¿Qué método añade al final?
   a) add
   b) append
   c) push

¿Qué tipos son iterables?
- list *
- int
- str *

¿Capital de Francia?
Respuesta: París
R: Paris

Explica qué es un diccionario.
R: a, b`

func TestParsePasted(t *testing.T) {
	ps := parsePasted(pastedText)
	if len(ps) != 7 {
		for _, p := range ps {
			t.Logf("%s %q", p.v.Type, p.v.Prompt)
		}
		t.Fatalf("%d questions", len(ps))
	}
	single, tf, num, open, multi, text := ps[0], ps[1], ps[2], ps[3], ps[4], ps[5]
	if single.v.Type != exam.TypeSingle || single.v.Correct != 1 || single.missing || single.v.Prompt != "¿Qué devuelve sorted()?" ||
		single.v.Options != "None\nUna lista nueva\nEl mismo objeto" {
		t.Fatalf("single %+v", single.v)
	}
	if tf.v.Type != exam.TypeTrueFalse || tf.v.Value == nil || *tf.v.Value || tf.missing {
		t.Fatalf("true or false %+v", tf.v)
	}
	if num.v.Type != exam.TypeShort || num.v.Input != "number" || num.v.Number != "8" || num.v.Prompt != "¿Qué imprime print(2 ** 3)?" {
		t.Fatalf("number %+v", num.v)
	}
	if !open.missing || open.v.Correct != -1 {
		t.Fatal("no option marked: it comes in incomplete")
	}
	if multi.v.Type != exam.TypeMultiple || len(multi.v.CorrectM) != 2 || multi.v.Options != "list\nint\nstr" {
		t.Fatalf("multiple %+v", multi.v)
	}
	if text.v.Input != "text" || text.v.Accept != "París\nParis" || text.missing {
		t.Fatalf("text %+v", text.v)
	}

	// "R: b" marks options by letter.
	if ps := parsePasted("Elige\na) uno\nb) dos\nR: b"); ps[0].v.Correct != 1 {
		t.Fatalf("answer by letter %+v", ps[0].v)
	}
}

// Nothing is added until Enter; then every question is, incomplete ones too.
func TestExamPasteMany(t *testing.T) {
	m := newTestExam(t)
	drive(m, keys("Python", "tab", "v")...)
	drive(m, tea.PasteMsg{Content: pastedText})
	v := view(m)
	if len(m.d.Exam.Questions) != 0 || !strings.Contains(v, "LO QUE HE ENTENDIDO") || !strings.Contains(v, "7 preguntas · 1 por completar") {
		t.Fatalf("a preview first:\n%s", v)
	}
	drive(m, keys("enter")...)
	if m.pasting != nil || len(m.d.Exam.Manifest.Items) != 7 || !strings.Contains(m.notice, "Añadidas 7") {
		t.Fatalf("added %d: %q", len(m.d.Exam.Manifest.Items), m.notice)
	}
	first := m.d.Exam.Manifest.Items[0].Question
	if q := m.d.Exam.Questions[first]; q.Prompt != "¿Qué devuelve sorted()?" || m.d.Exam.Key.Answers[first].Option != q.Options[1].ID {
		t.Fatalf("first %+v", q)
	}
	if !m.incomplete(m.d.Exam.Manifest.Items[3].Question) {
		t.Fatal("an unmarked question is incomplete")
	}

	drive(m, keys("v", "Algo", "esc")...)
	if m.pasting != nil || len(m.d.Exam.Questions) != 7 {
		t.Fatal("Esc adds nothing")
	}
}
