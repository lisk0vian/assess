package student

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/storage/sqlite"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

type env struct {
	t    *testing.T
	svc  *session.Service
	sess *session.Session
	srv  *httptest.Server
	clk  *clock
	h    *Handler
}

func newEnv(t *testing.T, visibility string) *env {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	pub, _, err := exam.Publish("../../../examples/algebra-01")
	if err != nil {
		t.Fatal(err)
	}
	if visibility != "" {
		pub.Exam.Manifest.ResultVisibility = visibility
	}
	clk := &clock{t: time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)}
	var h *Handler
	svc := session.NewService(db, session.WithClock(clk.Now), session.WithNotify(func(id string) {
		if h != nil {
			h.Notify(id)
		}
	}))
	sess, err := svc.CreateSession(ctx, pub, session.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h = New(svc, sess.ID, Options{JoinWindow: 10 * time.Minute})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &env{t: t, svc: svc, sess: sess, srv: srv, clk: clk, h: h}
}

// client is one browser with its own cookie jar.
type client struct {
	e    *env
	http *http.Client
}

func (e *env) browser() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path string, body any, out any) (int, string) {
	c.e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.e.srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && resp.StatusCode < 300 {
		if err := json.Unmarshal(raw, out); err != nil {
			c.e.t.Fatalf("decode %s: %v\n%s", path, err, raw)
		}
	}
	return resp.StatusCode, string(raw)
}

func (c *client) mustStatus(want int, method, path string, body, out any) string {
	c.e.t.Helper()
	got, raw := c.do(method, path, body, out)
	if got != want {
		c.e.t.Fatalf("%s %s = %d, want %d: %s", method, path, got, want, raw)
	}
	return raw
}

var correct = map[string]map[string]any{
	"q001": {"option": "b"}, "q002": {"options": []string{"a", "b", "d"}}, "q003": {"value": false},
	"q004": {"text": "4"}, "q005": {"option": "a"}, "q006": {"text": "coeficiente"}, "q007": {"text": "12"},
}

func TestStudentFlow(t *testing.T) {
	e := newEnv(t, "")
	ana := e.browser()

	ana.mustStatus(http.StatusUnauthorized, "GET", "/api/me", nil, nil)
	ana.mustStatus(http.StatusNotFound, "POST", "/api/join", map[string]string{"code": "ZZZZZZ", "name": "Ana", "student_id": "a1"}, nil)
	ana.mustStatus(http.StatusBadRequest, "POST", "/api/join", map[string]string{"code": e.sess.Code, "extra": "x"}, nil)

	var me MeView
	ana.mustStatus(http.StatusOK, "POST", "/api/join", map[string]string{"code": strings.ToLower(e.sess.Code), "name": "Ana Pérez", "student_id": "a-1"}, &me)
	if me.Session.State != session.StateLobby || len(me.Questions) != 6 || me.Student.StudentID != "A-1" || me.Attempt.Score != nil {
		t.Fatalf("unexpected lobby view: %+v", me)
	}
	ana.mustStatus(http.StatusConflict, "GET", "/api/questions/"+me.Questions[0].ID, nil, nil)

	if _, err := e.svc.Start(context.Background(), e.sess.ID); err != nil {
		t.Fatal(err)
	}

	seq := int64(0)
	for _, q := range me.Questions {
		var qv QuestionView
		raw := ana.mustStatus(http.StatusOK, "GET", "/api/questions/"+q.ID, nil, &qv)
		for _, leak := range []string{`"key"`, `"accept"`, `"correct"`, `"tolerance"`} {
			if strings.Contains(raw, leak) {
				t.Fatalf("question %s leaks %s: %s", q.ID, leak, raw)
			}
		}
		if qv.Number != q.Number || qv.Prompt == "" {
			t.Fatalf("bad question view %+v", qv)
		}
		seq++
		var ar answerResponse
		ana.mustStatus(http.StatusOK, "PUT", "/api/answers/"+q.ID, map[string]any{"seq": seq, "response": correct[q.ID]}, &ar)
		if !ar.Applied {
			t.Fatalf("answer %s not applied", q.ID)
		}
	}
	ana.mustStatus(http.StatusNotFound, "GET", "/api/questions/q999", nil, nil)
	ana.mustStatus(http.StatusBadRequest, "PUT", "/api/answers/"+me.Questions[0].ID, map[string]any{"seq": 99, "response": map[string]any{"option": "zz"}}, nil)

	ana.mustStatus(http.StatusOK, "GET", "/api/me", nil, &me)
	if len(me.Answers) != 6 || me.Attempt.LastSeq != 6 {
		t.Fatalf("answers not reflected: %+v", me.Answers)
	}

	ana.mustStatus(http.StatusOK, "POST", "/api/submit", nil, &me)
	if me.Attempt.State != session.AttemptSubmitted || me.Attempt.Score == nil || *me.Attempt.Score != 9 || me.Review != nil {
		t.Fatalf("unexpected result view: %+v", me.Attempt)
	}
	ana.mustStatus(http.StatusConflict, "POST", "/api/submit", nil, nil)
	ana.mustStatus(http.StatusConflict, "PUT", "/api/answers/"+me.Questions[0].ID, map[string]any{"seq": 100, "response": correct["q001"]}, nil)
}

