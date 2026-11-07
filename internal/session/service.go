package session

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/grading"
)

// Domain errors returned by Service.
var (
	ErrSessionClosed   = errors.New("session is closed")
	ErrNotStarted      = errors.New("session has not started")
	ErrAlreadyStarted  = errors.New("session has already started")
	ErrAttemptClosed   = errors.New("attempt is closed")
	ErrTimeUp          = errors.New("time is up")
	ErrAlreadyJoined   = errors.New("student already has an attempt in this session")
	ErrDuplicate       = errors.New("student is already registered in this session")
	ErrUnknownQuestion = errors.New("question is not part of this attempt")
	ErrInvalidInput    = errors.New("invalid input")
)

// DefaultGrace is how long after the deadline answers are still accepted, to
// absorb network latency and queued offline changes.
const DefaultGrace = 30 * time.Second

// Service enforces session rules on top of a Store. Time always comes from
// the server clock.
type Service struct {
	store  Store
	now    func() time.Time
	notify func(sessionID string)

	mu   sync.Mutex
	pubs map[string]*exam.Published // immutable, so safe to cache by hash
}

// Option configures a Service.
type Option func(*Service)

// WithClock overrides the clock, for tests.
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithNotify registers fn to be called (outside any transaction) after a
// change that clients should see: start, extra time, submissions, end.
func WithNotify(fn func(sessionID string)) Option { return func(s *Service) { s.notify = fn } }

func (s *Service) changed(sessionID string) {
	if s.notify != nil && sessionID != "" {
		s.notify(sessionID)
	}
}

// NewService returns a Service backed by store.
func NewService(store Store, opts ...Option) *Service {
	s := &Service{store: store, now: time.Now, pubs: map[string]*exam.Published{}}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Millisecond) }

func (s *Service) published(tx Tx, hash string) (*exam.Published, error) {
	s.mu.Lock()
	p, ok := s.pubs[hash]
	s.mu.Unlock()
	if ok {
		return p, nil
	}
	p, err := tx.GetPublished(hash)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.pubs[hash] = p
	s.mu.Unlock()
	return p, nil
}

// CreateOptions configures a new session.
type CreateOptions struct {
	Duration time.Duration // default: the manifest duration
	Grace    time.Duration // default: DefaultGrace; negative means none
	// ClassID registers the members of a class before anyone joins.
	ClassID    string
	RosterOnly bool // only the class may join without approval
}

