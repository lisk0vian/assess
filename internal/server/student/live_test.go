package student

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestJoinWindowAndResume(t *testing.T) {
	e := newEnv(t, "")
	early := e.browser()
	early.mustStatus(http.StatusOK, "POST", "/api/join", map[string]string{"code": e.sess.Code, "name": "Ana", "student_id": "A1"}, nil)
	if _, err := e.svc.Start(context.Background(), e.sess.ID); err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(11 * time.Minute)

	late := e.browser()
	raw := late.mustStatus(http.StatusConflict, "POST", "/api/join", map[string]string{"code": e.sess.Code, "name": "Tarde", "student_id": "T9"}, nil)
	if !strings.Contains(raw, "pending_approval") || e.h.Requests()[0].Kind != KindLate {
		t.Fatalf("unexpected body %s", raw)
	}
	// Someone already in can reload the page and keep going.
	early.mustStatus(http.StatusOK, "POST", "/api/join", map[string]string{"code": e.sess.Code, "name": "Ana", "student_id": "A1"}, nil)
}

func TestJoinRateLimit(t *testing.T) {
	e := newEnv(t, "")
	c := e.browser()
	for i := 0; i < 12; i++ {
		c.mustStatus(http.StatusNotFound, "POST", "/api/join", map[string]string{"code": "XXXXXX", "name": "A", "student_id": "1"}, nil)
	}
	c.mustStatus(http.StatusTooManyRequests, "POST", "/api/join", map[string]string{"code": e.sess.Code, "name": "A", "student_id": "1"}, nil)
}

func TestLocalNetworksOnly(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	_, school, _ := net.ParseCIDR("83.44.10.0/24")
	h := LocalNetworksOnly(ok, []*net.IPNet{school})
	for addr, want := range map[string]int{
		"83.44.10.9:5000":   http.StatusNoContent, // the computer's own subnet, public range
		"100.64.3.3:5000":   http.StatusNoContent,
		"83.44.99.9:5000":   http.StatusForbidden,
		"192.168.1.20:5000": http.StatusNoContent,
		"10.0.0.7:5000":     http.StatusNoContent,
		"127.0.0.1:5000":    http.StatusNoContent,
		"[fd00::1]:5000":    http.StatusNoContent,
		"8.8.8.8:5000":      http.StatusForbidden,
		"[2001:db8::1]:80":  http.StatusForbidden,
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = addr
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%s: status %d, want %d", addr, w.Code, want)
		}
	}
}

func TestLiveSocketPushesChanges(t *testing.T) {
	e := newEnv(t, "")
	c := e.browser()
	c.mustStatus(http.StatusOK, "POST", "/api/join", map[string]string{"code": e.sess.Code, "name": "Ana", "student_id": "A1"}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.srv.URL, "http")+"/api/ws", &websocket.DialOptions{
		HTTPClient: c.http,
		HTTPHeader: http.Header{"Origin": {e.srv.URL}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body != nil {
		resp.Body.Close()
	}
	defer ws.CloseNow()

	deadline := time.Now().Add(5 * time.Second)
	for e.h.Online() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("online = %d, want 1", e.h.Online())
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := e.svc.Start(context.Background(), e.sess.ID); err != nil {
		t.Fatal(err)
	}
	_, msg, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != `{"type":"changed"}` {
		t.Fatalf("message %q", msg)
	}

	anon, resp2, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.srv.URL, "http")+"/api/ws", nil)
	if resp2 != nil && resp2.Body != nil {
		resp2.Body.Close()
	}
	if err == nil {
		anon.CloseNow()
		t.Fatal("a socket without an attempt cookie must be refused")
	}
}
