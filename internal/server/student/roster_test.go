package student

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/storage/sqlite"
)

// A roster-only session lets the class in and holds strangers for the
// teacher, who can approve them and add them to the class at once.
func TestRosterOnly(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := session.NewService(db)
	cls, err := svc.CreateClass(ctx, "3B")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Enroll(ctx, cls.ID, []session.Enrollee{{Ref: "A-1", Name: "Ana"}}); err != nil {
		t.Fatal(err)
	}
	pub, _, err := exam.Publish("../../../examples/algebra-01")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := svc.CreateSession(ctx, pub, session.CreateOptions{ClassID: cls.ID, RosterOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	h := New(svc, sess.ID, Options{JoinWindow: 10 * time.Minute})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	e := &env{t: t, svc: svc, sess: sess, srv: srv, h: h}

	var info map[string]bool
	e.browser().mustStatus(http.StatusOK, "GET", "/api/join/info?code="+sess.Code, nil, &info)
	if !info["roster_only"] {
		t.Fatalf("the form learns it needs the identifier only: %v", info)
	}
	// On the list, the identifier is enough: the name comes from the list.
	e.browser().mustStatus(http.StatusOK, "POST", "/api/join", map[string]string{"code": sess.Code, "student_id": "a-1"}, nil)
	if n, _ := svc.KnownName(ctx, sess.ID, "A-1"); n != "Ana" {
		t.Fatalf("listed name %q", n)
	}
	e.browser().mustStatus(http.StatusBadRequest, "POST", "/api/join", map[string]string{"code": sess.Code, "student_id": "Z-9"}, nil)

	stranger := e.browser()
	join := map[string]string{"code": sess.Code, "name": "Zoe", "student_id": "Z-9"}
	stranger.mustStatus(http.StatusConflict, "POST", "/api/join", join, nil)
	reqs := h.Requests()
	if len(reqs) != 1 || reqs[0].Kind != KindUnlisted {
		t.Fatalf("requests = %+v", reqs)
	}
	if err := h.Enroll(httptest.NewRequest("POST", "/", nil), reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	stranger.mustStatus(http.StatusOK, "GET", "/api/join/status", nil, nil)
	p, err := svc.People(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if z := p.ByRef["Z-9"]; z == nil || len(p.Of[z.ID]) != 1 {
		t.Fatalf("the approved stranger must join the class: %+v", p.ByRef["Z-9"])
	}
}

// Open to anyone with the code, a class session puts whoever joins in the
// class, once, and their identifier keeps them one student.
func TestOpenClassSessionEnrollsWhoJoins(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := session.NewService(db)
	cls, _ := svc.CreateClass(ctx, "3B")
	other, _ := svc.CreateClass(ctx, "4A")
	if _, err := svc.Enroll(ctx, other.ID, []session.Enrollee{{Ref: "L-2", Name: "Leo"}}); err != nil {
		t.Fatal(err)
	}
	pub, _, err := exam.Publish("../../../examples/algebra-01")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := svc.CreateSession(ctx, pub, session.CreateOptions{ClassID: cls.ID})
	if err != nil {
		t.Fatal(err)
	}
	h := New(svc, sess.ID, Options{})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	e := &env{t: t, svc: svc, sess: sess, srv: srv, h: h}
	e.browser().mustStatus(http.StatusOK, "POST", "/api/join", map[string]string{"code": sess.Code, "name": "Zoe", "student_id": "Z-9"}, nil)
	e.browser().mustStatus(http.StatusOK, "POST", "/api/join", map[string]string{"code": sess.Code, "name": "Leo M.", "student_id": "l-2"}, nil)
	p, err := svc.People(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.List) != 2 || len(p.Members[cls.ID]) != 2 {
		t.Fatalf("two students, both in 3B: %d students, %d members", len(p.List), len(p.Members[cls.ID]))
	}
	if leo := p.ByRef["L-2"]; len(p.Of[leo.ID]) != 2 || leo.Name != "Leo" {
		t.Fatalf("Leo keeps 4A and his listed name: %+v %v", leo, p.Of[leo.ID])
	}
}
