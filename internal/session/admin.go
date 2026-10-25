package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotClosable reports a state change that does not apply to an attempt in
// its current state (e.g. readmitting an active attempt).
var ErrNotClosable = errors.New("not possible in the attempt's current state")

// Withdraw takes a student out of the exam: the attempt is graded for the
// record but marked withdrawn, leaving rankings and statistics. Their data is
// kept and Readmit undoes it.
func (s *Service) Withdraw(ctx context.Context, attemptID, note string) (*Attempt, error) {
	return s.closeAs(ctx, attemptID, AttemptWithdrawn, "attempt.withdrawn", note)
}

// Annul voids an attempt (teacher decision or, in phase 5, three strikes). It
// is graded for the record; Readmit undoes it.
func (s *Service) Annul(ctx context.Context, attemptID, note string) (*Attempt, error) {
	return s.closeAs(ctx, attemptID, AttemptAnnulled, "attempt.annulled", note)
}

func (s *Service) closeAs(ctx context.Context, attemptID string, state AttemptState, kind, note string) (*Attempt, error) {
	var att *Attempt
	err := s.store.Update(ctx, func(tx Tx) error {
		var err error
		if att, err = tx.GetAttempt(attemptID); err != nil {
			return err
		}
		if att.State != AttemptActive {
			return fmt.Errorf("%w: attempt is %s", ErrNotClosable, att.State)
		}
		sess, err := tx.GetSession(att.SessionID)
		if err != nil {
			return err
		}
		if err := s.close(tx, sess, att, SubmitEnded); err != nil {
			return err
		}
		att.State = state
		if err := tx.UpdateAttempt(att); err != nil {
			return err
		}
		return tx.InsertEvent(&Event{SessionID: att.SessionID, AttemptID: att.ID, Kind: kind, Severity: SeverityWarn,
			Data: map[string]any{"note": strings.TrimSpace(note)}, ReceivedAt: s.clock()})
	})
	if err == nil {
		s.changed(att.SessionID)
	}
	return att, err
}

// Readmit reopens a withdrawn or annulled attempt while the session is open.
// Time keeps its absolute deadline; give extra time separately if needed.
func (s *Service) Readmit(ctx context.Context, attemptID, note string) (*Attempt, error) {
	var att *Attempt
	err := s.store.Update(ctx, func(tx Tx) error {
		var err error
		if att, err = tx.GetAttempt(attemptID); err != nil {
			return err
		}
		if att.State != AttemptWithdrawn && att.State != AttemptAnnulled {
			return fmt.Errorf("%w: attempt is %s", ErrNotClosable, att.State)
		}
		sess, err := openSession(tx, att.SessionID)
		if err != nil {
			return err
		}
		if err := s.spareStrikes(tx, sess, att); err != nil {
			return err
		}
		att.State, att.SubmittedAt, att.SubmitReason, att.Score = AttemptActive, time.Time{}, "", 0
		if err := tx.UpdateAttempt(att); err != nil {
			return err
		}
		return tx.InsertEvent(&Event{SessionID: att.SessionID, AttemptID: att.ID, Kind: "attempt.readmitted", Severity: SeverityInfo,
			Data: map[string]any{"note": strings.TrimSpace(note)}, ReceivedAt: s.clock()})
	})
	if err == nil {
		s.changed(att.SessionID)
	}
	return att, err
}

// RemoveParticipant drops a pre-registered student who never joined, or
// withdraws one who did (their data is never deleted mid-session).
func (s *Service) RemoveParticipant(ctx context.Context, participantID string) error {
	var withdraw string
	var sessionID string
	err := s.store.Update(ctx, func(tx Tx) error {
		p, err := tx.GetParticipant(participantID)
		if err != nil {
			return err
		}
		sessionID = p.SessionID
		att, err := tx.GetAttemptByParticipant(participantID)
		if errors.Is(err, ErrNotFound) {
			return tx.DeleteParticipant(participantID)
		}
		if err != nil {
			return err
		}
		withdraw = att.ID
		return nil
	})
	if err != nil {
		return err
	}
	if withdraw != "" {
		_, err = s.Withdraw(ctx, withdraw, "removed by the teacher")
		if errors.Is(err, ErrNotClosable) {
			return nil // already closed: nothing to withdraw
		}
		return err
	}
	s.changed(sessionID)
	return nil
}

// RecordEvent stores an event (teacher actions, connection changes).
func (s *Service) RecordEvent(ctx context.Context, e *Event) error {
	if e.ReceivedAt.IsZero() {
		e.ReceivedAt = s.clock()
	}
	return s.store.Update(ctx, func(tx Tx) error { return tx.InsertEvent(e) })
}

// AttemptOfStudent finds the attempt of a student in a session.
func (s *Service) AttemptOfStudent(ctx context.Context, sessionID, studentID string) (*Attempt, error) {
	var att *Attempt
	err := s.store.View(ctx, func(tx Tx) error {
		p, err := tx.GetParticipantByStudent(sessionID, NormalizeStudentID(studentID))
		if err != nil {
			return err
		}
		att, err = tx.GetAttemptByParticipant(p.ID)
		return err
	})
	return att, err
}

// spareStrikes reverts the newest strikes of a readmitted attempt until one
// strike is left before the limit: readmitting means one more chance, not an
// annulment on the next signal. Reverted strikes stay in the record.
func (s *Service) spareStrikes(tx Tx, sess *Session, att *Attempt) error {
	pub, err := s.published(tx, sess.ExamHash)
	if err != nil {
		return err
	}
	limit := ProctorSettings(&pub.Exam.Manifest).StrikeLimit
	if limit <= 0 {
		return nil
	}
	ps, err := tx.ListPenalties(att.ID)
	if err != nil {
		return err
	}
	now := s.clock()
	for i := len(ps) - 1; i >= 0 && ActiveStrikes(ps) >= limit; i-- {
		if p := ps[i]; p.Kind == PenaltyStrike && p.Active() {
			if err := tx.RevertPenalty(p.ID, now, "readmitido"); err != nil {
				return err
			}
			p.RevertedAt = now
		}
	}
	return nil
}
