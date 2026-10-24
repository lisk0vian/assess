package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/session"
)

type tx struct {
	ctx context.Context
	tx  *sql.Tx
}

func (t *tx) exec(query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(t.ctx, query, args...)
}

func (t *tx) query(query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(t.ctx, query, args...)
}

func (t *tx) row(query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(t.ctx, query, args...)
}

// notFound maps sql.ErrNoRows to session.ErrNotFound.
func notFound(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", what, session.ErrNotFound)
	}
	return err
}

// Time columns are Unix milliseconds; zero times are NULL.
func ms(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UnixMilli(), Valid: true}
}

func fromMS(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return time.UnixMilli(v.Int64).UTC()
}

// ---- published exams ----

func (t *tx) PutPublished(p *exam.Published) error {
	content, err := json.Marshal(p.Exam)
	if err != nil {
		return err
	}
	res, err := t.exec(`INSERT INTO published_exams (hash, exam_id, title, content, created_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT (hash) DO NOTHING`,
		p.Hash, p.Exam.Manifest.ID, p.Exam.Manifest.Title, content, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // same content already published; assets are identical too
	}
	for path, data := range p.Assets {
		if _, err := t.exec(`INSERT INTO published_assets (exam_hash, path, content) VALUES (?, ?, ?)`,
			p.Hash, path, data); err != nil {
			return err
		}
	}
	return nil
}

func (t *tx) GetPublished(hash string) (*exam.Published, error) {
	var content []byte
	if err := t.row(`SELECT content FROM published_exams WHERE hash = ?`, hash).Scan(&content); err != nil {
		return nil, notFound(err, "published exam "+hash)
	}
	p := &exam.Published{Hash: hash, Exam: &exam.Exam{}, Assets: map[string][]byte{}}
	if err := json.Unmarshal(content, p.Exam); err != nil {
		return nil, fmt.Errorf("decode published exam %s: %w", hash, err)
	}
	rows, err := t.query(`SELECT path, content FROM published_assets WHERE exam_hash = ?`, hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var path string
		var data []byte
		if err := rows.Scan(&path, &data); err != nil {
			return nil, err
		}
		p.Assets[path] = data
	}
	return p, rows.Err()
}

// ---- sessions ----

const sessionCols = `id, code, exam_hash, state, duration_ms, grace_ms, created_at, started_at, ends_at, ended_at`

func scanSession(sc interface{ Scan(...any) error }) (*session.Session, error) {
	var s session.Session
	var state string
	var dur, grace int64
	var created, started, ends, ended sql.NullInt64
	if err := sc.Scan(&s.ID, &s.Code, &s.ExamHash, &state, &dur, &grace, &created, &started, &ends, &ended); err != nil {
		return nil, err
	}
	s.State = session.State(state)
	s.Duration, s.Grace = time.Duration(dur)*time.Millisecond, time.Duration(grace)*time.Millisecond
	s.CreatedAt, s.StartedAt, s.EndsAt, s.EndedAt = fromMS(created), fromMS(started), fromMS(ends), fromMS(ended)
	return &s, nil
}

func (t *tx) InsertSession(s *session.Session) error {
	_, err := t.exec(`INSERT INTO sessions (`+sessionCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Code, s.ExamHash, string(s.State), s.Duration.Milliseconds(), s.Grace.Milliseconds(),
		ms(s.CreatedAt), ms(s.StartedAt), ms(s.EndsAt), ms(s.EndedAt))
	return err
}

func (t *tx) UpdateSession(s *session.Session) error {
	_, err := t.exec(`UPDATE sessions SET state = ?, duration_ms = ?, grace_ms = ?, started_at = ?, ends_at = ?, ended_at = ?
		WHERE id = ?`,
		string(s.State), s.Duration.Milliseconds(), s.Grace.Milliseconds(), ms(s.StartedAt), ms(s.EndsAt), ms(s.EndedAt), s.ID)
	return err
}

