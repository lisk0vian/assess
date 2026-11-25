package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/i18n"
	"github.com/lisk0vian/assess/internal/report"
	"github.com/lisk0vian/assess/internal/session"
)

func sampleReport() *report.Report {
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.Local)
	p := func(v float64) *float64 { return &v }
	disc := -0.3
	r := &report.Report{
		Session: report.SessionInfo{Code: "K7Q2XM", ExamTitle: "Álgebra 1", StartedAt: t0, EndedAt: t0.Add(40 * time.Minute), DurationMin: 40},
		Summary: report.Summary{Students: 3, Submitted: 2, Annulled: 1, Mean: 62.5, Median: 62.5, StdDev: 12.5, Min: 50, Max: 75, MedianMinutes: 31},
		Questions: []report.QuestionStat{
			{ID: "q001", Type: "single", Prompt: "Resuelve 3x + 7 = 22", Points: 1, Seen: 2, Answered: 2, Correct: 2, Rate: 1, Leak: true},
			{ID: "q002", Type: "short", Prompt: "Valor de x", Points: 2, Seen: 2, Answered: 1, Correct: 1, Rate: 0.5, Discrimination: &disc, AwayHits: 1},
		},
		Incidents: report.Incidents{
			ByKind: []report.KindCount{{Kind: "hidden", Warn: 3, High: 1}, {Kind: "copy", High: 2}},
			Step:   2, Strikes: 3, Reverted: 1, TimeLostMin: 4.5,
		},
		Students: []report.StudentLine{
			{Who: report.Who{Name: "Ana Pérez", StudentID: "A1"}, State: "submitted", Percent: p(75), Pairs: 1},
			{Who: report.Who{Name: "Bea Ruiz", StudentID: "B2"}, State: "submitted", Percent: p(50), Pairs: 1, AwayHits: 1, Strikes: 2},
			{Who: report.Who{Name: "Carlos", StudentID: "C3"}, State: "annulled"},
		},
		Pairs: []report.Pair{{Students: [2]report.Who{{Name: "Ana Pérez"}, {Name: "Bea Ruiz"}}, Level: report.LevelHigh,
			Common: 10, BothWrong: 8, SameWrong: 8, Expected: 2.7, P: 0.00016, Synced: 6, Questions: []string{"q002", "q003"}}},
		AwayHits: []report.AwayHit{{Who: report.Who{Name: "Bea Ruiz"}, QuestionID: "q002", Signal: "hidden", AwayMs: 40000, AfterMs: 9000, Difficulty: 0.8, Strong: true}},
		History: []report.SessionLine{
			{Code: "OLD111", StartedAt: t0.Add(-7 * 24 * time.Hour), Students: 20, Submitted: 20, Mean: 55, Median: 52},
			{Code: "K7Q2XM", StartedAt: t0, Students: 3, Submitted: 2, Mean: 62.5, Median: 62.5, Current: true},
		},
		Leaks: []report.Leak{{QuestionID: "q001", Prompt: "Resuelve 3x + 7 = 22", Before: 0.2, BeforeN: 20, Now: 1, NowN: 2, Z: 3}},
	}
	for i := range 20 {
		r.Incidents.Timeline = append(r.Incidents.Timeline, report.TimeSlot{Minute: i * 2, Warn: i % 3, High: i % 5 / 4})
	}
	for i := range 10 {
		r.Scores = append(r.Scores, report.Bin{From: float64(i * 10), To: float64(i*10 + 10)})
	}
	r.Scores[5].Count, r.Scores[7].Count = 1, 1
	for i := range 10 {
		r.Times = append(r.Times, report.Bin{From: float64(i * 4), To: float64(i*4 + 4)})
	}
	r.Times[7].Count = 2
	return r
}

func TestReportText(t *testing.T) {
	out := ReportText(sampleReport(), 110)
	if path := os.Getenv("AX_TUI_REPORT_OUT"); path != "" {
		_ = os.WriteFile(path, []byte(out), 0o600)
	}
	for _, want := range []string{
		"Álgebra 1", "K7Q2XM", "3 alumnos", "2 entregados", "1 anulado", "Media 63 %", "Desviación 12,5",
		"Bea Ruiz", "2 strikes · 1 pareja parecida · salió y acertó 1 vez",
		"q001", "mucho más fácil que en sesiones anteriores", "la aciertan más quienes peor van", "1 acierto tras salir del examen",
		"3 avisos · 3 señales graves · 3 strikes activos · 1 penalización revertida · 4,5 min restados", "fuera de la pestaña", "0'",
		"ALTO", "Ana Pérez ↔ Bea Ruiz", "8 de 8 errores en común son idénticos (por azar, ~2,7; probabilidad 1 entre 6.250)",
		"6 respuestas guardadas a la vez", "errores idénticos en: q002 q003",
		"fuera de la pestaña 40 s → acertó 9 s después · difícil: la falló el 80 % del resto",
		"OLD111", "esta sesión", "antes 20 % (20) → ahora 100 % (2)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report text missing %q:\n%s", want, out)
		}
	}
}

