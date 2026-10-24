package student

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeviceApprovalFlow(t *testing.T) {
	e := newEnv(t, "")
	join := map[string]string{"code": e.sess.Code, "name": "Luis", "student_id": "L7"}
	first := e.browser()
	var me MeView
	first.mustStatus(http.StatusOK, "POST", "/api/join", join, &me)

	second := e.browser()
	second.mustStatus(http.StatusConflict, "POST", "/api/join", join, nil)
	second.mustStatus(http.StatusAccepted, "GET", "/api/join/status", nil, nil)
	second.mustStatus(http.StatusUnauthorized, "GET", "/api/me", nil, nil)

	reqs := e.h.Requests()
	if len(reqs) != 1 || reqs[0].Kind != KindDevice || reqs[0].StudentID != "L7" || reqs[0].Status != Pending {
		t.Fatalf("unexpected requests %+v", reqs)
	}
	if err := e.h.Approve(httptest.NewRequest("POST", "/", nil), reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	var approved MeView
	second.mustStatus(http.StatusOK, "GET", "/api/join/status", nil, &approved)
	if approved.Student.StudentID != "L7" || approved.Attempt.MaxScore != me.Attempt.MaxScore {
		t.Fatalf("approved device got %+v", approved.Student)
	}
	second.mustStatus(http.StatusOK, "GET", "/api/me", nil, nil) // now holds the attempt cookie

	third := e.browser()
	third.mustStatus(http.StatusConflict, "POST", "/api/join", join, nil)
	id := e.h.Requests()[0].ID
	if err := e.h.Reject(id); err != nil {
		t.Fatal(err)
	}
	third.mustStatus(http.StatusForbidden, "GET", "/api/join/status", nil, nil)
	if err := e.h.Approve(httptest.NewRequest("POST", "/", nil), "nope"); err == nil {
		t.Fatal("approving an unknown request must fail")
	}
}
