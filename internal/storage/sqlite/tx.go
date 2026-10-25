package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

const sessionCols = `id, code, exam_hash, state, duration_ms, grace_ms, created_at, started_at, ends_at, ended_at, secret`

func scanSession(sc interface{ Scan(...any) error }) (*session.Session, error) {
	var s session.Session
	var state string
	var dur, grace int64
	var created, started, ends, ended sql.NullInt64
	var secret sql.NullString
	if err := sc.Scan(&s.ID, &s.Code, &s.ExamHash, &state, &dur, &grace, &created, &started, &ends, &ended, &secret); err != nil {
		return nil, err
	}
	s.Secret = secret.String
	s.State = session.State(state)
	s.Duration, s.Grace = time.Duration(dur)*time.Millisecond, time.Duration(grace)*time.Millisecond
	s.CreatedAt, s.StartedAt, s.EndsAt, s.EndedAt = fromMS(created), fromMS(started), fromMS(ends), fromMS(ended)
	return &s, nil
}

func (t *tx) InsertSession(s *session.Session) error {
	_, err := t.exec(`INSERT INTO sessions (`+sessionCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Code, s.ExamHash, string(s.State), s.Duration.Milliseconds(), s.Grace.Milliseconds(),
		ms(s.CreatedAt), ms(s.StartedAt), ms(s.EndsAt), ms(s.EndedAt), nullString(s.Secret))
	return err
}

func (t *tx) UpdateSession(s *session.Session) error {
	_, err := t.exec(`UPDATE sessions SET state = ?, duration_ms = ?, grace_ms = ?, started_at = ?, ends_at = ?, ended_at = ?, secret = ?
		WHERE id = ?`,
		string(s.State), s.Duration.Milliseconds(), s.Grace.Milliseconds(), ms(s.StartedAt), ms(s.EndsAt), ms(s.EndedAt), nullString(s.Secret), s.ID)
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
	return t.listSessions(`WHERE state <> 'ended'`)
}

func (t *tx) ListSessions() ([]*session.Session, error) { return t.listSessions("") }