func TestReportKeys(t *testing.T) {
	m := NewReport(sampleReport(), time.Minute)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if !strings.Contains(m.View().Content, "NOTAS") {
		t.Fatal("the report should open on the summary")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.view != repQuestions || !strings.Contains(m.View().Content, "% DE ACIERTOS") {
		t.Fatal("→ should show the questions")
	}
	m.Update(tea.KeyPressMsg{Code: '4', Text: "4"})
	if m.view != repEvidence || !strings.Contains(m.View().Content, "RESPUESTAS PARECIDAS") {
		t.Fatal("4 should jump to the evidence")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.view != repHistory {
		t.Fatalf("← should wrap around, got view %d", m.view)
	}

	// The summary is taller than 20 rows: it scrolls, and says so.
	m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	if !strings.Contains(m.View().Content, "desplazar") {
		t.Fatal("a long view should offer scrolling")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if !strings.Contains(m.View().Content, "PARA REVISAR") {
		t.Fatal("End should reach the bottom of the view")
	}

	// Idle: the report closes itself.
	m.lastInput = time.Now().Add(-2 * time.Minute)
	if _, cmd := m.Update(reportTick(time.Now())); cmd == nil || !m.TimedOut {
		t.Fatal("an idle report should close")
	}
}

func TestReportCursorAndSort(t *testing.T) {
	m := NewReport(sampleReport(), 0)
	m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.sel != 1 {
		t.Fatalf("↓ moves through the questions: %d", m.sel)
	}
	if v := m.View().Content; !strings.Contains(v, "Valor de x") {
		t.Fatalf("the selected question shows its prompt:\n%s", v)
	}
	m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if m.orderedQuestions()[0].ID != "q002" {
		t.Fatal("O sorts the hardest first")
	}
}

// Every row fills the window, in both languages, at any size.
func TestReportFitsTheWindow(t *testing.T) {
	defer func() {
		if err := i18n.SetLanguage("es"); err != nil {
			t.Fatal(err)
		}
	}()
	for _, lang := range []string{"es", "en"} {
		if err := i18n.SetLanguage(lang); err != nil {
			t.Fatal(err)
		}
		for _, size := range [][2]int{{120, 36}, {80, 24}, {72, 16}, {160, 50}} {
			for view := range repCount {
				m := NewReport(sampleReport(), 0)
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.view = view
				lines := strings.Split(m.View().Content, "\n")
				if len(lines) != size[1] {
					t.Fatalf("%s %v view %d: %d rows", lang, size, view, len(lines))
				}
				for i, l := range lines {
					if w := ansi.StringWidth(l); w != size[0] {
						t.Fatalf("%s %v view %d: row %d is %d cells", lang, size, view, i, w)
					}
				}
			}
		}
	}
}

func TestReportFindingsOnlyStateWhatTheDataBacks(t *testing.T) {
	r := sampleReport()
	r.Pairs, r.History = nil, nil
	for i := range r.Questions {
		r.Questions[i].Rate = 0.9
	}
	if f := NewReport(r, 0).findings(); len(f) != 0 {
		t.Fatalf("nothing to flag, got %v", f)
	}
	if f := NewReport(sampleReport(), 0).findings(); len(f) == 0 {
		t.Fatal("a similar pair and a higher mean should be flagged")
	}
}

func TestReportIncidentsListWhoHasStrikes(t *testing.T) {
	m := NewReport(sampleReport(), 0)
	m.view = repIncidents
	if v := m.View().Content; !strings.Contains(v, "Bea Ruiz") {
		t.Fatalf("a student with strikes and no signals must be listed:\n%s", v)
	}
}

func stubFixer(applied *[]session.Correction) *Fixer {
	var done []session.Correction
	q := &exam.Question{ID: "q001", Type: exam.TypeSingle, Prompt: "¿Cuánto es 2 + 2?",
		Options: []exam.Option{{ID: "a", Text: "3"}, {ID: "b", Text: "4"}, {ID: "c", Text: "5"}}}
	return &Fixer{
		Question:    func(_ string) *exam.Question { return q },
		Corrections: func() []session.Correction { return done },
		Preview: func(_ session.Correction) ([]session.ScoreChange, error) {
			return []session.ScoreChange{{Name: "Luis Gómez", Before: 50, After: 62.5}}, nil
		},
		Apply: func(c session.Correction) (*report.Report, error) {
			c.ID = int64(len(done) + 1)
			done = append(done, c)
			*applied = append(*applied, c)
			return sampleReport(), nil
		},
		Undo: func(_ int64) (*report.Report, error) {
			done = done[:len(done)-1]
			return sampleReport(), nil
		},
	}
}

func TestReportCorrectsAQuestion(t *testing.T) {
	var applied []session.Correction
	m := NewReport(sampleReport(), 0)
	m.Fix = stubFixer(&applied)
	m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.fixing == nil {
		t.Fatal("Enter on a question opens the correction")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	v := view(m)
	for _, want := range []string{"Corregir", "TAMBIÉN VALE", "Luis Gómez", "50 % → 63 %"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(applied) != 1 || applied[0].Kind != session.CorrectAccept || applied[0].Value != "b" || m.fixing != nil {
		t.Fatalf("applied %+v", applied)
	}
	if v := view(m); !strings.Contains(v, "Corregido") || !strings.Contains(v, "también vale B") {
		t.Fatalf("the report says what changed:\n%s", v)
	}
	// Void, then undo the last one.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(applied) != 2 || applied[1].Kind != session.CorrectVoid {
		t.Fatalf("void %+v", applied)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: 'U', Text: "U"})
	if len(m.Fix.Corrections()) != 1 {
		t.Fatal("U undoes the last correction")
	}
	for _, size := range [][2]int{{120, 36}, {80, 24}, {72, 16}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		lines := strings.Split(m.View().Content, "\n")
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != size[0] || len(lines) != size[1] {
				t.Fatalf("%v: row %d is %d cells, %d rows", size, i, w, len(lines))
			}
		}
	}
}
