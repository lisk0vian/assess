package session

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/proctor"
)

// ErrQuestionClosed rejects answers to a voided question or one whose
// time_limit ran out.
var ErrQuestionClosed = errors.New("question is closed")

// questionGrace absorbs latency on per-question time limits.
const questionGrace = 5 * time.Second

// ProctorSettings returns the effective proctoring options of an exam.
func ProctorSettings(m *exam.Manifest) proctor.Settings {
	p := m.Proctoring
	return proctor.Resolve(m.Profile(), p.Fullscreen, p.Devices, p.Reveal, p.StrikeLimit)
}

// TelemetryKey is the per-attempt key the browser signs its telemetry with:
// HMAC-SHA256(session secret, attempt id), hex. Empty if the session has no
// secret (sessions created before phase 5).
func TelemetryKey(sess *Session, attemptID string) string {
	if sess.Secret == "" {
		return ""
	}
	m := hmac.New(sha256.New, []byte(sess.Secret))
	m.Write([]byte(attemptID))
	return hex.EncodeToString(m.Sum(nil))
}

func newSecret() string {
	b := make([]byte, 32)
	randomBytes(b)
	return hex.EncodeToString(b)
}

// SignalReport is one integrity signal from the browser.
type SignalReport struct {
	Kind       string
	DurationMs int64
	QuestionID string
	Seq        int64 // signed telemetry sequence; 0 for server-generated signals
	ClientAt   time.Time
	Data       map[string]any
}

// SignalOutcome is what a signal caused.
type SignalOutcome struct {
	Decision  proctor.Decision
	Duplicate bool // seq already seen: nothing recorded
	Penalties []*Penalty
	Strikes   int // active strikes after this signal
	Limit     int
	Annulled  bool // the strike limit was reached
}

// graceExempt signals count even during the start grace period.
var graceExempt = map[string]bool{proctor.Untrusted: true, proctor.Webdriver: true, proctor.Telemetry: true}

// ReportSignal records a signal and applies the profile's penalties. Reaching
// the strike limit annuls the attempt on the spot. Every penalty is reversible
// with RevertPenalty.
func (s *Service) ReportSignal(ctx context.Context, attemptID string, r SignalReport) (*SignalOutcome, error) {
	out := &SignalOutcome{}
	var sessionID string
	err := s.store.Update(ctx, func(tx Tx) error {
		att, sess, err := s.activeAttempt(tx, attemptID)
		if err != nil {
			return err
		}
		sessionID = sess.ID
		if r.Seq > 0 {
			if r.Seq <= att.SignalSeq {
				out.Duplicate = true
				return nil
			}
			att.SignalSeq = r.Seq
			if err := tx.UpdateAttempt(att); err != nil {
				return err
			}
		}
		pub, err := s.published(tx, sess.ExamHash)
		if err != nil {
			return err
		}
		set := ProctorSettings(&pub.Exam.Manifest)
		out.Limit = set.StrikeLimit
		now := s.clock()
		start := sess.StartedAt
		if att.CreatedAt.After(start) {
			start = att.CreatedAt
		}
		if now.Before(start.Add(set.Grace)) && !graceExempt[r.Kind] {
			out.Decision = proctor.Decision{Ignore: true}
			return nil
		}
		if r.QuestionID != "" && !inLayout(att, r.QuestionID) {
			r.QuestionID = ""
		}
		kind := "signal." + r.Kind
		prev, err := tx.CountEvents(att.ID, kind)
		if err != nil {
			return err
		}
		d := set.Decide(proctor.Signal{Kind: r.Kind, DurationMs: r.DurationMs, Count: prev + 1})
		out.Decision = d
		if d.Ignore {
			return nil
		}
		data := map[string]any{}
		for k, v := range r.Data {
			data[k] = v
		}
		if r.DurationMs > 0 {
			data["duration_ms"] = r.DurationMs
		}
		ev := &Event{SessionID: sess.ID, AttemptID: att.ID, Kind: kind, Severity: Severity(d.Severity),
			QuestionID: r.QuestionID, Data: data, ClientAt: r.ClientAt, ReceivedAt: now}
		if err := tx.InsertEvent(ev); err != nil {
			return err
		}
		add := func(k PenaltyKind, amount float64, q string) error {
			p := &Penalty{AttemptID: att.ID, EventID: ev.ID, Kind: k, Signal: r.Kind, QuestionID: q, Amount: amount, CreatedAt: now}
			if err := tx.InsertPenalty(p); err != nil {
				return err
			}
			out.Penalties = append(out.Penalties, p)
			return nil
		}
		if d.Strike {
			if err := add(PenaltyStrike, 0, r.QuestionID); err != nil {
				return err
			}
		}
		if d.LoseTime > 0 {
			att.Extra -= d.LoseTime
			if err := tx.UpdateAttempt(att); err != nil {
				return err
			}
			if err := add(PenaltyTime, float64(d.LoseTime.Milliseconds()), r.QuestionID); err != nil {
				return err
			}
		}
		if d.VoidQ && r.QuestionID != "" {
			if err := add(PenaltyVoid, 0, r.QuestionID); err != nil {
				return err
			}
		}
		all, err := tx.ListPenalties(att.ID)
		if err != nil {
			return err
		}
		out.Strikes = ActiveStrikes(all)
		if set.StrikeLimit > 0 && out.Strikes >= set.StrikeLimit {
			out.Annulled = true
			return s.annul(tx, sess, att, fmt.Sprintf("%d strikes", out.Strikes))
		}
		return nil
	})
	if err == nil && len(out.Penalties) > 0 {
		s.changed(sessionID)
	}
	return out, err
}

