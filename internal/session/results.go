package session

import (
	"context"
	"sort"
	"time"
)

// ResultRow is one participant's outcome, ready for export.
type ResultRow struct {
	StudentID    string             `json:"student_id"`
	Name         string             `json:"name"`
	State        AttemptState       `json:"state"`
	Score        *float64           `json:"score"` // nil until submitted
	MaxScore     float64            `json:"max_score"`
	SubmittedAt  time.Time          `json:"submitted_at,omitzero"`
	SubmitReason SubmitReason       `json:"submit_reason,omitempty"`
	Minutes      float64            `json:"minutes,omitempty"` // from the session start to submission
	Points       map[string]float64 `json:"points"`            // by question id, questions this attempt received
}

// Results is the export of one session.
type Results struct {
	SessionID string      `json:"session_id"`
	Code      string      `json:"code"`
	ExamID    string      `json:"exam_id"`
	ExamTitle string      `json:"exam_title"`
	State     State       `json:"state"`
	Questions []string    `json:"questions"` // every question id of the exam, sorted
	Rows      []ResultRow `json:"rows"`
}

// LatestSession returns the most recently created session.
func (s *Service) LatestSession(ctx context.Context) (*Session, error) {
	var sess *Session
	err := s.store.View(ctx, func(tx Tx) error {
		all, err := tx.ListSessions()
		if err != nil {
			return err
		}
		if len(all) == 0 {
			return ErrNotFound
		}
		sess = all[len(all)-1]
		return nil
	})
	return sess, err
}

// Results builds the export of sessionID, ordered by student name.
func (s *Service) Results(ctx context.Context, sessionID string) (*Results, error) {
	snap, err := s.Snapshot(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	_, pub, err := s.Session(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	r := &Results{
		SessionID: sessionID, Code: snap.Session.Code, ExamID: pub.Exam.Manifest.ID,
		ExamTitle: pub.Exam.Manifest.Title, State: snap.Session.State,
	}
	for id := range pub.Exam.Questions {
		r.Questions = append(r.Questions, id)
	}
	sort.Strings(r.Questions)

	for _, a := range snap.Attempts {
		p := snap.Participants[a.ParticipantID]
		row := ResultRow{StudentID: p.StudentID, Name: p.Name, State: a.State, MaxScore: a.MaxScore,
			SubmittedAt: a.SubmittedAt, SubmitReason: a.SubmitReason, Points: map[string]float64{}}
		if !a.SubmittedAt.IsZero() {
			score := a.Score
			row.Score = &score
			if !snap.Session.StartedAt.IsZero() {
				row.Minutes = float64(a.SubmittedAt.Sub(snap.Session.StartedAt).Round(time.Second)) / float64(time.Minute)
			}
		}
		graded := map[string]float64{}
		for _, ans := range snap.Answers[a.ID] {
			if ans.Graded {
				graded[ans.QuestionID] = ans.Points
			}
		}
		for _, sl := range a.Layout {
			row.Points[sl.QuestionID] = graded[sl.QuestionID]
		}
		r.Rows = append(r.Rows, row)
	}
	sort.SliceStable(r.Rows, func(i, j int) bool { return r.Rows[i].Name < r.Rows[j].Name })
	return r, nil
}