func (t *tx) listSessions(where string) ([]*session.Session, error) {
	rows, err := t.query(`SELECT ` + sessionCols + ` FROM sessions ` + where + ` ORDER BY created_at, id`)
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

func (t *tx) DeleteParticipant(id string) error {
	_, err := t.exec(`DELETE FROM participants WHERE id = ?`, id)
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
	submitted_at, submit_reason, score, max_score, last_seq, signal_seq, pledged_at`

func scanAttempt(sc interface{ Scan(...any) error }) (*session.Attempt, error) {
	var a session.Attempt
	var seed, extra int64
	var layout []byte
	var state string
	var created, submitted, pledged sql.NullInt64
	var reason sql.NullString
	var score sql.NullFloat64
	if err := sc.Scan(&a.ID, &a.SessionID, &a.ParticipantID, &seed, &layout, &state, &extra, &created,
		&submitted, &reason, &score, &a.MaxScore, &a.LastSeq, &a.SignalSeq, &pledged); err != nil {
		return nil, err
	}
	a.PledgedAt = fromMS(pledged)
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
	_, err = t.exec(`INSERT INTO attempts (`+attemptCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.SessionID, a.ParticipantID, int64(a.Seed), layout, //nolint:gosec // G115: bit pattern, see scanAttempt
		string(a.State), a.Extra.Milliseconds(),
		ms(a.CreatedAt), ms(a.SubmittedAt), nullString(string(a.SubmitReason)), nullScore(a), a.MaxScore, a.LastSeq, a.SignalSeq, ms(a.PledgedAt))
	return err
}

func (t *tx) UpdateAttempt(a *session.Attempt) error {
	_, err := t.exec(`UPDATE attempts SET state = ?, extra_ms = ?, submitted_at = ?, submit_reason = ?, score = ?, last_seq = ?,
		signal_seq = ?, pledged_at = ? WHERE id = ?`,
		string(a.State), a.Extra.Milliseconds(), ms(a.SubmittedAt), nullString(string(a.SubmitReason)), nullScore(a), a.LastSeq,
		a.SignalSeq, ms(a.PledgedAt), a.ID)
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

// ---- penalties ----

const penaltyCols = `id, attempt_id, event_id, kind, signal, question_id, amount, created_at, reverted_at, note`

func scanPenalty(sc interface{ Scan(...any) error }) (*session.Penalty, error) {
	var p session.Penalty
	var event, created, reverted sql.NullInt64
	var kind string
	var question, note sql.NullString
	if err := sc.Scan(&p.ID, &p.AttemptID, &event, &kind, &p.Signal, &question, &p.Amount, &created, &reverted, &note); err != nil {
		return nil, err
	}
	p.EventID, p.Kind, p.QuestionID, p.Note = event.Int64, session.PenaltyKind(kind), question.String, note.String
	p.CreatedAt, p.RevertedAt = fromMS(created), fromMS(reverted)
	return &p, nil
}

func (t *tx) InsertPenalty(p *session.Penalty) error {
	event := sql.NullInt64{Int64: p.EventID, Valid: p.EventID > 0}
	res, err := t.exec(`INSERT INTO penalties (attempt_id, event_id, kind, signal, question_id, amount, created_at, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.AttemptID, event, string(p.Kind), p.Signal, nullString(p.QuestionID), p.Amount, ms(p.CreatedAt), nullString(p.Note))
	if err != nil {
		return err
	}
	p.ID, err = res.LastInsertId()
	return err
}

func (t *tx) ListPenalties(attemptID string) ([]*session.Penalty, error) {
	rows, err := t.query(`SELECT `+penaltyCols+` FROM penalties WHERE attempt_id = ? ORDER BY id`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*session.Penalty
	for rows.Next() {
		p, err := scanPenalty(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (t *tx) ListSessionPenalties(sessionID string) ([]*session.Penalty, error) {
	rows, err := t.query(`SELECT p.`+strings.ReplaceAll(penaltyCols, ", ", ", p.")+` FROM penalties p
		JOIN attempts a ON a.id = p.attempt_id WHERE a.session_id = ? ORDER BY p.id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*session.Penalty
	for rows.Next() {
		p, err := scanPenalty(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (t *tx) GetPenalty(id int64) (*session.Penalty, error) {
	p, err := scanPenalty(t.row(`SELECT `+penaltyCols+` FROM penalties WHERE id = ?`, id))
	return p, notFound(err, fmt.Sprintf("penalty %d", id))
}

func (t *tx) RevertPenalty(id int64, at time.Time, note string) error {
	_, err := t.exec(`UPDATE penalties SET reverted_at = ?, note = ? WHERE id = ? AND reverted_at IS NULL`, ms(at), nullString(note), id)
	return err
}

func (t *tx) OpenQuestion(attemptID, questionID string, at time.Time) (time.Time, error) {
	if _, err := t.exec(`INSERT INTO question_opens (attempt_id, question_id, opened_at) VALUES (?, ?, ?)
		ON CONFLICT (attempt_id, question_id) DO NOTHING`, attemptID, questionID, ms(at)); err != nil {
		return time.Time{}, err
	}
	var opened sql.NullInt64
	err := t.row(`SELECT opened_at FROM question_opens WHERE attempt_id = ? AND question_id = ?`, attemptID, questionID).Scan(&opened)
	return fromMS(opened), err
}

func (t *tx) ListOpened(attemptID string) (map[string]time.Time, error) {
	rows, err := t.query(`SELECT question_id, opened_at FROM question_opens WHERE attempt_id = ?`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var q string
		var at sql.NullInt64
		if err := rows.Scan(&q, &at); err != nil {
			return nil, err
		}
		out[q] = fromMS(at)
	}
	return out, rows.Err()
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

func (t *tx) CountEvents(attemptID, kind string) (int, error) {
	var n int
	err := t.row(`SELECT COUNT(*) FROM events WHERE attempt_id = ? AND kind = ?`, attemptID, kind).Scan(&n)
	return n, err
}

func (t *tx) RecentEvents(sessionID, prefix string, limit int) ([]*session.Event, error) {
	return t.events(`WHERE session_id = ? AND kind LIKE ? ESCAPE '\' ORDER BY id DESC LIMIT ?`,
		sessionID, sessionID, likePrefix(prefix), limit)
}

func likePrefix(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)
	return r.Replace(s) + "%"
}

func (t *tx) SignalCounts(sessionID string) (map[string]int, error) {
	rows, err := t.query(`SELECT attempt_id, COUNT(*) FROM events
		WHERE session_id = ? AND kind LIKE 'signal.%' AND severity IN ('warn', 'high') AND attempt_id IS NOT NULL
		GROUP BY attempt_id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (t *tx) ListEvents(sessionID string) ([]*session.Event, error) {
	return t.events(`WHERE session_id = ? ORDER BY received_at, id`, sessionID, sessionID)
}

// events runs a query over the events of sessionID; where carries the
// conditions and order, args its parameters.
func (t *tx) events(where, sessionID string, args ...any) ([]*session.Event, error) {
	rows, err := t.query(`SELECT id, attempt_id, kind, severity, question_id, data, client_at, received_at
		FROM events `+where, args...)
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
