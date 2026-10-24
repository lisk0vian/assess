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
	"github.com/lisk0vian/assess/internal/session"
)

// MeView is the state of the caller's attempt.
type MeView struct {
	ServerNow int64                       `json:"server_now"` // Unix ms, to align the client clock
	Exam      ExamView                    `json:"exam"`
	Session   SessionView                 `json:"session"`
	Student   ParticipantView             `json:"student"`
	Attempt   AttemptView                 `json:"attempt"`
	Questions []QuestionSummary           `json:"questions"`
	Answers   map[string]grading.Response `json:"answers"`
	Review    []ReviewItem                `json:"review,omitempty"`
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
	ID       string  `json:"id"`
	Number   int     `json:"number"`
	Type     string  `json:"type"`
	Points   float64 `json:"points"`
	Answered bool    `json:"answered"`
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
}

// OptionView is an option in the order this attempt sees it.
type OptionView struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ReviewItem is a per-question correction, shown only after the session ends
// and only when result_visibility is "review".
type ReviewItem struct {
	QuestionID string  `json:"question_id"`
	Points     float64 `json:"points"`
	MaxPoints  float64 `json:"max_points"`
	Correct    bool    `json:"correct"`
	Key        KeyView `json:"key"`
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
	for i, sl := range a.Layout {
		q := d.Exam.Exam.Questions[sl.QuestionID]
		_, answered := v.Answers[sl.QuestionID]
		v.Questions = append(v.Questions, QuestionSummary{ID: q.ID, Number: i + 1, Type: q.Type, Points: sl.Points, Answered: answered})
	}

	graded := a.State == session.AttemptSubmitted
	vis := m.Visibility()
	if graded && (vis == "score" || vis == "review") {
		score := a.Score
		v.Attempt.Score = &score
	}
	if graded && vis == "review" && d.Session.State == session.StateEnded {
		for _, sl := range a.Layout {
			item := ReviewItem{QuestionID: sl.QuestionID, MaxPoints: sl.Points, Key: keyView(d.Exam.Exam.Key.Answers[sl.QuestionID])}
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
		}
		text := make(map[string]string, len(q.Options))
		for _, o := range q.Options {
			text[o.ID] = o.Text
		}
		for _, oid := range sl.Options {
			v.Options = append(v.Options, OptionView{ID: oid, Text: text[oid]})
		}
		return v, true
	}
	return QuestionView{}, false
}
