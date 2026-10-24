package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/grading"
	"github.com/lisk0vian/assess/internal/session"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type env struct {
	t     *testing.T
	ctx   context.Context
	path  string
	db    *DB
	clk   *clock
	svc   *session.Service
	pub   *exam.Published
	sess  *session.Session
	right map[string]grading.Response // correct responses for algebra-01
	wrong map[string]grading.Response
}

func ptr[T any](v T) *T { return &v }

func newEnv(t *testing.T) *env {
	t.Helper()
	pub, _, err := exam.Publish("../../../examples/algebra-01")
	if err != nil {
		t.Fatal(err)
	}
	e := &env{
		t: t, ctx: context.Background(), path: filepath.Join(t.TempDir(), "assess.db"),
		clk: &clock{t: time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)}, pub: pub,
		right: map[string]grading.Response{
			"q001": {Option: "b"}, "q002": {Options: []string{"a", "b", "d"}}, "q003": {Value: ptr(false)},
			"q004": {Text: "4"}, "q005": {Option: "a"}, "q006": {Text: " Coeficiente "}, "q007": {Text: "12"},
		},
		wrong: map[string]grading.Response{
			"q001": {Option: "a"}, "q002": {Options: []string{"a", "b"}}, "q003": {Value: ptr(true)},
			"q004": {Text: "5"}, "q005": {Option: "b"}, "q006": {Text: "constante"}, "q007": {Text: "3,5"},
		},
	}
	e.open()
	t.Cleanup(func() { e.db.Close() })
	return e
}

func (e *env) open() {
	e.t.Helper()
	db, err := Open(e.ctx, e.path)
	if err != nil {
		e.t.Fatal(err)
	}
	e.db = db
	e.svc = session.NewService(db, session.WithClock(e.clk.Now))
}

func (e *env) reopen() {
	e.t.Helper()
	if err := e.db.Close(); err != nil {
		e.t.Fatal(err)
	}
	e.open()
}

func (e *env) create() *session.Session {
	e.t.Helper()
	s, err := e.svc.CreateSession(e.ctx, e.pub, session.CreateOptions{})
	if err != nil {
		e.t.Fatal(err)
	}
	e.sess = s
	return s
}

func (e *env) join(studentID, name string) *session.Attempt {
	e.t.Helper()
	a, _, err := e.svc.Join(e.ctx, e.sess.ID, studentID, name)
	if err != nil {
		e.t.Fatal(err)
	}
	return a
}

// answerAll answers every question of the attempt from responses.
func (e *env) answerAll(a *session.Attempt, responses map[string]grading.Response, seq *int64) {
	e.t.Helper()
	for _, sl := range a.Layout {
		*seq++
		if _, err := e.svc.SaveAnswer(e.ctx, a.ID, sl.QuestionID, *seq, responses[sl.QuestionID]); err != nil {
			e.t.Fatalf("save %s: %v", sl.QuestionID, err)
		}
	}
}

func expectErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestFullSessionSurvivesRestart(t *testing.T) {
	e := newEnv(t)
	sess := e.create()
	if sess.State != session.StateLobby || len(sess.Code) != session.CodeLength {
		t.Fatalf("unexpected new session: %+v", sess)
	}

	ana := e.join("a-001", "Ana  Pérez")
	luis := e.join("A002", "Luis Gómez")
	if ana.MaxScore != 9 || luis.MaxScore != 9 || len(ana.Layout) != 6 {
		t.Fatalf("unexpected attempts: %+v / %+v", ana, luis)
	}

	// Answers are rejected in the lobby.
	_, err := e.svc.SaveAnswer(e.ctx, ana.ID, ana.Layout[0].QuestionID, 1, e.right[ana.Layout[0].QuestionID])
	expectErr(t, err, session.ErrNotStarted)

	if _, err := e.svc.Start(e.ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Start(e.ctx, sess.ID)
	expectErr(t, err, session.ErrAlreadyStarted)

	var anaSeq, luisSeq int64
	e.answerAll(luis, e.right, &luisSeq) // first right...
	e.answerAll(ana, e.right, &anaSeq)
	e.answerAll(luis, e.wrong, &luisSeq) // ...then changes his mind

	// A stale retry (older seq) must not overwrite the newer answer.
	q := luis.Layout[0].QuestionID
	applied, err := e.svc.SaveAnswer(e.ctx, luis.ID, q, 1, e.right[q])
	if err != nil || applied {
		t.Fatalf("stale retry: applied=%v err=%v", applied, err)
	}

	// The process dies here; everything must be on disk.
	e.reopen()

	got, err := e.svc.Submit(e.ctx, ana.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Score != 9 || got.SubmitReason != session.SubmitManual {
		t.Fatalf("ana: score %v reason %s, want 9 manual", got.Score, got.SubmitReason)
	}
	_, err = e.svc.Submit(e.ctx, ana.ID)
	expectErr(t, err, session.ErrAttemptClosed)

	if err := e.svc.End(e.ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	snap, err := e.svc.Snapshot(e.ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Session.State != session.StateEnded || len(snap.Attempts) != 2 || len(snap.Participants) != 2 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	for _, a := range snap.Attempts {
		p := snap.Participants[a.ParticipantID]
		switch p.StudentID {
		case "A-001":
			if p.Name != "Ana Pérez" {
				t.Errorf("name not normalized: %q", p.Name)
			}
		case "A002":
			// Only q002 (partial credit, 2 of 3 correct options) earns points: 2 × 2/3.
			if a.State != session.AttemptSubmitted || a.SubmitReason != session.SubmitEnded || a.Score != 1.3333 {
				t.Errorf("luis: %+v", a)
			}
			for _, ans := range snap.Answers[a.ID] {
				if !ans.Graded || ans.Correct {
					t.Errorf("luis %s graded=%v correct=%v", ans.QuestionID, ans.Graded, ans.Correct)
				}
			}
		default:
			t.Errorf("unexpected student %q", p.StudentID)
		}
	}

	events, err := e.db.listEvents(e.ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, ev := range events {
		kinds[ev.Kind]++
	}
	if kinds["session.created"] != 1 || kinds["attempt.joined"] != 2 || kinds["session.started"] != 1 ||
		kinds["attempt.submitted"] != 2 || kinds["session.ended"] != 1 {
		t.Fatalf("unexpected events: %v", kinds)
	}
}

func TestJoinRules(t *testing.T) {
	e := newEnv(t)
	e.create()
	e.join("x1", "Ana")
	_, _, err := e.svc.Join(e.ctx, e.sess.ID, " X 1 ", "Otra persona")
	expectErr(t, err, session.ErrAlreadyJoined)
	_, _, err = e.svc.Join(e.ctx, e.sess.ID, "", "Nadie")
	expectErr(t, err, session.ErrInvalidInput)

	// A pre-registered (roster) student can join once.
	if _, err := e.svc.AddParticipant(e.ctx, e.sess.ID, "R7", "Rosa", session.SourceRoster); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.AddParticipant(e.ctx, e.sess.ID, "r7", "Rosa", session.SourceManual)
	expectErr(t, err, session.ErrDuplicate)
	a, p, err := e.svc.Join(e.ctx, e.sess.ID, "r7", "Rosa M.")
	if err != nil || p.Source != session.SourceRoster || a == nil {
		t.Fatalf("roster join: %v %+v", err, p)
	}

	if err := e.svc.End(e.ctx, e.sess.ID); err != nil {
		t.Fatal(err)
	}
	_, _, err = e.svc.Join(e.ctx, e.sess.ID, "late", "Tarde")
	expectErr(t, err, session.ErrSessionClosed)
}

func TestDeadlinesGraceAndExtraTime(t *testing.T) {
	e := newEnv(t)
	e.create()
	a := e.join("s1", "Uno")
	b := e.join("s2", "Dos")
	if _, err := e.svc.Start(e.ctx, e.sess.ID); err != nil {
		t.Fatal(err)
	}
	q := a.Layout[0].QuestionID

	e.clk.Advance(45*time.Minute + 20*time.Second) // past the end, within the 30s grace
	if _, err := e.svc.SaveAnswer(e.ctx, a.ID, q, 1, e.right[q]); err != nil {
		t.Fatalf("answer within grace rejected: %v", err)
	}
	if _, err := e.svc.ExtendAttempt(e.ctx, b.ID, 5*time.Minute); err != nil {
		t.Fatal(err)
	}

	e.clk.Advance(15 * time.Second) // grace over for a, b still has extra time
	_, err := e.svc.SaveAnswer(e.ctx, a.ID, q, 2, e.right[q])
	expectErr(t, err, session.ErrTimeUp)
	if _, err := e.svc.SaveAnswer(e.ctx, b.ID, b.Layout[0].QuestionID, 1, e.right[b.Layout[0].QuestionID]); err != nil {
		t.Fatalf("answer with extra time rejected: %v", err)
	}

	n, err := e.svc.FinalizeExpired(e.ctx)
	if err != nil || n != 1 {
		t.Fatalf("finalize: n=%d err=%v, want 1", n, err)
	}
	snap, _ := e.svc.Snapshot(e.ctx, e.sess.ID)
	if snap.Session.State != session.StateRunning {
		t.Fatal("session ended while a student still had extra time")
	}
	for _, at := range snap.Attempts {
		if at.ID == a.ID && (at.SubmitReason != session.SubmitTimeout || at.Score != a.Layout[0].Points) {
			t.Fatalf("a: %+v", at)
		}
	}

	e.clk.Advance(10 * time.Minute)
	if n, err := e.svc.FinalizeExpired(e.ctx); err != nil || n != 1 {
		t.Fatalf("second finalize: n=%d err=%v", n, err)
	}
	snap, _ = e.svc.Snapshot(e.ctx, e.sess.ID)
	if snap.Session.State != session.StateEnded {
		t.Fatalf("session should end once nobody has time left, state %s", snap.Session.State)
	}
}

func TestRecoveryAfterDowntime(t *testing.T) {
	e := newEnv(t)
	e.create()
	a := e.join("s1", "Uno")
	if _, err := e.svc.Start(e.ctx, e.sess.ID); err != nil {
		t.Fatal(err)
	}
	var seq int64
	e.answerAll(a, e.right, &seq)

	// The server is down for longer than the exam: the clock kept running.
	e.db.Close()
	e.clk.Advance(2 * time.Hour)
	e.open()

	if n, err := e.svc.FinalizeExpired(e.ctx); err != nil || n != 1 {
		t.Fatalf("recovery finalize: n=%d err=%v", n, err)
	}
	snap, _ := e.svc.Snapshot(e.ctx, e.sess.ID)
	if snap.Session.State != session.StateEnded || snap.Attempts[0].Score != 9 {
		t.Fatalf("recovered state: %s, score %v", snap.Session.State, snap.Attempts[0].Score)
	}
}

func TestConcurrentAnswers(t *testing.T) {
	e := newEnv(t)
	e.create()
	const students = 40
	attempts := make([]*session.Attempt, students)
	for i := range attempts {
		attempts[i] = e.join(fmt.Sprintf("s%02d", i), fmt.Sprintf("Student %d", i))
	}
	if _, err := e.svc.Start(e.ctx, e.sess.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, students)
	for _, a := range attempts {
		wg.Go(func() {
			seq := int64(0)
			for range 3 { // every student rewrites each answer a few times
				for _, sl := range a.Layout {
					seq++
					if _, err := e.svc.SaveAnswer(e.ctx, a.ID, sl.QuestionID, seq, e.right[sl.QuestionID]); err != nil {
						errs <- err
						return
					}
				}
			}
			if _, err := e.svc.Submit(e.ctx, a.ID); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	snap, _ := e.svc.Snapshot(e.ctx, e.sess.ID)
	for _, a := range snap.Attempts {
		if a.Score != 9 || a.LastSeq != 18 {
			t.Fatalf("attempt %s: score %v last_seq %d", a.ID, a.Score, a.LastSeq)
		}
	}
}

func TestPublishedRoundTrip(t *testing.T) {
	e := newEnv(t)
	e.create()
	e.create() // same exam twice: stored once
	var got *exam.Published
	err := e.db.View(e.ctx, func(tx session.Tx) error {
		var err error
		got, err = tx.GetPublished(e.pub.Hash)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Exam.Manifest.Title != e.pub.Exam.Manifest.Title || len(got.Exam.Questions) != 7 ||
		got.Exam.Questions["q007"].TimeLimit.D() != 90*time.Second || *got.Exam.Key.Answers["q004"].Number != 4 {
		t.Fatalf("published exam did not round-trip: %+v", got.Exam.Manifest)
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	e := newEnv(t)
	e.reopen()
	e.reopen()
	var n int
	if err := e.db.r.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("schema_migrations rows = %d, err %v", n, err)
	}
	if _, err := e.db.w.Exec(`INSERT INTO schema_migrations VALUES (999, 0)`); err != nil {
		t.Fatal(err)
	}
	e.db.Close()
	if _, err := Open(e.ctx, e.path); err == nil {
		t.Fatal("opening a database from a newer ax must fail")
	}
}

// listEvents is a test helper reading through the public Tx.
func (db *DB) listEvents(ctx context.Context, sessionID string) (out []*session.Event, err error) {
	err = db.View(ctx, func(tx session.Tx) error {
		out, err = tx.ListEvents(sessionID)
		return err
	})
	return out, err
}
