package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/report"
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
	Strikes       int                  `json:"strikes"`           // active strikes
	Signals       int                  `json:"signals"`           // warn and high integrity signals
	AwayHits      int                  `json:"away_hits"`         // correct answers right after leaving the exam
	NoLens        bool                 `json:"no_lens,omitempty"` // reading accommodation: no lens
}

// Incident is one integrity signal in the live log, with what it cost.
type Incident struct {
	EventID    int64              `json:"event_id"`
	AttemptID  string             `json:"attempt_id"`
	Name       string             `json:"name"`
	StudentID  string             `json:"student_id"`
	Kind       string             `json:"kind"` // proctor signal kind
	Severity   session.Severity   `json:"severity"`
	Question   int                `json:"question,omitempty"` // 1-based position in the attempt
	DurationMs int64              `json:"duration_ms,omitempty"`
	At         int64              `json:"at"`
	Penalties  []*session.Penalty `json:"penalties,omitempty"`
}

// Board is everything the live panel shows.
type Board struct {
	Status      StatusView        `json:"status"`
	Students    []StudentRow      `json:"students"`
	Requests    []student.Request `json:"requests"`
	StrikeLimit int               `json:"strike_limit"` // 0: proctoring off
	AllPaused   bool              `json:"all_paused"`   // the teacher paused everyone
	Lens        bool              `json:"lens"`         // some prompts show through the lens
	Incidents   []Incident        `json:"incidents"`    // newest first
	AwayHits    []report.AwayHit  `json:"away_hits"`    // left and got it right, hardest questions first
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
	b := &Board{Status: *st, AllPaused: d.students.AllPaused()}
	_, pub, err := d.svc.Session(ctx, d.sessionID)
	if err != nil {
		return nil, err
	}
	if set := session.ProctorSettings(&pub.Exam.Manifest); set.Enabled() {
		b.StrikeLimit = set.StrikeLimit
		b.Lens = set.Lens
		for _, q := range pub.Exam.Questions {
			b.Lens = b.Lens || q.Lens != nil && *q.Lens
		}
	}
	attempted := map[string]bool{}
	byAttempt := map[string]*session.Attempt{}
	now := d.svc.Now()
	for _, a := range snap.Attempts {
		attempted[a.ParticipantID] = true
		byAttempt[a.ID] = a
		p := snap.Participants[a.ParticipantID]
		pr := presence[a.ID]
		row := StudentRow{
			AttemptID: a.ID, ParticipantID: p.ID, StudentID: p.StudentID, Name: p.Name, State: a.State,
			Online: pr.Online, Paused: pr.Paused, Total: len(a.Layout), MaxScore: a.MaxScore, ExtraMs: a.Extra.Milliseconds(),
			Strikes: session.ActiveStrikes(snap.Penalties[a.ID]), Signals: snap.SignalCounts[a.ID],
			NoLens: a.NoLens,
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
	b.Incidents = incidents(snap, byAttempt)
	if b.StrikeLimit > 0 {
		if err := d.awayHits(ctx, b, snap, pub); err != nil {
			return nil, err
		}
	}
	for _, r := range d.students.Requests() {
		if r.Status == student.Pending {
			b.Requests = append(b.Requests, r)
		}
	}
	return b, nil
}

// awayHitsShown bounds the "left and got it right" list of the panel.
const awayHitsShown = 20

// awayHits marks correct answers given right after leaving the question.
func (d *daemon) awayHits(ctx context.Context, b *Board, snap *session.Snapshot, pub *exam.Published) error {
	events, err := d.svc.Events(ctx, d.sessionID, report.AwaySignalKinds()...)
	if err != nil {
		return err
	}
	hits := report.AwayHits(&session.Archive{Snapshot: snap, Published: pub, Events: events})
	per := map[string]int{}
	for _, h := range hits {
		per[h.StudentID]++
	}
	for i := range b.Students {
		b.Students[i].AwayHits = per[b.Students[i].StudentID]
	}
	b.AwayHits = hits[:min(len(hits), awayHitsShown)]
	return nil
}

// incidents joins the newest signals with their students and penalties.
func incidents(snap *session.Snapshot, byAttempt map[string]*session.Attempt) []Incident {
	byEvent := map[int64][]*session.Penalty{}
	for _, ps := range snap.Penalties {
		for _, p := range ps {
			if p.EventID > 0 {
				byEvent[p.EventID] = append(byEvent[p.EventID], p)
			}
		}
	}
	out := make([]Incident, 0, len(snap.Incidents))
	for _, e := range snap.Incidents {
		in := Incident{EventID: e.ID, AttemptID: e.AttemptID, Kind: strings.TrimPrefix(e.Kind, "signal."),
			Severity: e.Severity, At: e.ReceivedAt.UnixMilli(), Penalties: byEvent[e.ID]}
		if a := byAttempt[e.AttemptID]; a != nil {
			if p := snap.Participants[a.ParticipantID]; p != nil {
				in.Name, in.StudentID = p.Name, p.StudentID
			}
			for i, sl := range a.Layout {
				if sl.QuestionID == e.QuestionID {
					in.Question = i + 1
				}
			}
		}
		if v, ok := e.Data["duration_ms"].(float64); ok {
			in.DurationMs = int64(v)
		}
		out = append(out, in)
	}
	return out
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

	mux.HandleFunc("POST /penalties/{id}/revert", act(func(r *http.Request, body map[string]any) error {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			return fmt.Errorf("%w: penalty id", session.ErrInvalidInput)
		}
		_, err = d.svc.RevertPenalty(r.Context(), id, str(body, "note"))
		return err
	}))
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
	mux.HandleFunc("POST /attempts/{id}/reading", act(func(r *http.Request, body map[string]any) error {
		noLens, _ := body["no_lens"].(bool)
		_, err := d.svc.SetReading(r.Context(), r.PathValue("id"), noLens)
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
	mux.HandleFunc("POST /message", act(func(r *http.Request, body map[string]any) error {
		text := strings.TrimSpace(str(body, "text"))
		if text == "" || len(text) > 500 {
			return fmt.Errorf("%w: message must have 1 to 500 characters", session.ErrInvalidInput)
		}
		d.students.MessageAll(text)
		return d.svc.RecordEvent(r.Context(), &session.Event{SessionID: d.sessionID, Kind: "teacher.message_all",
			Severity: session.SeverityInfo, Data: map[string]any{"text": text}})
	}))
	mux.HandleFunc("POST /pause", act(func(r *http.Request, body map[string]any) error {
		on, _ := body["on"].(bool)
		d.students.SetPausedAll(on)
		kind := "teacher.resumed_all"
		if on {
			kind = "teacher.paused_all"
		}
		return d.svc.RecordEvent(r.Context(), &session.Event{SessionID: d.sessionID, Kind: kind, Severity: session.SeverityInfo})
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

// AttemptAction runs extend|withdraw|annul|readmit|message|pause|reading on an attempt.
func (c *Client) AttemptAction(ctx context.Context, attemptID, action string, body map[string]any) (*Board, error) {
	return c.board(ctx, "POST", "/attempts/"+attemptID+"/"+action, body)
}

// RevertPenalties reverts penalties with a note, one by one.
func (c *Client) RevertPenalties(ctx context.Context, ids []int64, note string) (*Board, error) {
	var b *Board
	for _, id := range ids {
		var err error
		if b, err = c.board(ctx, "POST", fmt.Sprintf("/penalties/%d/revert", id), map[string]any{"note": note}); err != nil {
			return nil, err
		}
	}
	if b == nil {
		return c.Board(ctx)
	}
	return b, nil
}

// MessageAll shows a message to every student.
func (c *Client) MessageAll(ctx context.Context, text string) (*Board, error) {
	return c.board(ctx, "POST", "/message", map[string]any{"text": text})
}

// PauseAll pauses or resumes everyone.
func (c *Client) PauseAll(ctx context.Context, on bool) (*Board, error) {
	return c.board(ctx, "POST", "/pause", map[string]any{"on": on})
}

// DecideRequest approves or rejects a device request.
func (c *Client) DecideRequest(ctx context.Context, requestID string, approve bool) (*Board, error) {
	verb := "reject"
	if approve {
		verb = "approve"
	}
	return c.board(ctx, "POST", "/requests/"+requestID+"/"+verb, nil)
}
