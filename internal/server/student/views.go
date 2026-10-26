// Package student serves the HTTP API used by the students' browsers.
//
// The answer key never leaves the server: views are built field by field from
// the published exam, and scores or corrections appear only when the exam's
// result_visibility allows it.
package student

import (
	"time"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/grading"
	"github.com/lisk0vian/assess/internal/proctor"
	"github.com/lisk0vian/assess/internal/session"
)

// MeView is the state of the caller's attempt.
type MeView struct {
	ServerNow  int64                       `json:"server_now"` // Unix ms, to align the client clock
	Exam       ExamView                    `json:"exam"`
	Session    SessionView                 `json:"session"`
	Student    ParticipantView             `json:"student"`
	Attempt    AttemptView                 `json:"attempt"`
	Questions  []QuestionSummary           `json:"questions"`
	Answers    map[string]grading.Response `json:"answers"`
	Review     []ReviewItem                `json:"review,omitempty"`
	Proctoring ProctoringView              `json:"proctoring"`
	Penalties  []PenaltyView               `json:"penalties"`
}

// ProctoringView tells the browser which signals to collect and how to sign
// them. The key is only sent while the attempt is active.
type ProctoringView struct {
	Profile     string `json:"profile"` // off | standard | strict
	Fullscreen  string `json:"fullscreen"`
	Devices     string `json:"devices"`
	StrikeLimit int    `json:"strike_limit"`
	GraceMs     int64  `json:"grace_ms"`
	Key         string `json:"key,omitempty"` // hex telemetry key
	SignalSeq   int64  `json:"signal_seq"`
	Pledged     bool   `json:"pledged"`
	Strikes     int    `json:"strikes"`
	// Away limits for countdowns, ms.
	Thresholds proctor.Thresholds `json:"thresholds"`
	// Percentage of the remaining time a long absence costs.
	LosePercent int `json:"lose_percent"`
	// Report a still pointer followed by an answer (frozen_pointer).
	FrozenPointer bool `json:"frozen_pointer"`
	// Read the prompt once when it opens, in ranges of words.
	AutoRead bool `json:"auto_read"`
	// Reading guide speed: slow | normal | fast.
	ReadSpeed string `json:"read_speed"`
}

// PenaltyView is an active penalty, shown to the student for transparency.
type PenaltyView struct {
	Kind       session.PenaltyKind `json:"kind"`
	Signal     string              `json:"signal"`
	QuestionID string              `json:"question_id,omitempty"`
	Amount     float64             `json:"amount,omitempty"`
	At         int64               `json:"at"`
}

// ExamView is the public part of the manifest.
type ExamView struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Language         string `json:"language"`
	Instructions     string `json:"instructions,omitempty"`
	ResultVisibility string `json:"result_visibility"`
	Accent           string `json:"accent,omitempty"`
	Logo             string `json:"logo,omitempty"`
}

// SessionView is the session timing as the student sees it.
type SessionView struct {
	State     session.State `json:"state"`
	StartedAt int64         `json:"started_at,omitempty"`
	EndsAt    int64         `json:"ends_at,omitempty"`
}

// ParticipantView identifies the participant.
type ParticipantView struct {
	Name      string `json:"name"`
	StudentID string `json:"student_id"`
}

// AttemptView is the caller's attempt.
type AttemptView struct {
	State        session.AttemptState `json:"state"`
	Deadline     int64                `json:"deadline,omitempty"` // Unix ms, includes extra time
	ExtraMs      int64                `json:"extra_ms"`
	SubmittedAt  int64                `json:"submitted_at,omitempty"`
	SubmitReason session.SubmitReason `json:"submit_reason,omitempty"`
	Score        *float64             `json:"score,omitempty"`
	MaxScore     float64              `json:"max_score"`
	LastSeq      int64                `json:"last_seq"`
}

// QuestionSummary is one entry of the attempt's layout.
type QuestionSummary struct {
	ID          string  `json:"id"`
	Number      int     `json:"number"`
	Type        string  `json:"type"`
	Points      float64 `json:"points"`
	Answered    bool    `json:"answered"`
	Voided      bool    `json:"voided,omitempty"`
	TimeLimitMs int64   `json:"time_limit_ms,omitempty"` // opening the question starts its clock
	ClosesAt    int64   `json:"closes_at,omitempty"`     // Unix ms, timed questions once opened
}

