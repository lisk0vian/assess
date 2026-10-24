package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/lisk0vian/assess/internal/server/student"
	"github.com/lisk0vian/assess/internal/session"
)

// StudentRow is one line of the teacher's live table.
type StudentRow struct {
	AttemptID     string               `json:"attempt_id,omitempty"` // empty: registered, not joined yet
	ParticipantID string               `json:"participant_id"`
	StudentID     string               `json:"student_id"`
	Name          string               `json:"name"`
	State         session.AttemptState `json:"state,omitempty"`
	Online        bool                 `json:"online"`
	Paused        bool                 `json:"paused"`
	Question      int                  `json:"question,omitempty"` // 1-based position the student is on
	Answered      int                  `json:"answered"`
	Total         int                  `json:"total"`
	Score         *float64             `json:"score,omitempty"`
	MaxScore      float64              `json:"max_score"`
	ExtraMs       int64                `json:"extra_ms"`
	SubmittedAt   int64                `json:"submitted_at,omitempty"`
	UsedMs        int64                `json:"used_ms,omitempty"` // from the start to submission (or now)
}

// Board is everything the live panel shows.
type Board struct {
	Status   StatusView        `json:"status"`
	Students []StudentRow      `json:"students"`
	Requests []student.Request `json:"requests"`
}

func (d *daemon) board(ctx context.Context) (*Board, error) {
	st, err := d.status(ctx)
	if err != nil {
		return nil, err
	}
	snap, err := d.svc.Snapshot(ctx, d.sessionID)
	if err != nil {
		return nil, err
	}
	presence := map[string]student.Presence{}
	for _, p := range d.students.Presence() {
		presence[p.AttemptID] = p
	}
	b := &Board{Status: *st}
	attempted := map[string]bool{}
	now := d.svc.Now()
	for _, a := range snap.Attempts {
		attempted[a.ParticipantID] = true
		p := snap.Participants[a.ParticipantID]
		pr := presence[a.ID]
		row := StudentRow{
			AttemptID: a.ID, ParticipantID: p.ID, StudentID: p.StudentID, Name: p.Name, State: a.State,
			Online: pr.Online, Paused: pr.Paused, Total: len(a.Layout), MaxScore: a.MaxScore, ExtraMs: a.Extra.Milliseconds(),
		}
		for i, sl := range a.Layout {
			if sl.QuestionID == pr.Question {
				row.Question = i + 1
			}
		}
		for _, ans := range snap.Answers[a.ID] {
			if !ans.Response.Empty() {
				row.Answered++
			}
		}
		if !a.SubmittedAt.IsZero() {
			score := a.Score
			row.Score, row.SubmittedAt = &score, a.SubmittedAt.UnixMilli()
		}
		if started := snap.Session.StartedAt; !started.IsZero() {
			end := now
			if !a.SubmittedAt.IsZero() {
				end = a.SubmittedAt
			}
			row.UsedMs = end.Sub(started).Milliseconds()
		}
		b.Students = append(b.Students, row)
	}
	for _, p := range snap.Participants {
		if !attempted[p.ID] {
			b.Students = append(b.Students, StudentRow{ParticipantID: p.ID, StudentID: p.StudentID, Name: p.Name})
		}
	}
	sort.SliceStable(b.Students, func(i, j int) bool {
		return strings.ToLower(b.Students[i].Name) < strings.ToLower(b.Students[j].Name)
	})
	for _, r := range d.students.Requests() {
		if r.Status == student.Pending {
			b.Requests = append(b.Requests, r)
		}
	}
	return b, nil
}

