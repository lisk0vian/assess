package student

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/lisk0vian/assess/internal/session"
)

// PendingCookie identifies a device waiting for the teacher's approval.
const PendingCookie = "ax_pending"

// RequestKind says why a device needs approval.
type RequestKind string

// Request kinds.
const (
	KindDevice RequestKind = "device" // the student already has an attempt on another device
	KindLate   RequestKind = "late"   // joining after the join window closed
	// KindUnlisted is a student outside the class list of a roster-only session.
	KindUnlisted RequestKind = "unlisted"
)

// RequestStatus is the teacher's decision.
type RequestStatus string

// Request statuses.
const (
	Pending  RequestStatus = "pending"
	Approved RequestStatus = "approved"
	Rejected RequestStatus = "rejected"
)

// Request is a device asking the teacher to be let in.
type Request struct {
	ID        string        `json:"id"`
	Kind      RequestKind   `json:"kind"`
	StudentID string        `json:"student_id"`
	Name      string        `json:"name"`
	Status    RequestStatus `json:"status"`
	CreatedAt time.Time     `json:"created_at"`
	AttemptID string        `json:"attempt_id,omitempty"` // set when approved (or known for device requests)
	IP        string        `json:"ip"`
}

// ErrUnknownRequest is returned for a request id that does not exist.
var ErrUnknownRequest = errors.New("unknown request")

type requests struct {
	mu   sync.Mutex
	byID map[string]*Request
}

func newRequests() *requests { return &requests{byID: map[string]*Request{}} }

func (q *requests) add(r *Request) {
	q.mu.Lock()
	defer q.mu.Unlock()
	// One open request per student and kind: a retry replaces the old one.
	for id, old := range q.byID {
		if old.Status == Pending && old.StudentID == r.StudentID && old.Kind == r.Kind {
			delete(q.byID, id)
		}
	}
	q.byID[r.ID] = r
}

func (q *requests) get(id string) (Request, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	r, ok := q.byID[id]
	if !ok {
		return Request{}, false
	}
	return *r, true
}

func (q *requests) set(id string, fn func(*Request) error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	r, ok := q.byID[id]
	if !ok {
		return ErrUnknownRequest
	}
	return fn(r)
}

// Requests lists approval requests, newest first.
func (h *Handler) Requests() []Request {
	h.reqs.mu.Lock()
	defer h.reqs.mu.Unlock()
	out := make([]Request, 0, len(h.reqs.byID))
	for _, r := range h.reqs.byID {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Approve lets the device in: a device request shares the existing attempt,
// a late request creates the attempt now.
func (h *Handler) Approve(r *http.Request, id string) error {
	req, ok := h.reqs.get(id)
	if !ok {
		return ErrUnknownRequest
	}
	attemptID := req.AttemptID
	if req.Kind == KindLate || req.Kind == KindUnlisted {
		att, _, err := h.svc.Join(r.Context(), h.sessionID, req.StudentID, req.Name)
		if err != nil {
			return err
		}
		attemptID = att.ID
	}
	if err := h.reqs.set(id, func(x *Request) error {
		x.Status, x.AttemptID = Approved, attemptID
		return nil
	}); err != nil {
		return err
	}
	_ = h.svc.RecordEvent(r.Context(), &session.Event{SessionID: h.sessionID, AttemptID: attemptID, Kind: "attempt.device_approved",
		Severity: session.SeverityWarn, Data: map[string]any{"kind": string(req.Kind), "ip": req.IP}})
	return nil
}

// Enroll approves a request and adds the student to the session's class.
func (h *Handler) Enroll(r *http.Request, id string) error {
	req, ok := h.reqs.get(id)
	if !ok {
		return ErrUnknownRequest
	}
	if err := h.svc.AddToSessionClass(r.Context(), h.sessionID, req.StudentID, req.Name); err != nil {
		return err
	}
	return h.Approve(r, id)
}

// Reject denies the device.
func (h *Handler) Reject(id string) error {
	return h.reqs.set(id, func(x *Request) error {
		x.Status = Rejected
		return nil
	})
}

// pendingStatus lets a waiting device learn the decision. Once approved, the
// device receives the attempt cookie and its state.
func (h *Handler) pendingStatus(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(PendingCookie)
	if err != nil {
		writeError(w, http.StatusNotFound, "no_request", "no pending request")
		return
	}
	req, ok := h.reqs.get(c.Value)
	if !ok {
		writeError(w, http.StatusNotFound, "no_request", "no pending request")
		return
	}
	switch req.Status {
	case Pending:
		writeJSON(w, http.StatusAccepted, map[string]string{"status": string(Pending)})
	case Rejected:
		clearPending(w)
		writeError(w, http.StatusForbidden, "rejected", "the teacher did not approve this device")
	default:
		clearPending(w)
		h.setAttemptCookie(w, r, req.AttemptID)
		d, err := h.svc.Attempt(r.Context(), req.AttemptID)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.writeMe(w, d)
	}
}

func clearPending(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: PendingCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode}) //nolint:gosec // G124: deletion cookie
}

// askApproval records a request and answers 409 pending_approval.
func (h *Handler) askApproval(w http.ResponseWriter, r *http.Request, kind RequestKind, studentID, name, attemptID string) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	req := &Request{ID: hex.EncodeToString(b), Kind: kind, StudentID: session.NormalizeStudentID(studentID), Name: name,
		Status: Pending, CreatedAt: time.Now(), AttemptID: attemptID, IP: clientIP(r)}
	h.reqs.add(req)
	http.SetCookie(w, &http.Cookie{Name: PendingCookie, Value: req.ID, Path: "/", HttpOnly: true, //nolint:gosec // G124: Secure follows the transport (see setAttemptCookie)
		SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: int((2 * time.Hour).Seconds())})
	h.log.Info("approval requested", "kind", kind, "student", req.StudentID)
	h.onRequest()
	writeError(w, http.StatusConflict, "pending_approval", "waiting for the teacher to approve this device")
}