// QuestionView is a full question without any scoring information.
type QuestionView struct {
	ID          string       `json:"id"`
	Number      int          `json:"number"`
	Type        string       `json:"type"`
	Prompt      string       `json:"prompt"`
	Options     []OptionView `json:"options,omitempty"`
	Input       string       `json:"input,omitempty"`
	Placeholder string       `json:"placeholder,omitempty"`
	Points      float64      `json:"points"`
	TimeLimitMs int64        `json:"time_limit_ms,omitempty"`
	ClosesAt    int64        `json:"closes_at,omitempty"`
	Voided      bool         `json:"voided,omitempty"`
	Reveal      string       `json:"reveal"` // always | hover
	Lens        bool         `json:"lens"`   // read the prompt in ranges of words, one at a time
}

// OptionView is an option in the order this attempt sees it. With reveal
// "hover", Text is empty and Sealed carries it (see seal).
type OptionView struct {
	ID     string `json:"id"`
	Text   string `json:"text,omitempty"`
	Sealed string `json:"sealed,omitempty"`
}

// ReviewItem is a per-question correction, shown only after the session ends
// and only when result_visibility is "review".
type ReviewItem struct {
	QuestionID string       `json:"question_id"`
	Prompt     string       `json:"prompt"`
	Options    []OptionView `json:"options,omitempty"` // attempt order, text always visible
	Points     float64      `json:"points"`
	MaxPoints  float64      `json:"max_points"`
	Correct    bool         `json:"correct"`
	Key        KeyView      `json:"key"`
}

// KeyView is the correct answer of one question.
type KeyView struct {
	Option  string   `json:"option,omitempty"`
	Options []string `json:"options,omitempty"`
	Value   *bool    `json:"value,omitempty"`
	Number  *float64 `json:"number,omitempty"`
	Accept  []string `json:"accept,omitempty"`
}

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func buildMe(d *session.AttemptDetail, now time.Time) MeView {
	m := d.Exam.Exam.Manifest
	a := d.Attempt
	v := MeView{
		ServerNow: now.UnixMilli(),
		Exam: ExamView{
			ID: m.ID, Title: m.Title, Language: m.Lang(), Instructions: m.Instructions,
			ResultVisibility: m.Visibility(), Accent: m.Branding.Accent, Logo: m.Branding.Logo,
		},
		Session: SessionView{State: d.Session.State, StartedAt: ms(d.Session.StartedAt), EndsAt: ms(d.Session.EndsAt)},
		Student: ParticipantView{Name: d.Participant.Name, StudentID: d.Participant.StudentID},
		Attempt: AttemptView{
			State: a.State, Deadline: ms(a.Deadline(d.Session)), ExtraMs: a.Extra.Milliseconds(),
			SubmittedAt: ms(a.SubmittedAt), SubmitReason: a.SubmitReason, MaxScore: a.MaxScore, LastSeq: a.LastSeq,
		},
		Answers: map[string]grading.Response{},
	}
	byQ := map[string]*session.Answer{}
	for _, ans := range d.Answers {
		byQ[ans.QuestionID] = ans
		if !ans.Response.Empty() {
			v.Answers[ans.QuestionID] = ans.Response
		}
	}
	void := voidedQuestions(d.Penalties)
	for i, sl := range a.Layout {
		q := d.Exam.Exam.Questions[sl.QuestionID]
		_, answered := v.Answers[sl.QuestionID]
		v.Questions = append(v.Questions, QuestionSummary{ID: q.ID, Number: i + 1, Type: q.Type, Points: sl.Points,
			Answered: answered, Voided: void[q.ID], TimeLimitMs: q.TimeLimit.D().Milliseconds(), ClosesAt: closesAt(d, q.ID)})
	}

	set := session.ProctorSettings(&m)
	v.Proctoring = ProctoringView{
		Profile: set.Profile, Fullscreen: set.Fullscreen, Devices: set.Devices, StrikeLimit: set.StrikeLimit,
		GraceMs: set.Grace.Milliseconds(), SignalSeq: a.SignalSeq, Pledged: !a.PledgedAt.IsZero(),
		Thresholds: set.Thresholds(), LosePercent: int(set.LoseShare()*100 + 0.5), FrozenPointer: set.FrozenPointer,
		AutoRead: set.AutoRead, ReadSpeed: set.ReadSpeed,
	}
	if a.State == session.AttemptActive && set.Enabled() {
		v.Proctoring.Key = session.TelemetryKey(d.Session, a.ID)
	}
	v.Penalties = []PenaltyView{}
	for _, p := range d.Penalties {
		if !p.Active() {
			continue
		}
		if p.Kind == session.PenaltyStrike {
			v.Proctoring.Strikes++
		}
		v.Penalties = append(v.Penalties, PenaltyView{Kind: p.Kind, Signal: p.Signal, QuestionID: p.QuestionID, Amount: p.Amount, At: ms(p.CreatedAt)})
	}

	graded := a.State == session.AttemptSubmitted
	vis := m.Visibility()
	if graded && (vis == "score" || vis == "review") {
		score := a.Score
		v.Attempt.Score = &score
	}
	if graded && vis == "review" && d.Session.State == session.StateEnded {
		for _, sl := range a.Layout {
			q := d.Exam.Exam.Questions[sl.QuestionID]
			item := ReviewItem{
				QuestionID: sl.QuestionID,
				Prompt:     q.Prompt,
				Options:    reviewOptions(sl, q),
				MaxPoints:  sl.Points,
				Key:        keyView(d.Exam.Exam.Key.Answers[sl.QuestionID]),
			}
			if ans, ok := byQ[sl.QuestionID]; ok {
				item.Points, item.Correct = ans.Points, ans.Correct
			}
			v.Review = append(v.Review, item)
		}
	}
	return v
}

