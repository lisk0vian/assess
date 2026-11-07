package session

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/grading"
)

// Corrections of a finished session's questions, made after the teacher sees
// the report. They are events, so the record keeps them and they can be undone.
const (
	CorrectVoid   = "void"   // the question no longer counts, for anybody
	CorrectAccept = "accept" // another answer also scores

	eventCorrected = "session.corrected"
	eventUndone    = "session.correction_undone"
)

// Correction is one correction of a question for everyone.
type Correction struct {
	ID         int64 // its event
	QuestionID string
	Kind       string
	Value      string // the accepted answer: an option id, ids joined by commas, true or false, a number or a text
	At         time.Time
}

// ScoreChange is how a correction moves one student's grade, in percent.
type ScoreChange struct {
	Name          string
	Before, After float64
}

// num reads an id that went through JSON.
func num(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

func corrections(tx Tx, sessionID string) ([]Correction, error) {
	evs, err := tx.ListEventsOfKind(sessionID, []string{eventCorrected, eventUndone})
	if err != nil {
		return nil, err
	}
	undone := map[int64]bool{}
	for _, e := range evs {
		if e.Kind == eventUndone {
			undone[num(e.Data["correction"])] = true
		}
	}
	var out []Correction
	for _, e := range evs {
		if e.Kind == eventCorrected && !undone[e.ID] {
			kind, _ := e.Data["kind"].(string)
			value, _ := e.Data["value"].(string)
			out = append(out, Correction{ID: e.ID, QuestionID: e.QuestionID, Kind: kind, Value: value, At: e.ReceivedAt})
		}
	}
	return out, nil
}

// accepted is the answer key a correction adds.
func accepted(q *exam.Question, key exam.Answer, value string) (exam.Answer, error) {
	a := key
	value = strings.TrimSpace(value)
	bad := fmt.Errorf("%w: %q is not an answer to %s", ErrInvalidInput, value, q.ID)
	hasOption := func(id string) bool {
		return slices.ContainsFunc(q.Options, func(o exam.Option) bool { return o.ID == id })
	}
	switch {
	case q.Type == exam.TypeSingle:
		if !hasOption(value) {
			return a, bad
		}
		a.Option = value
	case q.Type == exam.TypeMultiple:
		ids := strings.Split(value, ",")
		for _, id := range ids {
			if !hasOption(id) {
				return a, bad
			}
		}
		a.Options = ids
	case q.Type == exam.TypeTrueFalse:
		v, err := strconv.ParseBool(value)
		if err != nil {
			return a, bad
		}
		a.Value = &v
	case q.Input == "number":
		n, ok := grading.ParseNumber(value)
		if !ok {
			return a, bad
		}
		a.Number = &n
	default:
		if value == "" {
			return a, bad
		}
		a.Accept = []string{value}
	}
	return a, nil
}

// score grades att with the session's corrections, without writing: each
// question's points and whether it is right, the total and the maximum.
type scored struct {
	points  map[string]float64
	correct map[string]bool
	score   float64
	max     float64
}

func (s *Service) score(tx Tx, sess *Session, att *Attempt, extra *Correction) (*scored, error) {
	pub, err := s.published(tx, sess.ExamHash)
	if err != nil {
		return nil, err
	}
	answers, err := tx.ListAnswers(att.ID)
	if err != nil {
		return nil, err
	}
	penalties, err := tx.ListPenalties(att.ID)
	if err != nil {
		return nil, err
	}
	corr, err := corrections(tx, sess.ID)
	if err != nil {
		return nil, err
	}
	if extra != nil {
		corr = append(corr, *extra)
	}
	voidAll := map[string]bool{}
	alt := map[string][]exam.Answer{}
	for _, c := range corr {
		q := pub.Exam.Questions[c.QuestionID]
		switch {
		case q == nil:
		case c.Kind == CorrectVoid:
			voidAll[c.QuestionID] = true
		case c.Kind == CorrectAccept:
			if a, err := accepted(q, pub.Exam.Key.Answers[q.ID], c.Value); err == nil {
				alt[q.ID] = append(alt[q.ID], a)
			}
		}
	}
	void := voided(penalties)
	byQ := make(map[string]*Answer, len(answers))
	for _, a := range answers {
		byQ[a.QuestionID] = a
	}
	out := &scored{points: map[string]float64{}, correct: map[string]bool{}}
	for _, sl := range att.Layout {
		if voidAll[sl.QuestionID] {
			out.points[sl.QuestionID] = 0
			continue
		}
		out.max += sl.Points
		a, ok := byQ[sl.QuestionID]
		if !ok {
			continue
		}
		q := pub.Exam.Questions[sl.QuestionID]
		res := grading.Grade(q, pub.Exam.Key.Answers[sl.QuestionID], a.Response)
		for _, k := range alt[sl.QuestionID] {
			if r := grading.Grade(q, k, a.Response); r.Points > res.Points {
				res = r
			}
		}
		if void[sl.QuestionID] {
			res.Points = 0
		}
		out.points[sl.QuestionID], out.correct[sl.QuestionID] = res.Points, res.Correct
		out.score += res.Points
	}
	out.score = roundScore(max(0, out.score-pointsLost(penalties)))
	return out, nil
}

// endedSession is a session that can be corrected: finished, and the question in its exam.
func (s *Service) endedSession(tx Tx, sessionID, questionID string) (*Session, error) {
	sess, err := tx.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	if sess.State != StateEnded {
		return nil, fmt.Errorf("%w: correct a session once it has ended", ErrInvalidInput)
	}
	if questionID != "" {
		pub, err := s.published(tx, sess.ExamHash)
		if err != nil {
			return nil, err
		}
		if pub.Exam.Questions[questionID] == nil {
			return nil, fmt.Errorf("%w: no question %q", ErrInvalidInput, questionID)
		}
	}
	return sess, nil
}

func (s *Service) checkCorrection(tx Tx, sess *Session, c Correction) error {
	if c.Kind != CorrectVoid && c.Kind != CorrectAccept {
		return fmt.Errorf("%w: unknown correction %q", ErrInvalidInput, c.Kind)
	}
	if c.Kind == CorrectVoid {
		return nil
	}
	pub, err := s.published(tx, sess.ExamHash)
	if err != nil {
		return err
	}
	q := pub.Exam.Questions[c.QuestionID]
	_, err = accepted(q, pub.Exam.Key.Answers[q.ID], c.Value)
	return err
}

func pct(score, maxScore float64) float64 {
	if maxScore <= 0 {
		return 0
	}
	return score / maxScore * 100
}

// PreviewCorrection says whose grade a correction would change, writing nothing.
func (s *Service) PreviewCorrection(ctx context.Context, sessionID string, c Correction) ([]ScoreChange, error) {
	var out []ScoreChange
	err := s.store.View(ctx, func(tx Tx) error {
		sess, err := s.endedSession(tx, sessionID, c.QuestionID)
		if err != nil {
			return err
		}
		if err := s.checkCorrection(tx, sess, c); err != nil {
			return err
		}
		attempts, err := tx.ListAttempts(sessionID)
		if err != nil {
			return err
		}
		for _, att := range attempts {
			if att.State != AttemptSubmitted {
				continue
			}
			sc, err := s.score(tx, sess, att, &c)
			if err != nil {
				return err
			}
			before, after := pct(att.Score, att.MaxScore), pct(sc.score, sc.max)
			if roundScore(before) == roundScore(after) {
				continue
			}
			name := att.ParticipantID
			if p, err := tx.GetParticipant(att.ParticipantID); err == nil {
				name = p.Name
			}
			out = append(out, ScoreChange{Name: name, Before: before, After: after})
		}
		return nil
	})
	return out, err
}

// regradeAll regrades every submitted attempt of a session.
func (s *Service) regradeAll(tx Tx, sess *Session) error {
	attempts, err := tx.ListAttempts(sess.ID)
	if err != nil {
		return err
	}
	for _, att := range attempts {
		if att.State == AttemptSubmitted {
			if err := s.regrade(tx, sess, att); err != nil {
				return err
			}
		}
	}
	return nil
}

// Correct records a correction of a finished session and regrades it. The
// students' grades change, and the record keeps what was corrected and when.
func (s *Service) Correct(ctx context.Context, sessionID string, c Correction) (Correction, error) {
	err := s.store.Update(ctx, func(tx Tx) error {
		sess, err := s.endedSession(tx, sessionID, c.QuestionID)
		if err != nil {
			return err
		}
		if err := s.checkCorrection(tx, sess, c); err != nil {
			return err
		}
		c.At = s.clock()
		e := &Event{SessionID: sessionID, Kind: eventCorrected, Severity: SeverityInfo, QuestionID: c.QuestionID,
			Data: map[string]any{"kind": c.Kind, "value": c.Value}, ReceivedAt: c.At}
		if err := tx.InsertEvent(e); err != nil {
			return err
		}
		c.ID = e.ID
		return s.regradeAll(tx, sess)
	})
	if err == nil {
		s.changed(sessionID)
	}
	return c, err
}

// UndoCorrection takes a correction back and regrades the session.
func (s *Service) UndoCorrection(ctx context.Context, sessionID string, id int64) error {
	err := s.store.Update(ctx, func(tx Tx) error {
		sess, err := s.endedSession(tx, sessionID, "")
		if err != nil {
			return err
		}
		active, err := corrections(tx, sessionID)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(active, func(c Correction) bool { return c.ID == id }) {
			return fmt.Errorf("%w: no correction %d", ErrNotFound, id)
		}
		if err := tx.InsertEvent(&Event{SessionID: sessionID, Kind: eventUndone, Severity: SeverityInfo,
			Data: map[string]any{"correction": id}, ReceivedAt: s.clock()}); err != nil {
			return err
		}
		return s.regradeAll(tx, sess)
	})
	if err == nil {
		s.changed(sessionID)
	}
	return err
}

// Corrections lists the corrections in force, oldest first.
func (s *Service) Corrections(ctx context.Context, sessionID string) ([]Correction, error) {
	var out []Correction
	err := s.store.View(ctx, func(tx Tx) error {
		var err error
		out, err = corrections(tx, sessionID)
		return err
	})
	return out, err
}
