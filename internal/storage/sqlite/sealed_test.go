package sqlite

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/lisk0vian/assess/internal/seal"
	"github.com/lisk0vian/assess/internal/session"
)

func testCipher(t *testing.T) *seal.Cipher {
	t.Helper()
	c, err := seal.New(bytes.Repeat([]byte{7}, seal.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// openWith reopens the env's database with options.
func (e *env) openWith(opts ...Option) {
	e.t.Helper()
	if e.db != nil {
		_ = e.db.Close()
	}
	db, err := Open(e.ctx, e.path, opts...)
	if err != nil {
		e.t.Fatal(err)
	}
	e.db = db
	e.svc = session.NewService(db, session.WithClock(e.clk.Now))
}

// fileHas reports whether the database files contain text.
func fileHas(t *testing.T, path, text string) bool {
	t.Helper()
	for _, p := range []string{path, path + "-wal"} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if bytes.Contains(data, []byte(text)) {
			return true
		}
	}
	return false
}

var secrets = []string{"Zoraida Quintanilla", "ZQ-4471", "Resuelve la ecuación", "Coeficiente"}

func TestSealedColumnsHideContent(t *testing.T) {
	e := newEnv(t)
	c := testCipher(t)
	e.openWith(WithCipher(c, true))
	e.create()
	a := e.join("ZQ-4471", "Zoraida Quintanilla")
	if _, err := e.svc.Start(e.ctx, e.sess.ID); err != nil {
		t.Fatal(err)
	}
	var seq int64
	e.answerAll(a, e.right, &seq)
	if _, err := e.svc.Submit(e.ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.Compact(e.ctx); err != nil {
		t.Fatal(err)
	}
	for _, s := range secrets {
		if fileHas(t, e.path, s) {
			t.Errorf("the encrypted database contains %q", s)
		}
	}
	// It still works: lookups by identifier, names and grading.
	res, err := e.svc.Results(e.ctx, e.sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || res.Rows[0].Name != "Zoraida Quintanilla" || *res.Rows[0].Score != res.Rows[0].MaxScore {
		t.Fatalf("results = %+v", res.Rows)
	}
	h, err := e.svc.History(e.ctx, "ZQ-4471")
	if err != nil || len(h) != 1 || h[0].ExamTitle == "" {
		t.Fatalf("history = %+v, %v", h, err)
	}

	// Without the key: plain columns read, sealed ones refuse.
	e.openWith(WithCipher(nil, true))
	if _, err := e.svc.LatestSession(e.ctx); err != nil {
		t.Fatalf("plain columns must read while locked: %v", err)
	}
	if _, err := e.svc.Results(e.ctx, e.sess.ID); !errors.Is(err, seal.ErrLocked) {
		t.Fatalf("results while locked: %v", err)
	}
	if _, err := e.svc.AddStudent(e.ctx, "X-1", "Somebody", ""); !errors.Is(err, seal.ErrLocked) {
		t.Fatalf("writing a name while locked must fail, got %v", err)
	}
}

func TestResealRoundTrip(t *testing.T) {
	e := newEnv(t)
	e.create()
	a := e.join("ZQ-4471", "Zoraida Quintanilla")
	if _, err := e.svc.Start(e.ctx, e.sess.ID); err != nil {
		t.Fatal(err)
	}
	var seq int64
	e.answerAll(a, e.right, &seq)
	if !fileHas(t, e.path, "Zoraida Quintanilla") {
		t.Fatal("a plain database should hold the name")
	}
	c := testCipher(t)
	if err := e.db.Reseal(e.ctx, c); err != nil {
		t.Fatal(err)
	}
	for _, s := range secrets {
		if fileHas(t, e.path, s) {
			t.Errorf("after encrypting, the database still contains %q", s)
		}
	}
	e.openWith(WithCipher(c, true))
	if _, err := e.svc.Submit(e.ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.Reseal(e.ctx, nil); err != nil {
		t.Fatal(err)
	}
	e.openWith()
	res, err := e.svc.Results(e.ctx, e.sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows[0].Name != "Zoraida Quintanilla" || *res.Rows[0].Score != res.Rows[0].MaxScore {
		t.Fatalf("after decrypting: %+v", res.Rows[0])
	}
}

func TestPeople(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "sealed"}[encrypted], func(t *testing.T) {
			e := newEnv(t)
			if encrypted {
				e.openWith(WithCipher(testCipher(t), true))
			}
			ctx := e.ctx
			cls, err := e.svc.CreateClass(ctx, "3B")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.svc.CreateClass(ctx, "3b"); !errors.Is(err, session.ErrDuplicate) {
				t.Fatalf("class names are case-insensitive: %v", err)
			}
			res, err := e.svc.Enroll(ctx, cls.ID, []session.Enrollee{
				{Ref: "a-12", Name: "Ana García"}, {Ref: "B7", Name: "Luis Pérez"}, {Ref: "", Name: "nadie"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.Created != 2 || res.Added != 2 || len(res.Rejected) != 1 {
				t.Fatalf("enroll = %+v", res)
			}

			// A session for the class pre-registers its members.
			s, err := e.svc.CreateSession(ctx, e.pub, session.CreateOptions{ClassID: cls.ID, RosterOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			e.sess = s
			if listed, _ := e.svc.Listed(ctx, s.ID, "A-12"); !listed {
				t.Fatal("a class member must be listed")
			}
			if listed, _ := e.svc.Listed(ctx, s.ID, "Z-9"); listed {
				t.Fatal("a stranger must not be listed")
			}
			a := e.join("A-12", "Ana García")
			e.join("C-3", "Carla Ruiz") // a stranger becomes a student too
			p, err := e.svc.People(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.List) != 3 || p.ByRef["C-3"] == nil || len(p.Members[cls.ID]) != 2 {
				t.Fatalf("people = %+v", p)
			}

			// Renaming the identifier follows the student into the session.
			ana := p.ByRef["A-12"]
			if _, err := e.svc.RenameStudent(ctx, ana.ID, "a-120"); err != nil {
				t.Fatal(err)
			}
			if h, _ := e.svc.History(ctx, "A-120"); len(h) != 1 || h[0].Attempt.ID != a.ID {
				t.Fatalf("history after rename = %+v", h)
			}
			// Merging two students of the same session is refused.
			if err := e.svc.MergeStudents(ctx, p.ByRef["C-3"].ID, ana.ID); !errors.Is(err, session.ErrConflict) {
				t.Fatalf("merge in the same session: %v", err)
			}
			if err := e.svc.MergeStudents(ctx, p.ByRef["B7"].ID, ana.ID); !errors.Is(err, session.ErrConflict) {
				t.Fatalf("B7 was registered in the session too: %v", err)
			}
			dup, err := e.svc.AddStudent(ctx, "AG-1", "Ana García", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.svc.MergeStudents(ctx, dup.ID, ana.ID); err != nil {
				t.Fatal(err)
			}
			// Forgetting waits for the session to end, then removes everything.
			if _, err := e.svc.ForgetStudent(ctx, ana.ID); !errors.Is(err, session.ErrInUse) {
				t.Fatalf("forget during the session: %v", err)
			}
			if err := e.svc.End(ctx, s.ID); err != nil {
				t.Fatal(err)
			}
			touched, err := e.svc.ForgetStudent(ctx, ana.ID)
			if err != nil || len(touched) != 1 {
				t.Fatalf("forget = %v, %v", touched, err)
			}
			res2, _ := e.svc.Results(ctx, s.ID)
			for _, r := range res2.Rows {
				if strings.HasPrefix(r.StudentID, "A-12") {
					t.Fatal("the forgotten student is still in the results")
				}
			}
			if encrypted {
				_ = e.db.Compact(ctx)
				for _, s := range []string{"Ana García", "Carla Ruiz", "A-120"} {
					if fileHas(t, e.path, s) {
						t.Errorf("the database contains %q", s)
					}
				}
			}
		})
	}
}
