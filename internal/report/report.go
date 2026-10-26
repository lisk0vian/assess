// Package report analyses a finished session for the teacher: how scores and
// times spread, how each question behaved, when integrity signals happened,
// and three kinds of evidence worth a closer look: pairs of students sharing
// unusual wrong answers, correct answers given right after leaving the exam,
// and questions that became much easier than in earlier sessions.
//
// Evidence is never proof. The report points at what to review; the teacher
// decides.
package report

import (
	"math"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lisk0vian/assess/internal/grading"
	"github.com/lisk0vian/assess/internal/session"
)

// Report is the analysis of one session.
type Report struct {
	Session   SessionInfo    `json:"session"`
	Summary   Summary        `json:"summary"`
	Scores    []Bin          `json:"scores"` // percent of the maximum, submitted attempts
	Times     []Bin          `json:"times"`  // minutes used, submitted attempts
	Questions []QuestionStat `json:"questions"`
	Incidents Incidents      `json:"incidents"`
	Students  []StudentLine  `json:"students"`
	Pairs     []Pair         `json:"pairs"`     // possible collusion, most telling first
	AwayHits  []AwayHit      `json:"away_hits"` // left the exam, came back and got it right
	History   []SessionLine  `json:"history"`   // sessions of the same exam, oldest first, this one included
	Leaks     []Leak         `json:"leaks"`     // questions much easier than before
}

// SessionInfo identifies the session.
type SessionInfo struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	ExamID      string    `json:"exam_id"`
	ExamTitle   string    `json:"exam_title"`
	State       string    `json:"state"`
	StartedAt   time.Time `json:"started_at,omitzero"`
	EndedAt     time.Time `json:"ended_at,omitzero"`
	DurationMin float64   `json:"duration_min"`
}

// Summary is the headline of the session. Scores are percentages of each
// attempt's maximum and count submitted attempts only.
type Summary struct {
	Students      int     `json:"students"`
	Submitted     int     `json:"submitted"`
	Annulled      int     `json:"annulled"`
	Withdrawn     int     `json:"withdrawn"`
	Mean          float64 `json:"mean"`
	Median        float64 `json:"median"`
	StdDev        float64 `json:"std_dev"`
	Min           float64 `json:"min"`
	Max           float64 `json:"max"`
	MedianMinutes float64 `json:"median_minutes"`
}

// Bin is one bar of a histogram: values in [From, To).
type Bin struct {
	From  float64 `json:"from"`
	To    float64 `json:"to"`
	Count int     `json:"count"`
}

// QuestionStat is how one question behaved among submitted attempts.
type QuestionStat struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	Prompt   string  `json:"prompt"` // first line, plain text
	Points   float64 `json:"points"`
	Seen     int     `json:"seen"`     // attempts that received it
	Answered int     `json:"answered"` // gave a non-empty answer
	Correct  int     `json:"correct"`
	Rate     float64 `json:"rate"`      // Correct / Seen
	Share    float64 `json:"share"`     // average share of the points, partial credit included
	Voided   int     `json:"voided"`    // attempts where it was voided by a penalty
	AwayHits int     `json:"away_hits"` // correct answers right after leaving the exam
	Leak     bool    `json:"leak"`      // much easier than in earlier sessions
	// Discrimination correlates getting it right with doing well on the rest
	// of the exam; near zero or negative means it does not tell strong
	// students from weak ones. Nil when there are too few answers.
	Discrimination *float64 `json:"discrimination"`
}

// Incidents summarises the integrity signals of the session.
type Incidents struct {
	ByKind        []KindCount `json:"by_kind"`
	Step          int         `json:"step_min"` // minutes per timeline bucket
	Timeline      []TimeSlot  `json:"timeline"` // from the start of the session
	Strikes       int         `json:"strikes"`  // still active
	Reverted      int         `json:"reverted"` // penalties the teacher reverted
	TimeLostMin   float64     `json:"time_lost_min"`
	VoidedAnswers int         `json:"voided_answers"`
}