// CreateSession stores pub (if new) and opens a session in the lobby.
func (s *Service) CreateSession(ctx context.Context, pub *exam.Published, opts CreateOptions) (*Session, error) {
	if opts.Duration == 0 {
		opts.Duration = pub.Exam.Manifest.Duration.D()
	}
	if opts.Duration <= 0 || opts.Duration > exam.MaxDuration {
		return nil, fmt.Errorf("%w: duration must be between 1s and %s", ErrInvalidInput, exam.MaxDuration)
	}
	switch {
	case opts.Grace == 0:
		opts.Grace = DefaultGrace
	case opts.Grace < 0:
		opts.Grace = 0
	}
	now := s.clock()
	sess := &Session{
		ID:         newID(),
		ExamHash:   pub.Hash,
		State:      StateLobby,
		Duration:   opts.Duration,
		Grace:      opts.Grace,
		CreatedAt:  now,
		Secret:     newSecret(),
		ClassID:    opts.ClassID,
		RosterOnly: opts.RosterOnly && opts.ClassID != "",
	}
	err := s.store.Update(ctx, func(tx Tx) error {
		if err := tx.PutPublished(pub); err != nil {
			return err
		}
		code, err := uniqueCode(tx)
		if err != nil {
			return err
		}
		sess.Code = code
		if err := tx.InsertSession(sess); err != nil {
			return err
		}
		if err := registerClass(tx, sess, now); err != nil {
			return err
		}
		return tx.InsertEvent(&Event{SessionID: sess.ID, Kind: "session.created", Severity: SeverityInfo,
			Data: map[string]any{"exam": pub.Exam.Manifest.ID, "hash": pub.Hash, "duration_ms": opts.Duration.Milliseconds()}, ReceivedAt: now})
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.pubs[pub.Hash] = pub
	s.mu.Unlock()
	return sess, nil
}

func uniqueCode(tx Tx) (string, error) {
	for range 20 {
		code := newCode()
		if _, err := tx.GetOpenSessionByCode(code); errors.Is(err, ErrNotFound) {
			return code, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("could not allocate a session code")
}

// NormalizeStudentID trims, uppercases and removes inner spaces so "a 123"
// and "A123" identify the same student.
func NormalizeStudentID(id string) string {
	return strings.ToUpper(strings.Join(strings.Fields(id), ""))
}

func checkIdentity(studentID, name string) (string, string, error) {
	studentID = NormalizeStudentID(studentID)
	name = strings.Join(strings.Fields(name), " ")
	switch {
	case studentID == "" || utf8.RuneCountInString(studentID) > 64:
		return "", "", fmt.Errorf("%w: student id must have 1 to 64 characters", ErrInvalidInput)
	case name == "" || utf8.RuneCountInString(name) > 100:
		return "", "", fmt.Errorf("%w: name must have 1 to 100 characters", ErrInvalidInput)
	}
	return studentID, name, nil
}

func openSession(tx Tx, id string) (*Session, error) {
	sess, err := tx.GetSession(id)
	if err != nil {
		return nil, err
	}
	if sess.State == StateEnded {
		return nil, ErrSessionClosed
	}
	return sess, nil
}

// AddParticipant pre-registers a student (roster import or teacher action).
func (s *Service) AddParticipant(ctx context.Context, sessionID, studentID, name string, src ParticipantSource) (*Participant, error) {
	studentID, name, err := checkIdentity(studentID, name)
	if err != nil {
		return nil, err
	}
	var p *Participant
	err = s.store.Update(ctx, func(tx Tx) error {
		if _, err := openSession(tx, sessionID); err != nil {
			return err
		}
		if _, err := tx.GetParticipantByStudent(sessionID, studentID); err == nil {
			return ErrDuplicate
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		p = &Participant{ID: newID(), SessionID: sessionID, StudentID: studentID, Name: name, Source: src, CreatedAt: s.clock()}
		if err := tx.InsertParticipant(p); err != nil {
			return err
		}
		return ensureStudent(tx, p)
	})
	return p, err
}

// Join registers the student if needed and creates their attempt. A student
// who already has an attempt gets ErrAlreadyJoined; resuming it is the
// caller's decision (same device cookie, or teacher approval).
func (s *Service) Join(ctx context.Context, sessionID, studentID, name string) (*Attempt, *Participant, error) {
	studentID, name, err := checkIdentity(studentID, name)
	if err != nil {
		return nil, nil, err
	}
	var att *Attempt
	var part *Participant
	err = s.store.Update(ctx, func(tx Tx) error {
		sess, err := openSession(tx, sessionID)
		if err != nil {
			return err
		}
		now := s.clock()
		part, err = tx.GetParticipantByStudent(sessionID, studentID)
		switch {
		case errors.Is(err, ErrNotFound):
			part = &Participant{ID: newID(), SessionID: sessionID, StudentID: studentID, Name: name, Source: SourceSelf, CreatedAt: now}
			if err := tx.InsertParticipant(part); err != nil {
				return err
			}
			if err := ensureStudent(tx, part); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if _, err := tx.GetAttemptByParticipant(part.ID); err == nil {
				return ErrAlreadyJoined
			} else if !errors.Is(err, ErrNotFound) {
				return err
			}
		}

		pub, err := s.published(tx, sess.ExamHash)
		if err != nil {
			return err
		}
		seed := randomUint64()
		layout := pub.Exam.Assemble(seed)
		var maxScore float64
		for _, sl := range layout {
			maxScore += sl.Points
		}
		att = &Attempt{
			ID: newID(), SessionID: sessionID, ParticipantID: part.ID,
			Seed: seed, Layout: layout, State: AttemptActive, CreatedAt: now, MaxScore: maxScore,
		}
		if err := tx.InsertAttempt(att); err != nil {
			return err
		}
		return tx.InsertEvent(&Event{SessionID: sessionID, AttemptID: att.ID, Kind: "attempt.joined", Severity: SeverityInfo,
			Data: map[string]any{"student_id": studentID}, ReceivedAt: now})
	})
	if err != nil {
		return nil, nil, err
	}
	s.changed(sessionID)
	return att, part, nil
}

// Start moves the session from the lobby to running; everyone's clock starts now.
func (s *Service) Start(ctx context.Context, sessionID string) (*Session, error) {
	var sess *Session
	err := s.store.Update(ctx, func(tx Tx) error {
		var err error
		if sess, err = openSession(tx, sessionID); err != nil {
			return err
		}
		if sess.State != StateLobby {
			return ErrAlreadyStarted
		}
		now := s.clock()
		sess.State, sess.StartedAt, sess.EndsAt = StateRunning, now, now.Add(sess.Duration)
		if err := tx.UpdateSession(sess); err != nil {
			return err
		}
		return tx.InsertEvent(&Event{SessionID: sessionID, Kind: "session.started", Severity: SeverityInfo,
			Data: map[string]any{"ends_at": sess.EndsAt.UnixMilli()}, ReceivedAt: now})
	})
	if err == nil {
		s.changed(sessionID)
	}
	return sess, err
}

// ExtendSession adds d to everyone's time.
func (s *Service) ExtendSession(ctx context.Context, sessionID string, d time.Duration) (*Session, error) {
	if d <= 0 {
		return nil, fmt.Errorf("%w: extra time must be positive", ErrInvalidInput)
	}
	var sess *Session
	err := s.store.Update(ctx, func(tx Tx) error {
		var err error
		if sess, err = openSession(tx, sessionID); err != nil {
			return err
		}
		if sess.State != StateRunning {
			return ErrNotStarted
		}
		sess.EndsAt = sess.EndsAt.Add(d)
		if err := tx.UpdateSession(sess); err != nil {
			return err
		}
		return tx.InsertEvent(&Event{SessionID: sessionID, Kind: "session.extended", Severity: SeverityInfo,
			Data: map[string]any{"extra_ms": d.Milliseconds(), "ends_at": sess.EndsAt.UnixMilli()}, ReceivedAt: s.clock()})
	})
	if err == nil {
		s.changed(sessionID)
	}
	return sess, err
}

// ExtendAttempt adds d to one student's time.
func (s *Service) ExtendAttempt(ctx context.Context, attemptID string, d time.Duration) (*Attempt, error) {
	if d <= 0 {
		return nil, fmt.Errorf("%w: extra time must be positive", ErrInvalidInput)
	}
	var att *Attempt
	err := s.store.Update(ctx, func(tx Tx) error {
		var err error
		if att, err = tx.GetAttempt(attemptID); err != nil {
			return err
		}
		if att.State != AttemptActive {
			return ErrAttemptClosed
		}
		att.Extra += d
		if err := tx.UpdateAttempt(att); err != nil {
			return err
		}
		return tx.InsertEvent(&Event{SessionID: att.SessionID, AttemptID: att.ID, Kind: "attempt.extended", Severity: SeverityInfo,
			Data: map[string]any{"extra_ms": d.Milliseconds()}, ReceivedAt: s.clock()})
	})
	if err == nil {
		s.changed(att.SessionID)
	}
	return att, err
}

// SaveAnswer records a response change. Changes carry a client sequence
// number so retries and out-of-order deliveries are harmless: an older or
// repeated seq is ignored (applied=false, no error).
func (s *Service) SaveAnswer(ctx context.Context, attemptID, questionID string, seq int64, r grading.Response) (applied bool, err error) {
	if seq <= 0 {
		return false, fmt.Errorf("%w: sequence number must be positive", ErrInvalidInput)
	}
	err = s.store.Update(ctx, func(tx Tx) error {
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
		if err := grading.Validate(pub.Exam.Questions[questionID], r); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidInput, err)
		}
		penalties, err := tx.ListPenalties(attemptID)
		if err != nil {
			return err
		}
		if voided(penalties)[questionID] {
			return fmt.Errorf("%w: voided", ErrQuestionClosed)
		}
		now := s.clock()
		closes, err := questionCloses(tx, pub, attemptID, questionID, now)
		if err != nil {
			return err
		}
		if !closes.IsZero() && now.After(closes.Add(questionGrace)) {
			return fmt.Errorf("%w: time limit", ErrQuestionClosed)
		}
		applied, err = tx.UpsertAnswer(&Answer{AttemptID: attemptID, QuestionID: questionID, Response: r, Seq: seq, UpdatedAt: now})
		if err != nil {
			return err
		}
		if seq > att.LastSeq {
			att.LastSeq = seq
			return tx.UpdateAttempt(att)
		}
		return nil
	})
	return applied, err
}

// activeAttempt loads an attempt that may still receive answers.
func (s *Service) activeAttempt(tx Tx, attemptID string) (*Attempt, *Session, error) {
	att, err := tx.GetAttempt(attemptID)
	if err != nil {
		return nil, nil, err
	}
	if att.State != AttemptActive {
		return nil, nil, ErrAttemptClosed
	}
	sess, err := tx.GetSession(att.SessionID)
	if err != nil {
		return nil, nil, err
	}
	switch sess.State {
	case StateLobby:
		return nil, nil, ErrNotStarted
	case StateEnded:
		return nil, nil, ErrSessionClosed
	}
	if s.clock().After(att.Deadline(sess).Add(sess.Grace)) {
		return nil, nil, ErrTimeUp
	}
	return att, sess, nil
}

func inLayout(att *Attempt, questionID string) bool {
	for _, sl := range att.Layout {
		if sl.QuestionID == questionID {
			return true
		}
	}
	return false
}

// Submit closes an attempt at the student's request and grades it.
func (s *Service) Submit(ctx context.Context, attemptID string) (*Attempt, error) {
	var att *Attempt
	err := s.store.Update(ctx, func(tx Tx) error {
		var err error
		if att, err = tx.GetAttempt(attemptID); err != nil {
			return err
		}
		if att.State != AttemptActive {
			return ErrAttemptClosed
		}
		sess, err := tx.GetSession(att.SessionID)
		if err != nil {
			return err
		}
		if sess.State == StateLobby {
			return ErrNotStarted
		}
		reason := SubmitManual
		if s.clock().After(att.Deadline(sess).Add(sess.Grace)) {
			reason = SubmitTimeout
		}
		if err := s.close(tx, sess, att, reason); err != nil {
			return err
		}
		return s.finishIfDone(tx, sess)
	})
	if err == nil {
		s.changed(att.SessionID)
	}
	return att, err
}

// close grades every slot of att and marks it submitted.
func (s *Service) close(tx Tx, sess *Session, att *Attempt, reason SubmitReason) error {
	if err := s.grade(tx, sess, att); err != nil {
		return err
	}
	now := s.clock()
	att.State, att.SubmittedAt, att.SubmitReason = AttemptSubmitted, now, reason
	if err := tx.UpdateAttempt(att); err != nil {
		return err
	}
	return tx.InsertEvent(&Event{SessionID: att.SessionID, AttemptID: att.ID, Kind: "attempt.submitted", Severity: SeverityInfo,
		Data: map[string]any{"reason": string(reason), "score": att.Score, "max": att.MaxScore}, ReceivedAt: now})
}

// grade scores every slot of att into att.Score and att.MaxScore. Voided
// questions score 0, point penalties are subtracted, never below 0, and the
// session's corrections apply (see correction.go).
func (s *Service) grade(tx Tx, sess *Session, att *Attempt) error {
	sc, err := s.score(tx, sess, att, nil)
	if err != nil {
		return err
	}
	answers, err := tx.ListAnswers(att.ID)
	if err != nil {
		return err
	}
	for _, a := range answers {
		if p, ok := sc.points[a.QuestionID]; ok {
			if err := tx.SetAnswerGrade(att.ID, a.QuestionID, p, sc.correct[a.QuestionID]); err != nil {
				return err
			}
		}
	}
	att.Score, att.MaxScore = sc.score, sc.max
	return nil
}

// regrade recomputes the score of a closed attempt after a penalty changed.
func (s *Service) regrade(tx Tx, sess *Session, att *Attempt) error {
	if err := s.grade(tx, sess, att); err != nil {
		return err
	}
	return tx.UpdateAttempt(att)
}

func roundScore(v float64) float64 { return float64(int64(v*1e4+0.5)) / 1e4 }

// finishIfDone ends a running session once every student has turned in (or
// ran out of time): there is no point in waiting for the clock. It runs on
// submissions only, so withdrawing or annulling the last student leaves the
// session open for a readmission.
func (s *Service) finishIfDone(tx Tx, sess *Session) error {
	if sess.State != StateRunning {
		return nil
	}
	attempts, err := tx.ListAttempts(sess.ID)
	if err != nil {
		return err
	}
	if len(attempts) == 0 {
		return nil
	}
	for _, a := range attempts {
		if a.State == AttemptActive {
			return nil
		}
	}
	return s.end(tx, sess, SubmitEnded, "session.finished")
}

// End closes the session now and submits every active attempt.
func (s *Service) End(ctx context.Context, sessionID string) error {
	err := s.store.Update(ctx, func(tx Tx) error {
		sess, err := openSession(tx, sessionID)
		if err != nil {
			return err
		}
		return s.end(tx, sess, SubmitEnded, "session.ended")
	})
	if err == nil {
		s.changed(sessionID)
	}
	return err
}

func (s *Service) end(tx Tx, sess *Session, reason SubmitReason, kind string) error {
	attempts, err := tx.ListAttempts(sess.ID)
	if err != nil {
		return err
	}
	for _, a := range attempts {
		if a.State == AttemptActive {
			if err := s.close(tx, sess, a, reason); err != nil {
				return err
			}
		}
	}
	now := s.clock()
	sess.State, sess.EndedAt = StateEnded, now
	if err := tx.UpdateSession(sess); err != nil {
		return err
	}
	return tx.InsertEvent(&Event{SessionID: sess.ID, Kind: kind, Severity: SeverityInfo, ReceivedAt: now})
}

// FinalizeExpired submits attempts whose time (plus grace) is over and ends
// running sessions once nobody has time left. Call it periodically and on
// startup, which also recovers sessions that expired while the server was
// down. It returns the number of attempts submitted.
func (s *Service) FinalizeExpired(ctx context.Context) (int, error) {
	n := 0
	touched := map[string]bool{}
	err := s.store.Update(ctx, func(tx Tx) error {
		sessions, err := tx.ListOpenSessions()
		if err != nil {
			return err
		}
		now := s.clock()
		for _, sess := range sessions {
			if sess.State != StateRunning {
				continue
			}
			attempts, err := tx.ListAttempts(sess.ID)
			if err != nil {
				return err
			}
			remaining := false
			for _, a := range attempts {
				if a.State != AttemptActive {
					continue
				}
				if now.After(a.Deadline(sess).Add(sess.Grace)) {
					if err := s.close(tx, sess, a, SubmitTimeout); err != nil {
						return err
					}
					n++
					touched[sess.ID] = true
				} else {
					remaining = true
				}
			}
			if !remaining && now.After(sess.EndsAt.Add(sess.Grace)) {
				if err := s.end(tx, sess, SubmitTimeout, "session.expired"); err != nil {
					return err
				}
				touched[sess.ID] = true
			}
		}
		return nil
	})
	if err == nil {
		for id := range touched {
			s.changed(id)
		}
	}
	return n, err
}

// Snapshot is a consistent view of a session for displays and exports.
type Snapshot struct {
	Session      *Session
	Participants map[string]*Participant // by participant id
	Attempts     []*Attempt
	Answers      map[string][]*Answer  // by attempt id
	Penalties    map[string][]*Penalty // by attempt id
	SignalCounts map[string]int        // warn and high signals by attempt id
	Incidents    []*Event              // newest integrity signals, newest first
}

// incidentLimit bounds the signal log shown live.
const incidentLimit = 80

// Snapshot reads the whole state of a session in one transaction.
func (s *Service) Snapshot(ctx context.Context, sessionID string) (*Snapshot, error) {
	snap := &Snapshot{Participants: map[string]*Participant{}, Answers: map[string][]*Answer{}, Penalties: map[string][]*Penalty{}}
	err := s.store.View(ctx, func(tx Tx) error {
		var err error
		if snap.Session, err = tx.GetSession(sessionID); err != nil {
			return err
		}
		parts, err := tx.ListParticipants(sessionID)
		if err != nil {
			return err
		}
		for _, p := range parts {
			snap.Participants[p.ID] = p
		}
		if snap.Attempts, err = tx.ListAttempts(sessionID); err != nil {
			return err
		}
		ps, err := tx.ListSessionPenalties(sessionID)
		if err != nil {
			return err
		}
		for _, p := range ps {
			snap.Penalties[p.AttemptID] = append(snap.Penalties[p.AttemptID], p)
		}
		if snap.SignalCounts, err = tx.SignalCounts(sessionID); err != nil {
			return err
		}
		if snap.Incidents, err = tx.RecentEvents(sessionID, "signal.", incidentLimit); err != nil {
			return err
		}
		for _, a := range snap.Attempts {
			if snap.Answers[a.ID], err = tx.ListAnswers(a.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return snap, err
}

// Now returns the server clock used for every rule, in UTC milliseconds.
func (s *Service) Now() time.Time { return s.clock() }

// AttemptDetail is everything a student's client needs about one attempt.
type AttemptDetail struct {
	Attempt     *Attempt
	Session     *Session
	Participant *Participant
	Answers     []*Answer
	Exam        *exam.Published
	Penalties   []*Penalty
	Opened      map[string]time.Time // timed questions: when each was first opened
}

// Attempt loads an attempt with its session, participant, answers and exam.
func (s *Service) Attempt(ctx context.Context, attemptID string) (*AttemptDetail, error) {
	d := &AttemptDetail{}
	err := s.store.View(ctx, func(tx Tx) error {
		var err error
		if d.Attempt, err = tx.GetAttempt(attemptID); err != nil {
			return err
		}
		if d.Session, err = tx.GetSession(d.Attempt.SessionID); err != nil {
			return err
		}
		if d.Participant, err = tx.GetParticipant(d.Attempt.ParticipantID); err != nil {
			return err
		}
		if d.Answers, err = tx.ListAnswers(attemptID); err != nil {
			return err
		}
		if d.Penalties, err = tx.ListPenalties(attemptID); err != nil {
			return err
		}
		if d.Opened, err = tx.ListOpened(attemptID); err != nil {
			return err
		}
		d.Exam, err = s.published(tx, d.Session.ExamHash)
		return err
	})
	if err != nil {
		return nil, err
	}
	return d, nil
}

// OpenSession returns the session that is still in the lobby or running.
// A data directory holds at most one; ErrNotFound when there is none.
func (s *Service) OpenSession(ctx context.Context) (*Session, error) {
	var sess *Session
	err := s.store.View(ctx, func(tx Tx) error {
		open, err := tx.ListOpenSessions()
		if err != nil {
			return err
		}
		if len(open) == 0 {
			return ErrNotFound
		}
		sess = open[len(open)-1]
		return nil
	})
	return sess, err
}

// Session loads a session and its published exam.
func (s *Service) Session(ctx context.Context, sessionID string) (*Session, *exam.Published, error) {
	var sess *Session
	var pub *exam.Published
	err := s.store.View(ctx, func(tx Tx) error {
		var err error
		if sess, err = tx.GetSession(sessionID); err != nil {
			return err
		}
		pub, err = s.published(tx, sess.ExamHash)
		return err
	})
	return sess, pub, err
}

// Session code alphabet: no 0/O, 1/I/L, so codes can be dictated.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// CodeLength is the number of characters of a session code.
const CodeLength = 6

func newCode() string {
	b := make([]byte, CodeLength)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(b)
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns a random 128-bit identifier as 26 lowercase base32 characters.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return idEncoding.EncodeToString(b)
}

func randomUint64() uint64 {
	var b [8]byte
	randomBytes(b[:])
	return binary.LittleEndian.Uint64(b[:])
}

func randomBytes(b []byte) {
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
}
