package report

import (
	"fmt"
	"testing"
	"time"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/grading"
	"github.com/lisk0vian/assess/internal/session"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

const numQ = 10

// testExam has numQ single-choice questions; "a" is always right.
func testExam() *exam.Published {
	ex := &exam.Exam{Manifest: exam.Manifest{ID: "demo", Title: "Demo"}, Questions: map[string]*exam.Question{},
		Key: exam.AnswerKey{Answers: map[string]exam.Answer{}}}
	for i := 1; i <= numQ; i++ {
		id := fmt.Sprintf("q%d", i)
		ex.Questions[id] = &exam.Question{ID: id, Type: exam.TypeSingle, Prompt: "## Pregunta **" + id + "**\n\nTexto",
			Options: []exam.Option{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}}
		ex.Key.Answers[id] = exam.Answer{Points: 1, Option: "a"}
	}
	return &exam.Published{Hash: "h", Exam: ex}
}

// builder assembles an archive of submitted attempts.
type builder struct {
	arc *session.Archive
	n   int
}

func newBuilder(id string) *builder {
	sess := &session.Session{ID: id, Code: "ABC", State: session.StateEnded, Duration: 30 * time.Minute, StartedAt: t0, EndedAt: t0.Add(30 * time.Minute)}
	return &builder{arc: &session.Archive{Snapshot: &session.Snapshot{Session: sess, Participants: map[string]*session.Participant{},
		Answers: map[string][]*session.Answer{}, Penalties: map[string][]*session.Penalty{}, SignalCounts: map[string]int{}},
		Published: testExam()}}
}

// add registers a student whose answers are given as options for q1..q10
// ("-" leaves it blank); answer i is saved at minute i, or at the given time.
func (b *builder) add(name string, opts string, reversed bool, at map[string]time.Time) *session.Attempt {
	b.n++
	pid, aid := fmt.Sprintf("p%d", b.n), fmt.Sprintf("a%d", b.n)
	b.arc.Participants[pid] = &session.Participant{ID: pid, StudentID: name, Name: name}
	att := &session.Attempt{ID: aid, ParticipantID: pid, State: session.AttemptSubmitted, CreatedAt: t0, MaxScore: numQ, SubmittedAt: t0.Add(20 * time.Minute)}
	for i := 1; i <= numQ; i++ {
		att.Layout = append(att.Layout, exam.Slot{QuestionID: fmt.Sprintf("q%d", i)})
	}
	if reversed {
		for i, j := 0, len(att.Layout)-1; i < j; i, j = i+1, j-1 {
			att.Layout[i], att.Layout[j] = att.Layout[j], att.Layout[i]
		}
	}
	for i, c := range opts {
		q := fmt.Sprintf("q%d", i+1)
		if c == '-' {
			continue
		}
		when := t0.Add(time.Duration(i+1) * time.Minute)
		if t, ok := at[q]; ok {
			when = t
		}
		b.arc.Answers[aid] = append(b.arc.Answers[aid], &session.Answer{AttemptID: aid, QuestionID: q,
			Response: grading.Response{Option: string(c)}, UpdatedAt: when})
		if c == 'a' {
			att.Score++
		}
	}
	b.arc.Attempts = append(b.arc.Attempts, att)
	return att
}

func TestCollusionAwayHitsAndLeaks(t *testing.T) {
	b := newBuilder("now")
	// Ana and Bea share eight rare wrong answers ("d") and saved every answer
	// at the same moment, although their questions came in different orders.
	b.add("Ana", "adddddddda", false, nil)
	b.add("Bea", "adddddddda", true, nil)
	// Everyone else gets q1 right and spreads wrong answers over b and c;
	// q10 is hard: only Ana, Bea and Carla get it.
	carla := b.add("Carla", "acbbcbccba", false, map[string]time.Time{"q10": t0.Add(10*time.Minute + 10*time.Second)})
	rest := []string{"abcbcbcbcb", "acbcbcbcbc", "abbccbbccb", "acccbbcccb", "abcbbcbcbc", "acbbccbbcc", "abccbcabcb", "acbcbbcbcb", "abbbccbbcc"}
	for i, o := range rest {
		b.add(fmt.Sprintf("Z%02d", i), o, false, nil)
	}
	// Carla left on q10 for 40 s and answered it right 10 s after coming back.
	b.arc.Events = append(b.arc.Events, &session.Event{AttemptID: carla.ID, Kind: "signal.hidden", Severity: session.SeverityWarn,
		QuestionID: "q10", Data: map[string]any{"duration_ms": float64(40000)}, ReceivedAt: t0.Add(10 * time.Minute)})

	// Earlier sessions: q1 was hard (2 of 12 right).
	old := newBuilder("before")
	for i := range 12 {
		o := "bbbbbbbbbb"
		if i < 2 {
			o = "abbbbbbbbb"
		}
		old.add(fmt.Sprintf("Y%02d", i), o, false, nil)
	}
	old.arc.Session.StartedAt = t0.Add(-7 * 24 * time.Hour)

	r := Build(b.arc, []*session.Archive{old.arc})

	if r.Summary.Students != 12 || r.Summary.Submitted != 12 {
		t.Fatalf("summary: %+v", r.Summary)
	}
	total := 0
	for _, bin := range r.Scores {
		total += bin.Count
	}
	if total != 12 || len(r.Scores) != 10 || r.Times[6].Count != 12 {
		t.Fatalf("histograms: %+v %+v", r.Scores, r.Times)
	}

	if len(r.Pairs) != 1 {
		t.Fatalf("want only Ana and Bea flagged, got %+v", r.Pairs)
	}
	p := r.Pairs[0]
	if p.Students[0].Name != "Ana" || p.Students[1].Name != "Bea" || p.Level != LevelHigh || p.SameWrong != 8 || p.Synced != 10 {
		t.Fatalf("pair: %+v", p)
	}

	if len(r.AwayHits) != 1 {
		t.Fatalf("away hits: %+v", r.AwayHits)
	}
	h := r.AwayHits[0]
	if h.Name != "Carla" || h.QuestionID != "q10" || h.AwayMs != 40000 || h.AfterMs != 10000 || !h.Strong {
		t.Fatalf("away hit: %+v", h)
	}

	if len(r.Leaks) != 1 || r.Leaks[0].QuestionID != "q1" || r.Leaks[0].Before != 0.17 || r.Leaks[0].Now != 1 {
		t.Fatalf("leaks: %+v", r.Leaks)
	}
	q1 := question(r, "q1")
	if q1.ID != "q1" || !q1.Leak || q1.Rate != 1 || q1.Prompt != "Pregunta q1" || q1.Discrimination != nil {
		t.Fatalf("q1: %+v", q1)
	}
	// Only the weakest students got q10: it discriminates backwards.
	if d := question(r, "q10").Discrimination; d == nil || *d >= 0 {
		t.Fatalf("q10 should discriminate negatively, got %v", d)
	}
	if len(r.History) != 2 || !r.History[1].Current || r.History[0].Submitted != 12 {
		t.Fatalf("history: %+v", r.History)
	}
	var ana StudentLine
	for _, s := range r.Students {
		if s.Name == "Ana" {
			ana = s
		}
	}
	if ana.Pairs != 1 || ana.Percent == nil || *ana.Percent != 20 {
		t.Fatalf("Ana: %+v", ana)
	}
}

// A common distractor shared by many is not evidence.
func TestPopularWrongAnswerIsNotCollusion(t *testing.T) {
	b := newBuilder("s")
	for i := range 10 {
		b.add(fmt.Sprintf("S%d", i), "abbbbbbbba", false, nil)
	}
	if r := Build(b.arc, nil); len(r.Pairs) != 0 {
		t.Fatalf("no pair should be flagged: %+v", r.Pairs)
	}
}

func TestIncidentTimeline(t *testing.T) {
	b := newBuilder("s")
	a := b.add("Ana", "aaaaaaaaaa", false, nil)
	for _, m := range []int{1, 2, 25} {
		b.arc.Events = append(b.arc.Events, &session.Event{AttemptID: a.ID, Kind: "signal.copy", Severity: session.SeverityHigh, ReceivedAt: t0.Add(time.Duration(m) * time.Minute)})
	}
	b.arc.Events = append(b.arc.Events,
		&session.Event{AttemptID: a.ID, Kind: "signal.blur", Severity: session.SeverityWarn, ReceivedAt: t0.Add(time.Minute)},
		&session.Event{AttemptID: a.ID, Kind: "signal.resize", Severity: session.SeverityInfo, ReceivedAt: t0},
		&session.Event{AttemptID: a.ID, Kind: "attempt.submitted", Severity: session.SeverityInfo, ReceivedAt: t0})
	b.arc.Penalties[a.ID] = []*session.Penalty{
		{Kind: session.PenaltyStrike}, {Kind: session.PenaltyStrike, RevertedAt: t0},
		{Kind: session.PenaltyTime, Amount: 90000},
	}
	inc := Build(b.arc, nil).Incidents
	if len(inc.ByKind) != 2 || inc.ByKind[0].Kind != "copy" || inc.ByKind[0].High != 3 || inc.ByKind[1].Warn != 1 {
		t.Fatalf("by kind: %+v", inc.ByKind)
	}
	if inc.Step != 1 || len(inc.Timeline) != 30 || inc.Timeline[1].High != 1 || inc.Timeline[1].Warn != 1 || inc.Timeline[25].High != 1 {
		t.Fatalf("timeline: %+v", inc.Timeline)
	}
	if inc.Strikes != 1 || inc.Reverted != 1 || inc.TimeLostMin != 1.5 {
		t.Fatalf("penalties: %+v", inc)
	}
}

func question(r *Report, id string) QuestionStat {
	for _, q := range r.Questions {
		if q.ID == id {
			return q
		}
	}
	return QuestionStat{}
}

func TestTailAtLeast(t *testing.T) {
	if p := tailAtLeast([]float64{0.5, 0.5}, 2); p != 0.25 {
		t.Fatalf("got %v", p)
	}
	if p := tailAtLeast([]float64{0.5, 0.5}, 0); p != 1 {
		t.Fatalf("got %v", p)
	}
	if p := tailAtLeast(nil, 1); p != 0 {
		t.Fatalf("got %v", p)
	}
}