// KindCount counts the signals of one kind.
type KindCount struct {
	Kind string `json:"kind"`
	Warn int    `json:"warn"`
	High int    `json:"high"`
}

// TimeSlot counts signals in one stretch of the session.
type TimeSlot struct {
	Minute int `json:"minute"` // start of the slot, minutes from the start
	Warn   int `json:"warn"`
	High   int `json:"high"`
}

// Who identifies a student.
type Who struct {
	Name      string `json:"name"`
	StudentID string `json:"student_id"`
}

// StudentLine is one student's outcome and the evidence that involves them.
type StudentLine struct {
	Who
	State    string   `json:"state"`
	Percent  *float64 `json:"percent"` // nil unless submitted
	Minutes  float64  `json:"minutes,omitempty"`
	Strikes  int      `json:"strikes"`
	Signals  int      `json:"signals"` // warn and high
	AwayHits int      `json:"away_hits"`
	Pairs    int      `json:"pairs"` // flagged pairs they are part of
}

// Evidence levels.
const (
	LevelHigh   = "high"
	LevelMedium = "medium"
)

// Pair is two students whose answers look more alike than chance explains.
type Pair struct {
	Students  [2]Who   `json:"students"`
	Level     string   `json:"level"`
	Common    int      `json:"common"`     // questions both received and answered
	BothWrong int      `json:"both_wrong"` // ...and both got wrong
	SameWrong int      `json:"same_wrong"` // ...with the very same wrong answer
	Expected  float64  `json:"expected"`   // same wrong answers expected by chance
	P         float64  `json:"p"`          // chance of SameWrong or more by luck
	Synced    int      `json:"synced"`     // same answer saved within syncWindow, at different positions
	Questions []string `json:"questions"`  // with the same wrong answer
}

// AwayHit is a correct answer saved right after coming back to the exam.
type AwayHit struct {
	Who
	QuestionID string  `json:"question_id"`
	Signal     string  `json:"signal"` // hidden, blur or fullscreen_exit
	AwayMs     int64   `json:"away_ms"`
	AfterMs    int64   `json:"after_ms"` // from coming back to saving the answer
	Difficulty float64 `json:"difficulty"`
	Strong     bool    `json:"strong"` // the question was hard for the rest of the class
}

// SessionLine is one session in the exam's history.
type SessionLine struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	StartedAt time.Time `json:"started_at,omitzero"`
	Students  int       `json:"students"`
	Submitted int       `json:"submitted"`
	Mean      float64   `json:"mean"`
	Median    float64   `json:"median"`
	Current   bool      `json:"current"`
}

// Leak is a question that became much easier than in earlier sessions.
type Leak struct {
	QuestionID string  `json:"question_id"`
	Prompt     string  `json:"prompt"`
	Before     float64 `json:"before"` // correct rate in earlier sessions
	BeforeN    int     `json:"before_n"`
	Now        float64 `json:"now"`
	NowN       int     `json:"now_n"`
	Z          float64 `json:"z"`
}

// Analysis thresholds.
const (
	syncWindow    = 20 * time.Second // identical answers saved this close look coordinated
	awayBefore    = 2 * time.Second  // an answer this long before the return still counts (clock skew)
	awayAfter     = 90 * time.Second // answers saved this long after coming back count as a hit
	strongHard    = 0.5              // a hit is strong when at least half the others failed
	minSameWrong  = 3                // fewer shared wrong answers is never evidence
	minSynced     = 4
	leakMinBefore = 10 // earlier answers needed to compare
	leakMinNow    = 5
	leakMinJump   = 0.2  // 20 points more correct
	leakZ         = 2.33 // one-sided 1 %
	maxPairs      = 50
	timelineSlots = 30
)

// awaySignals are the signals that mean the student left the exam, or stayed
// still in front of it (frozen_pointer: maybe photographing the screen).
var awaySignals = map[string]string{
	"signal.hidden": "hidden", "signal.blur": "blur", "signal.fullscreen_exit": "fullscreen_exit",
	"signal.frozen_pointer": "frozen_pointer",
}

