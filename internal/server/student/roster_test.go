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

	e.browser().mustStatus(http.StatusOK, "POST", "/api/join", map[string]string{"code": sess.Code, "name": "Ana", "student_id": "a-1"}, nil)

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