// studentRoutes registers the per-student actions of the control API.
func (c *controlServer) studentRoutes(mux *http.ServeMux) {
	d := c.d
	mux.HandleFunc("GET /board", func(w http.ResponseWriter, r *http.Request) {
		b, err := d.board(r.Context())
		if err != nil {
			writeControlError(w, err)
			return
		}
		writeOK(w, b)
	})
	act := func(fn func(r *http.Request, body map[string]any) error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			body := map[string]any{}
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body)
			if err := fn(r, body); err != nil {
				writeControlError(w, err)
				return
			}
			b, err := d.board(r.Context())
			if err != nil {
				writeControlError(w, err)
				return
			}
			writeOK(w, b)
		}
	}
	str := func(body map[string]any, k string) string { v, _ := body[k].(string); return v }

	mux.HandleFunc("POST /students", act(func(r *http.Request, body map[string]any) error {
		_, err := d.svc.AddParticipant(r.Context(), d.sessionID, str(body, "student_id"), str(body, "name"), session.SourceManual)
		return err
	}))
	mux.HandleFunc("DELETE /participants/{id}", act(func(r *http.Request, _ map[string]any) error {
		return d.svc.RemoveParticipant(r.Context(), r.PathValue("id"))
	}))
	mux.HandleFunc("POST /attempts/{id}/extend", act(func(r *http.Request, body map[string]any) error {
		mins, _ := body["minutes"].(float64)
		_, err := d.svc.ExtendAttempt(r.Context(), r.PathValue("id"), time.Duration(mins*float64(time.Minute)))
		return err
	}))
	mux.HandleFunc("POST /attempts/{id}/withdraw", act(func(r *http.Request, body map[string]any) error {
		_, err := d.svc.Withdraw(r.Context(), r.PathValue("id"), str(body, "note"))
		return err
	}))
	mux.HandleFunc("POST /attempts/{id}/annul", act(func(r *http.Request, body map[string]any) error {
		_, err := d.svc.Annul(r.Context(), r.PathValue("id"), str(body, "note"))
		return err
	}))
	mux.HandleFunc("POST /attempts/{id}/readmit", act(func(r *http.Request, body map[string]any) error {
		_, err := d.svc.Readmit(r.Context(), r.PathValue("id"), str(body, "note"))
		return err
	}))
	mux.HandleFunc("POST /attempts/{id}/message", act(func(r *http.Request, body map[string]any) error {
		text := strings.TrimSpace(str(body, "text"))
		if text == "" || len(text) > 500 {
			return fmt.Errorf("%w: message must have 1 to 500 characters", session.ErrInvalidInput)
		}
		d.students.Message(r.PathValue("id"), text)
		return d.svc.RecordEvent(r.Context(), &session.Event{SessionID: d.sessionID, AttemptID: r.PathValue("id"),
			Kind: "teacher.message", Severity: session.SeverityInfo, Data: map[string]any{"text": text}})
	}))
	mux.HandleFunc("POST /attempts/{id}/pause", act(func(r *http.Request, body map[string]any) error {
		on, _ := body["on"].(bool)
		d.students.SetPaused(r.PathValue("id"), on)
		kind := "teacher.resumed"
		if on {
			kind = "teacher.paused"
		}
		return d.svc.RecordEvent(r.Context(), &session.Event{SessionID: d.sessionID, AttemptID: r.PathValue("id"), Kind: kind, Severity: session.SeverityInfo})
	}))
	mux.HandleFunc("POST /requests/{id}/approve", act(func(r *http.Request, _ map[string]any) error {
		return d.students.Approve(r, r.PathValue("id"))
	}))
	mux.HandleFunc("POST /requests/{id}/reject", act(func(r *http.Request, _ map[string]any) error {
		return d.students.Reject(r.PathValue("id"))
	}))
}

func writeOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// ---- client ----

func (c *Client) board(ctx context.Context, method, path string, body any) (*Board, error) {
	var b Board
	if err := c.do(ctx, method, path, body, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// Board returns the live table.
func (c *Client) Board(ctx context.Context) (*Board, error) {
	return c.board(ctx, "GET", "/board", nil)
}

// AddStudent registers a student by hand.
func (c *Client) AddStudent(ctx context.Context, studentID, name string) (*Board, error) {
	return c.board(ctx, "POST", "/students", map[string]any{"student_id": studentID, "name": name})
}

// RemoveParticipant drops a registered student or withdraws one who joined.
func (c *Client) RemoveParticipant(ctx context.Context, participantID string) (*Board, error) {
	return c.board(ctx, "DELETE", "/participants/"+participantID, nil)
}

// AttemptAction runs extend|withdraw|annul|readmit|message|pause on an attempt.
func (c *Client) AttemptAction(ctx context.Context, attemptID, action string, body map[string]any) (*Board, error) {
	return c.board(ctx, "POST", "/attempts/"+attemptID+"/"+action, body)
}

// DecideRequest approves or rejects a device request.
func (c *Client) DecideRequest(ctx context.Context, requestID string, approve bool) (*Board, error) {
	verb := "reject"
	if approve {
		verb = "approve"
	}
	return c.board(ctx, "POST", "/requests/"+requestID+"/"+verb, nil)
}