// attempt is one attempt prepared for analysis.
type attempt struct {
	*session.Attempt
	who     Who
	start   time.Time
	pos     map[string]int     // question id → position in this attempt's exam
	answers map[string]*answer // non-empty answers by question id
	raw     float64            // points before penalties
	rawMax  float64
}

// answer is one non-empty response, graded.
type answer struct {
	key     string // equal for identical responses
	correct bool
	points  float64
	at      time.Time
}

// prepare grades every attempt of arc from scratch, ignoring penalties, so
// the analysis sees what students actually answered.
func prepare(arc *session.Archive) []*attempt {
	ex := arc.Published.Exam
	out := make([]*attempt, 0, len(arc.Attempts))
	for _, a := range arc.Attempts {
		p := arc.Participants[a.ParticipantID]
		at := &attempt{Attempt: a, start: arc.Session.StartedAt, pos: map[string]int{}, answers: map[string]*answer{}}
		if p != nil {
			at.who = Who{Name: p.Name, StudentID: p.StudentID}
		}
		if a.CreatedAt.After(at.start) {
			at.start = a.CreatedAt
		}
		for i, sl := range a.Layout {
			at.pos[sl.QuestionID] = i
			at.rawMax += ex.Key.Answers[sl.QuestionID].Points
		}
		for _, ans := range arc.Answers[a.ID] {
			q := ex.Questions[ans.QuestionID]
			if _, ok := at.pos[ans.QuestionID]; !ok || q == nil || ans.Response.Empty() {
				continue
			}
			res := grading.Grade(q, ex.Key.Answers[ans.QuestionID], ans.Response)
			at.answers[ans.QuestionID] = &answer{key: responseKey(ans.Response), correct: res.Correct, points: res.Points, at: ans.UpdatedAt}
			at.raw += res.Points
		}
		out = append(out, at)
	}
	return out
}

// responseKey renders a response so identical answers compare equal.
func responseKey(r grading.Response) string {
	switch {
	case r.Option != "":
		return "o:" + r.Option
	case len(r.Options) > 0:
		opts := append([]string(nil), r.Options...)
		sort.Strings(opts)
		return "m:" + strings.Join(opts, ",")
	case r.Value != nil:
		if *r.Value {
			return "b:1"
		}
		return "b:0"
	default:
		return "t:" + strings.Join(strings.Fields(strings.ToLower(r.Text)), " ")
	}
}

func (a *attempt) submitted() bool { return a.State == session.AttemptSubmitted }

func (a *attempt) percent() float64 {
	if a.MaxScore <= 0 {
		return 0
	}
	return a.Score / a.MaxScore * 100
}

func (a *attempt) minutes() float64 {
	if a.SubmittedAt.IsZero() || a.start.IsZero() {
		return 0
	}
	return a.SubmittedAt.Sub(a.start).Minutes()
}

// Build analyses cur. earlier are previous sessions of the same exam, used
// for the history and to spot leaked questions.
func Build(cur *session.Archive, earlier []*session.Archive) *Report {
	sess := cur.Session
	ex := cur.Published.Exam
	r := &Report{Session: SessionInfo{
		ID: sess.ID, Code: sess.Code, ExamID: ex.Manifest.ID, ExamTitle: ex.Manifest.Title, State: string(sess.State),
		StartedAt: sess.StartedAt, EndedAt: sess.EndedAt, DurationMin: sess.Duration.Minutes(),
	}}
	atts := prepare(cur)
	r.summary(atts)
	r.questions(cur, atts)
	r.incidents(cur)
	r.pairs(atts)
	r.awayHits(cur, atts)
	r.history(cur, earlier)
	r.leaks(earlier)
	r.students(cur, atts)
	return r
}