func TestRejoinNeedsSameBrowser(t *testing.T) {
	e := newEnv(t, "")
	join := map[string]string{"code": e.sess.Code, "name": "Luis", "student_id": "L7"}
	first := e.browser()
	first.mustStatus(http.StatusOK, "POST", "/api/join", join, nil)
	first.mustStatus(http.StatusOK, "POST", "/api/join", join, nil) // reload: resumes

	other := e.browser()
	raw := other.mustStatus(http.StatusConflict, "POST", "/api/join", join, nil)
	if !strings.Contains(raw, "pending_approval") {
		t.Fatalf("unexpected body %s", raw)
	}
}

func TestVisibilityNoneHidesScore(t *testing.T) {
	e := newEnv(t, "none")
	c := e.browser()
	var me MeView
	c.mustStatus(http.StatusOK, "POST", "/api/join", map[string]string{"code": e.sess.Code, "name": "Eva", "student_id": "E1"}, &me)
	e.svc.Start(context.Background(), e.sess.ID)
	c.mustStatus(http.StatusOK, "POST", "/api/submit", nil, &me)
	if me.Attempt.Score != nil {
		t.Fatal("score shown with result_visibility none")
	}
}

func TestReviewOnlyAfterSessionEnds(t *testing.T) {
	e := newEnv(t, "review")
	c := e.browser()
	var me MeView
	c.mustStatus(http.StatusOK, "POST", "/api/join", map[string]string{"code": e.sess.Code, "name": "Eva", "student_id": "E1"}, &me)
	e.svc.Start(context.Background(), e.sess.ID)
	q := me.Questions[0].ID
	c.mustStatus(http.StatusOK, "PUT", "/api/answers/"+q, map[string]any{"seq": 1, "response": correct[q]}, nil)
	c.mustStatus(http.StatusOK, "POST", "/api/submit", nil, &me)
	if me.Review != nil {
		t.Fatal("review shown while the session is still running")
	}
	if err := e.svc.End(context.Background(), e.sess.ID); err != nil {
		t.Fatal(err)
	}
	c.mustStatus(http.StatusOK, "GET", "/api/me", nil, &me)
	if len(me.Review) != 6 || !me.Review[0].Correct || me.Review[0].QuestionID != q {
		t.Fatalf("unexpected review: %+v", me.Review)
	}
}

func TestTimeUp(t *testing.T) {
	e := newEnv(t, "")
	c := e.browser()
	var me MeView
	c.mustStatus(http.StatusOK, "POST", "/api/join", map[string]string{"code": e.sess.Code, "name": "Eva", "student_id": "E1"}, &me)
	e.svc.Start(context.Background(), e.sess.ID)
	e.clk.Advance(46 * time.Minute)
	raw := c.mustStatus(http.StatusConflict, "PUT", "/api/answers/"+me.Questions[0].ID, map[string]any{"seq": 1, "response": correct[me.Questions[0].ID]}, nil)
	if !strings.Contains(raw, "time_up") {
		t.Fatalf("unexpected body %s", raw)
	}
	c.mustStatus(http.StatusNotFound, "GET", "/api/assets/nope.png", nil, nil)
	c.mustStatus(http.StatusNotFound, "GET", "/api/nothing", nil, nil)
}
