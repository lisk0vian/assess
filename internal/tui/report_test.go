package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lisk0vian/assess/internal/report"
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