func (r *Report) summary(atts []*attempt) {
	s := &r.Summary
	var pct, mins []float64
	for _, a := range atts {
		s.Students++
		switch a.State {
		case session.AttemptSubmitted:
			s.Submitted++
			pct = append(pct, a.percent())
			mins = append(mins, a.minutes())
		case session.AttemptAnnulled:
			s.Annulled++
		case session.AttemptWithdrawn:
			s.Withdrawn++
		}
	}
	if len(pct) > 0 {
		s.Mean, s.StdDev = meanStd(pct)
		s.Median = median(pct)
		s.Min, s.Max = round2(slices.Min(pct)), round2(slices.Max(pct))
		s.MedianMinutes = median(mins)
	}
	r.Scores = histogram(pct, 0, 100, 10)
	width := math.Max(1, math.Ceil(r.Session.DurationMin/10))
	top := math.Max(width, math.Ceil(r.Session.DurationMin/width)*width)
	r.Times = histogram(mins, 0, top, width)
}

// histogram counts values in bins of width from lo up to hi; values at or
// beyond hi land in the last bin.
func histogram(vals []float64, lo, hi, width float64) []Bin {
	n := max(1, int(math.Ceil((hi-lo)/width)))
	bins := make([]Bin, n)
	for i := range bins {
		bins[i] = Bin{From: lo + float64(i)*width, To: lo + float64(i+1)*width}
	}
	for _, v := range vals {
		i := int((v - lo) / width)
		bins[min(max(i, 0), n-1)].Count++
	}
	return bins
}

func (r *Report) questions(arc *session.Archive, atts []*attempt) {
	ex := arc.Published.Exam
	voided := map[string]int{}
	for _, ps := range arc.Penalties {
		for _, p := range ps {
			if p.Kind == session.PenaltyVoid && p.Active() {
				voided[p.QuestionID]++
			}
		}
	}
	ids := make([]string, 0, len(ex.Questions))
	for id := range ex.Questions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		q := ex.Questions[id]
		pts := ex.Key.Answers[id].Points
		st := QuestionStat{ID: id, Type: q.Type, Prompt: plainLine(q.Prompt), Points: pts, Voided: voided[id]}
		var xs, ys []float64
		for _, a := range atts {
			if _, ok := a.pos[id]; !ok || !a.submitted() {
				continue
			}
			st.Seen++
			x, got := 0.0, 0.0
			if ans := a.answers[id]; ans != nil {
				st.Answered++
				got = ans.points
				if ans.correct {
					st.Correct++
					x = 1
				}
				if pts > 0 {
					st.Share += ans.points / pts
				}
			}
			if rest := a.rawMax - pts; rest > 0 {
				xs, ys = append(xs, x), append(ys, (a.raw-got)/rest)
			}
		}
		if st.Seen > 0 {
			st.Rate = float64(st.Correct) / float64(st.Seen)
			st.Share /= float64(st.Seen)
		}
		if len(xs) >= 5 {
			if c, ok := pearson(xs, ys); ok {
				st.Discrimination = &c
			}
		}
		r.Questions = append(r.Questions, st)
	}
}