func keyView(a exam.Answer) KeyView {
	return KeyView{Option: a.Option, Options: a.Options, Value: a.Value, Number: a.Number, Accept: a.Accept}
}

// reviewOptions returns the options in the order this attempt saw them. The
// attempt is over, so the text is never sealed.
func reviewOptions(sl exam.Slot, q *exam.Question) []OptionView {
	if len(sl.Options) == 0 {
		return nil
	}
	text := make(map[string]string, len(q.Options))
	for _, o := range q.Options {
		text[o.ID] = o.Text
	}
	out := make([]OptionView, 0, len(sl.Options))
	for _, id := range sl.Options {
		out = append(out, OptionView{ID: id, Text: text[id]})
	}
	return out
}

// buildQuestion renders question id as the attempt sees it, or false when the
// question is not part of the attempt.
func buildQuestion(d *session.AttemptDetail, id string) (QuestionView, bool) {
	for i, sl := range d.Attempt.Layout {
		if sl.QuestionID != id {
			continue
		}
		q := d.Exam.Exam.Questions[id]
		v := QuestionView{
			ID: q.ID, Number: i + 1, Type: q.Type, Prompt: q.Prompt, Input: q.Input,
			Placeholder: q.Placeholder, Points: sl.Points, TimeLimitMs: q.TimeLimit.D().Milliseconds(),
			ClosesAt: closesAt(d, id), Voided: voidedQuestions(d.Penalties)[id], Reveal: revealOf(d, q),
		}
		v.Lens = lensOf(d, q)
		key := session.TelemetryKey(d.Session, d.Attempt.ID)
		if v.Reveal == "hover" && key == "" {
			v.Reveal = "always" // nothing to seal with (pre-phase-5 session)
		}
		text := make(map[string]string, len(q.Options))
		for _, o := range q.Options {
			text[o.ID] = o.Text
		}
		for _, oid := range sl.Options {
			if v.Reveal == "hover" {
				v.Options = append(v.Options, OptionView{ID: oid, Sealed: seal(key, id, oid, text[oid])})
			} else {
				v.Options = append(v.Options, OptionView{ID: oid, Text: text[oid]})
			}
		}
		return v, true
	}
	return QuestionView{}, false
}

func inLayout(d *session.AttemptDetail, id string) bool {
	for _, sl := range d.Attempt.Layout {
		if sl.QuestionID == id {
			return true
		}
	}
	return false
}

// revealOf is the question's reveal mode: its own setting, else the exam's.
// With proctoring off, options are always shown.
func revealOf(d *session.AttemptDetail, q *exam.Question) string {
	set := session.ProctorSettings(&d.Exam.Exam.Manifest)
	if q.Reveal != "" && set.Enabled() {
		return q.Reveal
	}
	return set.Reveal
}

// lensOf is whether the question's prompt shows through the lens: its own
// setting, else the exam's, unless the teacher switched the lens off for this
// student. With proctoring off, never.
func lensOf(d *session.AttemptDetail, q *exam.Question) bool {
	set := session.ProctorSettings(&d.Exam.Exam.Manifest)
	if !set.Enabled() || d.Attempt.NoLens {
		return false
	}
	if q.Lens != nil {
		return *q.Lens
	}
	return set.Lens
}

func closesAt(d *session.AttemptDetail, id string) int64 {
	opened, ok := d.Opened[id]
	limit := d.Exam.Exam.Questions[id].TimeLimit.D()
	if !ok || limit <= 0 {
		return 0
	}
	return ms(opened.Add(limit))
}

func voidedQuestions(ps []*session.Penalty) map[string]bool {
	out := map[string]bool{}
	for _, p := range ps {
		if p.Kind == session.PenaltyVoid && p.Active() {
			out[p.QuestionID] = true
		}
	}
	return out
}