// ActiveStrikes counts the strikes that still apply.
func ActiveStrikes(ps []*Penalty) int {
	n := 0
	for _, p := range ps {
		if p.Kind == PenaltyStrike && p.Active() {
			n++
		}
	}
	return n
}

// annul closes att as annulled inside tx.
func (s *Service) annul(tx Tx, sess *Session, att *Attempt, note string) error {
	if err := s.close(tx, sess, att, SubmitEnded); err != nil {
		return err
	}
	att.State = AttemptAnnulled
	if err := tx.UpdateAttempt(att); err != nil {
		return err
	}
	return tx.InsertEvent(&Event{SessionID: att.SessionID, AttemptID: att.ID, Kind: "attempt.annulled", Severity: SeverityHigh,
		Data: map[string]any{"note": note}, ReceivedAt: s.clock()})
}

// RevertPenalty undoes a penalty, keeping it in the record with the note.
// Lost time is given back; a closed attempt is regraded so voided questions
// and point penalties stop counting. An annulled attempt stays annulled:
// readmit it separately.
func (s *Service) RevertPenalty(ctx context.Context, penaltyID int64, note string) (*Penalty, error) {
	var p *Penalty
	var sessionID string
	err := s.store.Update(ctx, func(tx Tx) error {
		var err error
		if p, err = tx.GetPenalty(penaltyID); err != nil {
			return err
		}
		if !p.Active() {
			return fmt.Errorf("%w: penalty already reverted", ErrNotClosable)
		}
		att, err := tx.GetAttempt(p.AttemptID)
		if err != nil {
			return err
		}
		sessionID = att.SessionID
		now := s.clock()
		note = strings.TrimSpace(note)
		if err := tx.RevertPenalty(p.ID, now, note); err != nil {
			return err
		}
		p.RevertedAt, p.Note = now, note
		if p.Kind == PenaltyTime {
			att.Extra += time.Duration(p.Amount) * time.Millisecond
			if err := tx.UpdateAttempt(att); err != nil {
				return err
			}
		}
		if att.State != AttemptActive && (p.Kind == PenaltyVoid || p.Kind == PenaltyPoints) {
			sess, err := tx.GetSession(att.SessionID)
			if err != nil {
				return err
			}
			if err := s.regrade(tx, sess, att); err != nil {
				return err
			}
		}
		return tx.InsertEvent(&Event{SessionID: att.SessionID, AttemptID: att.ID, Kind: "penalty.reverted", Severity: SeverityInfo,
			QuestionID: p.QuestionID, Data: map[string]any{"penalty": p.ID, "kind": string(p.Kind), "note": note}, ReceivedAt: now})
	})
	if err == nil {
		s.changed(sessionID)
	}
	return p, err
}

// Penalties lists an attempt's penalties, reverted ones included.
func (s *Service) Penalties(ctx context.Context, attemptID string) ([]*Penalty, error) {
	var out []*Penalty
	err := s.store.View(ctx, func(tx Tx) error {
		var err error
		out, err = tx.ListPenalties(attemptID)
		return err
	})
	return out, err
}

// Pledge records that the student accepted the exam rules. Idempotent.
func (s *Service) Pledge(ctx context.Context, attemptID string) (*Attempt, error) {
	var att *Attempt
	err := s.store.Update(ctx, func(tx Tx) error {
		var err error
		if att, err = tx.GetAttempt(attemptID); err != nil {
			return err
		}
		if att.State != AttemptActive {
			return ErrAttemptClosed
		}
		if !att.PledgedAt.IsZero() {
			return nil
		}
		att.PledgedAt = s.clock()
		if err := tx.UpdateAttempt(att); err != nil {
			return err
		}
		return tx.InsertEvent(&Event{SessionID: att.SessionID, AttemptID: att.ID, Kind: "attempt.pledged", Severity: SeverityInfo, ReceivedAt: att.PledgedAt})
	})
	return att, err
}

// OpenQuestion records that the student opened a question and returns when
// its time_limit runs out (zero if it has none).
func (s *Service) OpenQuestion(ctx context.Context, attemptID, questionID string) (time.Time, error) {
	var closes time.Time
	err := s.store.Update(ctx, func(tx Tx) error {
		att, sess, err := s.activeAttempt(tx, attemptID)
		if err != nil {
			return err
		}
		if !inLayout(att, questionID) {
			return ErrUnknownQuestion
		}
		pub, err := s.published(tx, sess.ExamHash)
		if err != nil {
			return err
		}
		closes, err = questionCloses(tx, pub, att.ID, questionID, s.clock())
		return err
	})
	return closes, err
}

// questionCloses returns when a timed question closes, recording its first
// opening at now if needed.
func questionCloses(tx Tx, pub *exam.Published, attemptID, questionID string, now time.Time) (time.Time, error) {
	limit := pub.Exam.Questions[questionID].TimeLimit.D()
	if limit <= 0 {
		return time.Time{}, nil
	}
	opened, err := tx.OpenQuestion(attemptID, questionID, now)
	if err != nil {
		return time.Time{}, err
	}
	return opened.Add(limit), nil
}

// voided returns the questions voided by active penalties.
func voided(ps []*Penalty) map[string]bool {
	out := map[string]bool{}
	for _, p := range ps {
		if p.Kind == PenaltyVoid && p.Active() && p.QuestionID != "" {
			out[p.QuestionID] = true
		}
	}
	return out
}

// pointsLost sums active point penalties.
func pointsLost(ps []*Penalty) float64 {
	var n float64
	for _, p := range ps {
		if p.Kind == PenaltyPoints && p.Active() {
			n += p.Amount
		}
	}
	return n
}