func (r *Report) incidents(arc *session.Archive) {
	inc := &r.Incidents
	start := arc.Session.StartedAt
	inc.Step = max(1, int(math.Ceil(r.Session.DurationMin/timelineSlots)))
	kinds := map[string]*KindCount{}
	slots := map[int]*TimeSlot{}
	last := int(r.Session.DurationMin)
	for _, e := range arc.Events {
		kind, ok := strings.CutPrefix(e.Kind, "signal.")
		if !ok || (e.Severity != session.SeverityWarn && e.Severity != session.SeverityHigh) {
			continue
		}
		k := kinds[kind]
		if k == nil {
			k = &KindCount{Kind: kind}
			kinds[kind] = k
		}
		minute := 0
		if !start.IsZero() && e.ReceivedAt.After(start) {
			minute = int(e.ReceivedAt.Sub(start).Minutes())
		}
		last = max(last, minute+1)
		slot := minute / inc.Step
		ts := slots[slot]
		if ts == nil {
			ts = &TimeSlot{Minute: slot * inc.Step}
			slots[slot] = ts
		}
		if e.Severity == session.SeverityHigh {
			k.High++
			ts.High++
		} else {
			k.Warn++
			ts.Warn++
		}
	}
	for _, k := range kinds {
		inc.ByKind = append(inc.ByKind, *k)
	}
	sort.Slice(inc.ByKind, func(i, j int) bool {
		a, b := inc.ByKind[i], inc.ByKind[j]
		if a.Warn+a.High != b.Warn+b.High {
			return a.Warn+a.High > b.Warn+b.High
		}
		return a.Kind < b.Kind
	})
	for i := range (last + inc.Step - 1) / inc.Step {
		if ts := slots[i]; ts != nil {
			inc.Timeline = append(inc.Timeline, *ts)
		} else {
			inc.Timeline = append(inc.Timeline, TimeSlot{Minute: i * inc.Step})
		}
	}
	for _, ps := range arc.Penalties {
		for _, p := range ps {
			if !p.Active() {
				inc.Reverted++
				continue
			}
			switch p.Kind {
			case session.PenaltyStrike:
				inc.Strikes++
			case session.PenaltyTime:
				inc.TimeLostMin += p.Amount / float64(time.Minute.Milliseconds())
			case session.PenaltyVoid:
				inc.VoidedAnswers++
			case session.PenaltyPoints:
			}
		}
	}
}

// pairs compares every two students. Sharing a wrong answer is telling in
// proportion to how rare that wrong answer is: two students who both picked
// the one tempting distractor say little, two who typed the same odd wrong
// number say a lot. The chance of each match comes from how the whole class
// answered wrong; their sum is what luck alone would give.
func (r *Report) pairs(atts []*attempt) {
	// How likely two wrong answers to a question are the same, by question.
	wrong := map[string]map[string]int{}
	for _, a := range atts {
		for q, ans := range a.answers {
			if !ans.correct {
				if wrong[q] == nil {
					wrong[q] = map[string]int{}
				}
				wrong[q][ans.key]++
			}
		}
	}
	match := map[string]float64{}
	for q, keys := range wrong {
		var n, same float64
		for _, c := range keys {
			n += float64(c)
			same += float64(c) * float64(c-1)
		}
		if n >= 2 {
			match[q] = same / (n * (n - 1))
		}
	}

	compared := len(atts) * (len(atts) - 1) / 2
	var out []Pair
	for i, a := range atts {
		for _, b := range atts[i+1:] {
			p := Pair{Students: [2]Who{a.who, b.who}}
			var probs []float64
			for q, x := range a.answers {
				y := b.answers[q]
				if y == nil {
					continue
				}
				p.Common++
				if !x.correct && !y.correct {
					p.BothWrong++
					probs = append(probs, match[q])
					p.Expected += match[q]
					if x.key == y.key {
						p.SameWrong++
						p.Questions = append(p.Questions, q)
					}
				}
				if x.key == y.key && a.pos[q] != b.pos[q] && absDur(x.at.Sub(y.at)) <= syncWindow {
					p.Synced++
				}
			}
			p.P = tailAtLeast(probs, p.SameWrong)
			adj := p.P * float64(compared) // many pairs are compared: demand more of each one
			wrongs := p.SameWrong >= minSameWrong && adj < 0.05
			synced := p.Synced >= minSynced && p.Synced*3 >= p.Common
			switch {
			case wrongs && (adj < 0.01 || synced): // very unlikely, or two separate signs
				p.Level = LevelHigh
			case wrongs || synced:
				p.Level = LevelMedium
			default:
				continue
			}
			sort.Strings(p.Questions)
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Level == LevelHigh) != (out[j].Level == LevelHigh) {
			return out[i].Level == LevelHigh
		}
		if out[i].P != out[j].P {
			return out[i].P < out[j].P
		}
		return out[i].Synced > out[j].Synced
	})
	if len(out) > maxPairs {
		out = out[:maxPairs]
	}
	r.Pairs = out
}