func (t *tx) GetSession(id string) (*session.Session, error) {
	s, err := scanSession(t.row(`SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id))
	return s, notFound(err, "session "+id)
}

func (t *tx) GetOpenSessionByCode(code string) (*session.Session, error) {
	s, err := scanSession(t.row(`SELECT `+sessionCols+` FROM sessions WHERE code = ? AND state <> 'ended'`, code))
	return s, notFound(err, "session code "+code)
}

func (t *tx) ListOpenSessions() ([]*session.Session, error) {
	rows, err := t.query(`SELECT ` + sessionCols + ` FROM sessions WHERE state <> 'ended' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*session.Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---- participants ----

const participantCols = `id, session_id, student_id, name, source, created_at`

func scanParticipant(sc interface{ Scan(...any) error }) (*session.Participant, error) {
	var p session.Participant
	var src string
	var created sql.NullInt64
	if err := sc.Scan(&p.ID, &p.SessionID, &p.StudentID, &p.Name, &src, &created); err != nil {
		return nil, err
	}
	p.Source, p.CreatedAt = session.ParticipantSource(src), fromMS(created)
	return &p, nil
}

func (t *tx) InsertParticipant(p *session.Participant) error {
	_, err := t.exec(`INSERT INTO participants (`+participantCols+`) VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.SessionID, p.StudentID, p.Name, string(p.Source), ms(p.CreatedAt))
	return err
}

func (t *tx) GetParticipant(id string) (*session.Participant, error) {
	p, err := scanParticipant(t.row(`SELECT `+participantCols+` FROM participants WHERE id = ?`, id))
	return p, notFound(err, "participant "+id)
}

func (t *tx) GetParticipantByStudent(sessionID, studentID string) (*session.Participant, error) {
	p, err := scanParticipant(t.row(`SELECT `+participantCols+` FROM participants WHERE session_id = ? AND student_id = ?`,
		sessionID, studentID))
	return p, notFound(err, "student "+studentID)
}

func (t *tx) ListParticipants(sessionID string) ([]*session.Participant, error) {
	rows, err := t.query(`SELECT `+participantCols+` FROM participants WHERE session_id = ? ORDER BY name, student_id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*session.Participant
	for rows.Next() {
		p, err := scanParticipant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- attempts ----

const attemptCols = `id, session_id, participant_id, seed, layout, state, extra_ms, created_at,
	submitted_at, submit_reason, score, max_score, last_seq`

func scanAttempt(sc interface{ Scan(...any) error }) (*session.Attempt, error) {
	var a session.Attempt
	var seed, extra int64
	var layout []byte
	var state string
	var created, submitted sql.NullInt64
	var reason sql.NullString
	var score sql.NullFloat64
	if err := sc.Scan(&a.ID, &a.SessionID, &a.ParticipantID, &seed, &layout, &state, &extra, &created,
		&submitted, &reason, &score, &a.MaxScore, &a.LastSeq); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(layout, &a.Layout); err != nil {
		return nil, fmt.Errorf("decode layout of attempt %s: %w", a.ID, err)
	}
	a.Seed = uint64(seed) //nolint:gosec // G115: seeds are stored as their int64 bit pattern
	a.State = session.AttemptState(state)
	a.Extra = time.Duration(extra) * time.Millisecond
	a.CreatedAt, a.SubmittedAt = fromMS(created), fromMS(submitted)
	a.SubmitReason = session.SubmitReason(reason.String)
	a.Score = score.Float64
	return &a, nil
}

func (t *tx) InsertAttempt(a *session.Attempt) error {
	layout, err := json.Marshal(a.Layout)
	if err != nil {
		return err
	}
	_, err = t.exec(`INSERT INTO attempts (`+attemptCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.SessionID, a.ParticipantID, int64(a.Seed), layout, //nolint:gosec // G115: bit pattern, see scanAttempt
		string(a.State), a.Extra.Milliseconds(),
		ms(a.CreatedAt), ms(a.SubmittedAt), nullString(string(a.SubmitReason)), nullScore(a), a.MaxScore, a.LastSeq)
	return err
}

func (t *tx) UpdateAttempt(a *session.Attempt) error {
	_, err := t.exec(`UPDATE attempts SET state = ?, extra_ms = ?, submitted_at = ?, submit_reason = ?, score = ?, last_seq = ?
		WHERE id = ?`,
		string(a.State), a.Extra.Milliseconds(), ms(a.SubmittedAt), nullString(string(a.SubmitReason)), nullScore(a), a.LastSeq, a.ID)
	return err
}

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

// nullScore stores a score only once the attempt has been graded.
func nullScore(a *session.Attempt) sql.NullFloat64 {
	return sql.NullFloat64{Float64: a.Score, Valid: !a.SubmittedAt.IsZero()}
}

func (t *tx) GetAttempt(id string) (*session.Attempt, error) {
	a, err := scanAttempt(t.row(`SELECT `+attemptCols+` FROM attempts WHERE id = ?`, id))
	return a, notFound(err, "attempt "+id)
}

func (t *tx) GetAttemptByParticipant(participantID string) (*session.Attempt, error) {
	a, err := scanAttempt(t.row(`SELECT `+attemptCols+` FROM attempts WHERE participant_id = ?`, participantID))
	return a, notFound(err, "attempt of participant "+participantID)
}

func (t *tx) ListAttempts(sessionID string) ([]*session.Attempt, error) {
	rows, err := t.query(`SELECT `+attemptCols+` FROM attempts WHERE session_id = ? ORDER BY created_at, id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*session.Attempt
	for rows.Next() {
		a, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- answers ----

func (t *tx) UpsertAnswer(a *session.Answer) (bool, error) {
	resp, err := json.Marshal(a.Response)
	if err != nil {
		return false, err
	}
	res, err := t.exec(`INSERT INTO answers (attempt_id, question_id, response, seq, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (attempt_id, question_id) DO UPDATE
		SET response = excluded.response, seq = excluded.seq, updated_at = excluded.updated_at
		WHERE excluded.seq > answers.seq`,
		a.AttemptID, a.QuestionID, resp, a.Seq, ms(a.UpdatedAt))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (t *tx) ListAnswers(attemptID string) ([]*session.Answer, error) {
	rows, err := t.query(`SELECT question_id, response, seq, updated_at, points, correct
		FROM answers WHERE attempt_id = ? ORDER BY question_id`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*session.Answer
	for rows.Next() {
		a := &session.Answer{AttemptID: attemptID}
		var resp []byte
		var updated sql.NullInt64
		var points sql.NullFloat64
		var correct sql.NullBool
		if err := rows.Scan(&a.QuestionID, &resp, &a.Seq, &updated, &points, &correct); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(resp, &a.Response); err != nil {
			return nil, fmt.Errorf("decode answer %s/%s: %w", attemptID, a.QuestionID, err)
		}
		a.UpdatedAt = fromMS(updated)
		a.Graded, a.Points, a.Correct = points.Valid, points.Float64, correct.Bool
		out = append(out, a)
	}
	return out, rows.Err()
}

func (t *tx) SetAnswerGrade(attemptID, questionID string, points float64, correct bool) error {
	_, err := t.exec(`UPDATE answers SET points = ?, correct = ? WHERE attempt_id = ? AND question_id = ?`,
		points, correct, attemptID, questionID)
	return err
}

// ---- events ----

func (t *tx) InsertEvent(e *session.Event) error {
	var data []byte
	if len(e.Data) > 0 {
		var err error
		if data, err = json.Marshal(e.Data); err != nil {
			return err
		}
	}
	res, err := t.exec(`INSERT INTO events (session_id, attempt_id, kind, severity, question_id, data, client_at, received_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.SessionID, nullString(e.AttemptID), e.Kind, string(e.Severity), nullString(e.QuestionID), data,
		ms(e.ClientAt), ms(e.ReceivedAt))
	if err != nil {
		return err
	}
	e.ID, err = res.LastInsertId()
	return err
}

func (t *tx) ListEvents(sessionID string) ([]*session.Event, error) {
	rows, err := t.query(`SELECT id, attempt_id, kind, severity, question_id, data, client_at, received_at
		FROM events WHERE session_id = ? ORDER BY received_at, id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*session.Event
	for rows.Next() {
		e := &session.Event{SessionID: sessionID}
		var attempt, question sql.NullString
		var sev string
		var data []byte
		var client, received sql.NullInt64
		if err := rows.Scan(&e.ID, &attempt, &e.Kind, &sev, &question, &data, &client, &received); err != nil {
			return nil, err
		}
		e.AttemptID, e.QuestionID, e.Severity = attempt.String, question.String, session.Severity(sev)
		e.ClientAt, e.ReceivedAt = fromMS(client), fromMS(received)
		if len(data) > 0 {
			if err := json.Unmarshal(data, &e.Data); err != nil {
				return nil, fmt.Errorf("decode event %d: %w", e.ID, err)
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
