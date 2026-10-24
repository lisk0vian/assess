package daemon

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lisk0vian/assess/internal/server/student"
	"github.com/lisk0vian/assess/internal/session"
)

// StatusView is the control API's description of the running session.
type StatusView struct {
	PID        int           `json:"pid"`
	Version    string        `json:"version"`
	SessionID  string        `json:"session_id"`
	ExamID     string        `json:"exam_id"`
	ExamTitle  string        `json:"exam_title"`
	Code       string        `json:"code"`
	State      session.State `json:"state"`
	Duration   int64         `json:"duration_ms"`
	StartedAt  int64         `json:"started_at,omitempty"`
	EndsAt     int64         `json:"ends_at,omitempty"`
	ServerNow  int64         `json:"server_now"`
	StudentURL string        `json:"student_url"`
	OtherURLs  []string      `json:"other_urls,omitempty"`
	Joined     int           `json:"joined"`
	Online     int           `json:"online"` // students with the exam open right now
	Active     int           `json:"active"`
	Submitted  int           `json:"submitted"`
	UptimeMs   int64         `json:"uptime_ms"`
	LastError  string        `json:"last_error,omitempty"`
}

type controlServer struct {
	d     *daemon
	token string
}

func (c *controlServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", c.status)
	mux.HandleFunc("POST /start", c.action(func(ctx context.Context, _ map[string]any) error {
		_, err := c.d.svc.Start(ctx, c.d.sessionID)
		return err
	}))
	mux.HandleFunc("POST /extend", c.action(func(ctx context.Context, body map[string]any) error {
		mins, _ := body["minutes"].(float64)
		_, err := c.d.svc.ExtendSession(ctx, c.d.sessionID, time.Duration(mins*float64(time.Minute)))
		return err
	}))
	mux.HandleFunc("POST /end", c.action(func(ctx context.Context, _ map[string]any) error {
		return c.d.svc.End(ctx, c.d.sessionID)
	}))
	mux.HandleFunc("POST /shutdown", c.action(func(ctx context.Context, body map[string]any) error {
		if end, _ := body["end"].(bool); end {
			if err := c.d.svc.End(ctx, c.d.sessionID); err != nil && !errors.Is(err, session.ErrSessionClosed) {
				return err
			}
		}
		c.d.requestStop()
		return nil
	}))
	c.studentRoutes(mux)
	return c.auth(mux)
}

func (c *controlServer) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(c.token)) != 1 {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (c *controlServer) status(w http.ResponseWriter, r *http.Request) {
	v, err := c.d.status(r.Context())
	if err != nil {
		writeControlError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (c *controlServer) action(fn func(context.Context, map[string]any) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body)
		if err := fn(r.Context(), body); err != nil {
			writeControlError(w, err)
			return
		}
		c.status(w, r)
	}
}

func writeControlError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	for _, e := range []error{session.ErrAlreadyStarted, session.ErrNotStarted, session.ErrSessionClosed, session.ErrInvalidInput,
		session.ErrNotClosable, session.ErrDuplicate, session.ErrAttemptClosed, session.ErrNotFound, student.ErrUnknownRequest} {
		if errors.Is(err, e) {
			status = http.StatusConflict
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func (d *daemon) status(ctx context.Context) (*StatusView, error) {
	snap, err := d.svc.Snapshot(ctx, d.sessionID)
	if err != nil {
		return nil, err
	}
	v := &StatusView{
		PID: os.Getpid(), Version: d.cfg.Version, SessionID: d.sessionID, ExamID: d.examID, ExamTitle: d.examTitle,
		Code: snap.Session.Code, State: snap.Session.State, Duration: snap.Session.Duration.Milliseconds(),
		StartedAt: msOrZero(snap.Session.StartedAt), EndsAt: msOrZero(snap.Session.EndsAt), ServerNow: d.svc.Now().UnixMilli(),
		StudentURL: d.studentURL, OtherURLs: d.otherURLs, Joined: len(snap.Attempts), Online: d.students.Online(),
		UptimeMs: time.Since(d.startedAt).Milliseconds(), LastError: d.lastError(),
	}
	for _, a := range snap.Attempts {
		switch a.State {
		case session.AttemptActive:
			v.Active++
		case session.AttemptSubmitted:
			v.Submitted++
		}
	}
	return v, nil
}

func msOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// Client talks to a running daemon's control API.
type Client struct {
	addr, token string
	hc          *http.Client
}

// Dial connects to the daemon of dir; it fails unless the daemon is running.
func Dial(dir string) (*Client, *State, error) {
	health, st, err := Check(dir)
	if err != nil {
		return nil, nil, err
	}
	if health != Running {
		return nil, st, fmt.Errorf("no exam server is running (%s)", health)
	}
	return &Client{addr: st.ControlAddr, token: st.Token, hc: &http.Client{Timeout: 10 * time.Second}}, st, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	payload := []byte("{}")
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+c.addr+path, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error string }
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return errors.New(e.Error)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) call(ctx context.Context, method, path string, body any) (*StatusView, error) {
	var v StatusView
	if err := c.do(ctx, method, path, body, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// Status returns the session status.
func (c *Client) Status(ctx context.Context) (*StatusView, error) {
	return c.call(ctx, "GET", "/status", nil)
}

// Start begins the exam for everyone in the lobby.
func (c *Client) Start(ctx context.Context) (*StatusView, error) {
	return c.call(ctx, "POST", "/start", nil)
}

// Extend adds minutes to everyone's time.
func (c *Client) Extend(ctx context.Context, minutes float64) (*StatusView, error) {
	return c.call(ctx, "POST", "/extend", map[string]any{"minutes": minutes})
}

// End finishes the exam and submits every attempt; the server keeps running.
func (c *Client) End(ctx context.Context) (*StatusView, error) {
	return c.call(ctx, "POST", "/end", nil)
}

// Shutdown stops the server, ending the exam first when end is true.
func (c *Client) Shutdown(ctx context.Context, end bool) error {
	_, err := c.call(ctx, "POST", "/shutdown", map[string]any{"end": end})
	return err
}