// tailAtLeast is the chance that at least k of independent events with
// probabilities ps happen (Poisson binomial).
func tailAtLeast(ps []float64, k int) float64 {
	if k <= 0 {
		return 1
	}
	dist := make([]float64, len(ps)+1)
	dist[0] = 1
	for i, p := range ps {
		for j := i + 1; j > 0; j-- {
			dist[j] = dist[j]*(1-p) + dist[j-1]*p
		}
		dist[0] *= 1 - p
	}
	var tail float64
	for j := k; j < len(dist); j++ {
		tail += dist[j]
	}
	return math.Min(1, tail)
}

// awayHits finds correct answers saved right after an absence that started
// on the same question, weighted by how hard the question was for the rest.
func (r *Report) awayHits(arc *session.Archive, atts []*attempt) {
	r.AwayHits = awayHits(arc, atts)
}

// AwaySignalKinds are the event kinds AwayHits reads; arc.Events may hold
// only these.
func AwaySignalKinds() []string {
	out := make([]string, 0, len(awaySignals))
	for k := range awaySignals {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AwayHits finds, in arc, correct answers saved right after the student came
// back to the question they left. It works on a running session too: then
// the difficulty counts students still answering as not having it right.
func AwayHits(arc *session.Archive) []AwayHit { return awayHits(arc, prepare(arc)) }

func awayHits(arc *session.Archive, atts []*attempt) []AwayHit {
	byID := make(map[string]*attempt, len(atts))
	seen, correct := map[string]int{}, map[string]int{}
	for _, a := range atts {
		byID[a.ID] = a
		for q := range a.pos {
			seen[q]++
			if ans := a.answers[q]; ans != nil && ans.correct {
				correct[q]++
			}
		}
	}
	best := map[[2]string]AwayHit{}
	for _, e := range arc.Events {
		sig, ok := awaySignals[e.Kind]
		if !ok || e.QuestionID == "" {
			continue
		}
		a := byID[e.AttemptID]
		if a == nil {
			continue
		}
		ans := a.answers[e.QuestionID]
		if ans == nil || !ans.correct || ans.at.Before(e.ReceivedAt.Add(-awayBefore)) || ans.at.After(e.ReceivedAt.Add(awayAfter)) {
			continue
		}
		diff := 0.5 // unknown when nobody else saw the question
		if others := seen[e.QuestionID] - 1; others > 0 {
			diff = 1 - float64(correct[e.QuestionID]-1)/float64(others)
		}
		h := AwayHit{Who: a.who, QuestionID: e.QuestionID, Signal: sig, AwayMs: durationMs(e.Data),
			AfterMs: max(0, ans.at.Sub(e.ReceivedAt).Milliseconds()), Difficulty: round2(diff), Strong: diff >= strongHard}
		k := [2]string{a.ID, e.QuestionID}
		if old, ok := best[k]; !ok || h.AwayMs > old.AwayMs {
			best[k] = h
		}
	}
	out := make([]AwayHit, 0, len(best))
	for _, h := range best {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Difficulty != out[j].Difficulty {
			return out[i].Difficulty > out[j].Difficulty
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].QuestionID < out[j].QuestionID
	})
	return out
}

func durationMs(data map[string]any) int64 {
	switch v := data["duration_ms"].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

func (r *Report) history(cur *session.Archive, earlier []*session.Archive) {
	line := func(arc *session.Archive, current bool) SessionLine {
		l := SessionLine{ID: arc.Session.ID, Code: arc.Session.Code, StartedAt: arc.Session.StartedAt, Current: current}
		var pct []float64
		for _, a := range arc.Attempts {
			l.Students++
			if a.State == session.AttemptSubmitted && a.MaxScore > 0 {
				l.Submitted++
				pct = append(pct, a.Score/a.MaxScore*100)
			}
		}
		if len(pct) > 0 {
			l.Mean, _ = meanStd(pct)
			l.Median = median(pct)
		}
		return l
	}
	for _, arc := range earlier {
		r.History = append(r.History, line(arc, false))
	}
	r.History = append(r.History, line(cur, true))
}

// leaks compares each question's correct rate with earlier sessions. A big,
// significant jump suggests the question circulated: rotate it.
func (r *Report) leaks(earlier []*session.Archive) {
	before := map[string][2]int{} // question → correct, seen
	for _, arc := range earlier {
		for _, a := range prepare(arc) {
			if !a.submitted() {
				continue
			}
			for q := range a.pos {
				c := before[q]
				c[1]++
				if ans := a.answers[q]; ans != nil && ans.correct {
					c[0]++
				}
				before[q] = c
			}
		}
	}
	leaked := map[string]bool{}
	for _, st := range r.Questions {
		b := before[st.ID]
		if b[1] < leakMinBefore || st.Seen < leakMinNow {
			continue
		}
		p0, p1 := float64(b[0])/float64(b[1]), st.Rate
		pool := float64(b[0]+st.Correct) / float64(b[1]+st.Seen)
		se := math.Sqrt(pool * (1 - pool) * (1/float64(b[1]) + 1/float64(st.Seen)))
		if se == 0 || p1-p0 < leakMinJump {
			continue
		}
		if z := (p1 - p0) / se; z >= leakZ {
			r.Leaks = append(r.Leaks, Leak{QuestionID: st.ID, Prompt: st.Prompt, Before: round2(p0), BeforeN: b[1],
				Now: round2(p1), NowN: st.Seen, Z: round2(z)})
			leaked[st.ID] = true
		}
	}
	hits := map[string]int{}
	for _, h := range r.AwayHits {
		hits[h.QuestionID]++
	}
	for i := range r.Questions {
		r.Questions[i].Leak = leaked[r.Questions[i].ID]
		r.Questions[i].AwayHits = hits[r.Questions[i].ID]
	}
}

func (r *Report) students(arc *session.Archive, atts []*attempt) {
	hits, pairs := map[string]int{}, map[string]int{}
	for _, h := range r.AwayHits {
		hits[h.StudentID]++
	}
	for _, p := range r.Pairs {
		pairs[p.Students[0].StudentID]++
		pairs[p.Students[1].StudentID]++
	}
	for _, a := range atts {
		l := StudentLine{Who: a.who, State: string(a.State), Strikes: session.ActiveStrikes(arc.Penalties[a.ID]),
			Signals: arc.SignalCounts[a.ID], AwayHits: hits[a.who.StudentID], Pairs: pairs[a.who.StudentID]}
		if a.submitted() {
			p := round2(a.percent())
			l.Percent = &p
			l.Minutes = round2(a.minutes())
		}
		r.Students = append(r.Students, l)
	}
	sort.SliceStable(r.Students, func(i, j int) bool { return r.Students[i].Name < r.Students[j].Name })
}

// plainLine is the first line of a Markdown prompt as plain text.
func plainLine(md string) string {
	for line := range strings.SplitSeq(md, "\n") {
		line = strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "#>*_`-"))
		line = strings.NewReplacer("**", "", "__", "", "`", "", "$", "").Replace(line)
		line = strings.Join(strings.Fields(line), " ")
		if line == "" || strings.HasPrefix(line, "![") || strings.HasPrefix(line, "|") {
			continue
		}
		if utf8.RuneCountInString(line) > 80 {
			line = string([]rune(line)[:79]) + "…"
		}
		return line
	}
	return ""
}

func meanStd(v []float64) (avg, std float64) {
	avg = mean(v)
	for _, x := range v {
		std += (x - avg) * (x - avg)
	}
	return round2(avg), round2(math.Sqrt(std / float64(len(v))))
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return round2(s[n/2])
	}
	return round2((s[n/2-1] + s[n/2]) / 2)
}

// pearson is the correlation of xs and ys; false when either is constant.
func pearson(xs, ys []float64) (float64, bool) {
	mx, my := mean(xs), mean(ys)
	var sxy, sxx, syy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return 0, false
	}
	return round2(sxy / math.Sqrt(sxx*syy)), true
}

func mean(v []float64) float64 {
	var m float64
	for _, x := range v {
		m += x
	}
	return m / float64(len(v))
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
