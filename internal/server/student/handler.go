package student

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/lisk0vian/assess/internal/grading"
	"github.com/lisk0vian/assess/internal/session"
)

// CookieName holds the attempt id, which is the student's credential.
const CookieName = "ax_attempt"

const maxBody = 16 << 10

// Options tunes the student API.
type Options struct {
	// JoinWindow closes the door this long after the exam starts; students
	// who are already in keep resuming. Zero keeps joining open.
	JoinWindow time.Duration
	Log        *slog.Logger
	// OnRequest is called when a device asks for the teacher's approval.
	OnRequest func()
}

// Handler serves the student API for one session.
type Handler struct {
	svc       *session.Service
	sessionID string
	opts      Options
	log       *slog.Logger
	mux       *http.ServeMux
	live      *hub
	joins     *limiter
	reqs      *requests
	away      away

	gapMu sync.Mutex
	gaps  map[string]time.Time // last telemetry gap reported per attempt
}

// New returns the student API for sessionID.
func New(svc *session.Service, sessionID string, opts Options) *Handler {
	log := opts.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	h := &Handler{svc: svc, sessionID: sessionID, opts: opts, log: log, mux: http.NewServeMux(),
		live: newHub(), joins: newLimiter(12, time.Minute), reqs: newRequests(), gaps: map[string]time.Time{}}
	h.mux.HandleFunc("POST /api/join", h.join)
	h.mux.HandleFunc("GET /api/join/status", h.pendingStatus)
	h.mux.HandleFunc("GET /api/me", h.withAttempt(h.me))
	h.mux.HandleFunc("GET /api/questions/{id}", h.withAttempt(h.question))
	h.mux.HandleFunc("PUT /api/answers/{id}", h.withAttempt(h.answer))
	h.mux.HandleFunc("POST /api/submit", h.withAttempt(h.submit))
	h.mux.HandleFunc("POST /api/pledge", h.withAttempt(h.pledge))
	h.mux.HandleFunc("GET /api/assets/{path...}", h.withAttempt(h.asset))
	h.mux.HandleFunc("GET /api/ws", h.withAttempt(h.liveSocket))
	h.mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "unknown endpoint")
	})
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	h.mux.ServeHTTP(w, r)
}

// ---- join ----

type joinRequest struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	StudentID string `json:"student_id"`
}

func (h *Handler) join(w http.ResponseWriter, r *http.Request) {
	if !h.joins.allow(clientIP(r), time.Now()) {
		writeError(w, http.StatusTooManyRequests, "too_many_attempts", "too many attempts; wait a minute")
		return
	}
	var req joinRequest
	if !decode(w, r, &req) {
		return
	}
	sess, _, err := h.svc.Session(r.Context(), h.sessionID)
	if err != nil {
		h.fail(w, err)
		return
	}
	if !strings.EqualFold(strings.TrimSpace(req.Code), sess.Code) {
		writeError(w, http.StatusNotFound, "bad_code", "no open exam uses that code")
		return
	}

	// A browser that already holds this student's attempt resumes it.
	if d := h.cookieAttempt(r); d != nil && d.Participant.StudentID == session.NormalizeStudentID(req.StudentID) {
		h.writeMe(w, d)
		return
	}
	name := strings.Join(strings.Fields(req.Name), " ")
	if win := h.opts.JoinWindow; win > 0 && sess.State == session.StateRunning && h.svc.Now().After(sess.StartedAt.Add(win)) {
		if existing, err := h.svc.AttemptOfStudent(r.Context(), h.sessionID, req.StudentID); err == nil {
			h.askApproval(w, r, KindDevice, req.StudentID, name, existing.ID)
		} else {
			h.askApproval(w, r, KindLate, req.StudentID, name, "")
		}
		return
	}
	att, _, err := h.svc.Join(r.Context(), h.sessionID, req.StudentID, req.Name)
	if errors.Is(err, session.ErrAlreadyJoined) {
		existing, lookupErr := h.svc.AttemptOfStudent(r.Context(), h.sessionID, req.StudentID)
		if lookupErr != nil {
			h.fail(w, lookupErr)
			return
		}
		h.askApproval(w, r, KindDevice, req.StudentID, name, existing.ID)
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	h.setAttemptCookie(w, r, att.ID)
	d, err := h.svc.Attempt(r.Context(), att.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.log.Info("student joined", "attempt", att.ID, "student", d.Participant.StudentID)
	h.writeMe(w, d)
}

// setAttemptCookie hands the device its credential. Secure follows the
// transport: classroom LANs run plain HTTP by design (TLS is opt-in).
func (h *Handler) setAttemptCookie(w http.ResponseWriter, r *http.Request, attemptID string) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: see comment above
		Name: CookieName, Value: attemptID, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: int((24 * time.Hour).Seconds()),
	})
}

func (h *Handler) onRequest() {
	if h.opts.OnRequest != nil {
		h.opts.OnRequest()
	}
}

// ---- authenticated endpoints ----

type attemptHandler func(http.ResponseWriter, *http.Request, *session.AttemptDetail)

func (h *Handler) withAttempt(next attemptHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := h.cookieAttempt(r)
		if d == nil {
			writeError(w, http.StatusUnauthorized, "no_attempt", "join the exam first")
			return
		}
		next(w, r, d)
	}
}

func (h *Handler) cookieAttempt(r *http.Request) *session.AttemptDetail {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" || len(c.Value) > 64 {
		return nil
	}
	d, err := h.svc.Attempt(r.Context(), c.Value)
	if err != nil || d.Session.ID != h.sessionID {
		return nil
	}
	return d
}

func (h *Handler) me(w http.ResponseWriter, _ *http.Request, d *session.AttemptDetail) {
	h.writeMe(w, d)
}

func (h *Handler) writeMe(w http.ResponseWriter, d *session.AttemptDetail) {
	writeJSON(w, http.StatusOK, buildMe(d, h.svc.Now()))
}

func (h *Handler) question(w http.ResponseWriter, r *http.Request, d *session.AttemptDetail) {
	if d.Session.State == session.StateLobby {
		writeError(w, http.StatusConflict, "not_started", "the exam has not started")
		return
	}
	id := r.PathValue("id")
	if !inLayout(d, id) {
		writeError(w, http.StatusNotFound, "unknown_question", "question is not part of this attempt")
		return
	}
	// Opening a timed question starts its clock, once.
	if d.Exam.Exam.Questions[id].TimeLimit.D() > 0 && d.Attempt.State == session.AttemptActive {
		if _, ok := d.Opened[id]; !ok {
			closes, err := h.svc.OpenQuestion(r.Context(), d.Attempt.ID, id)
			if err != nil {
				h.fail(w, err)
				return
			}
			d.Opened[id] = closes.Add(-d.Exam.Exam.Questions[id].TimeLimit.D())
		}
	}
	q, _ := buildQuestion(d, id)
	writeJSON(w, http.StatusOK, q)
}

type answerRequest struct {
	Seq      int64            `json:"seq"`
	Response grading.Response `json:"response"`
}

type answerResponse struct {
	Applied   bool  `json:"applied"`
	ServerNow int64 `json:"server_now"`
}

func (h *Handler) answer(w http.ResponseWriter, r *http.Request, d *session.AttemptDetail) {
	var req answerRequest
	if !decode(w, r, &req) {
		return
	}
	applied, err := h.svc.SaveAnswer(r.Context(), d.Attempt.ID, r.PathValue("id"), req.Seq, req.Response)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.checkGap(r.Context(), d)
	writeJSON(w, http.StatusOK, answerResponse{Applied: applied, ServerNow: h.svc.Now().UnixMilli()})
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request, d *session.AttemptDetail) {
	if _, err := h.svc.Submit(r.Context(), d.Attempt.ID); err != nil {
		h.fail(w, err)
		return
	}
	fresh, err := h.svc.Attempt(r.Context(), d.Attempt.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.log.Info("attempt submitted", "attempt", d.Attempt.ID)
	h.writeMe(w, fresh)
}

func (h *Handler) asset(w http.ResponseWriter, r *http.Request, d *session.AttemptDetail) {
	rel := path.Clean("assets/" + r.PathValue("path"))
	data, ok := d.Exam.Assets[rel]
	if !ok || !strings.HasPrefix(rel, "assets/") {
		writeError(w, http.StatusNotFound, "not_found", "asset not found")
		return
	}
	ctype := mime.TypeByExtension(path.Ext(rel))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	// SVGs can carry scripts: render them inert if opened directly.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(data) //nolint:gosec // G705: exam asset bytes, typed, nosniff and CSP sandbox
}

// ---- helpers ----

type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: code, Message: msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return false
	}
	return true
}

// fail maps domain errors to HTTP responses; anything unexpected is a 500.
func (h *Handler) fail(w http.ResponseWriter, err error) {
	type mapping struct {
		target error
		status int
		code   string
	}
	for _, m := range []mapping{
		{session.ErrInvalidInput, http.StatusBadRequest, "invalid_input"},
		{session.ErrNotFound, http.StatusNotFound, "not_found"},
		{session.ErrUnknownQuestion, http.StatusNotFound, "unknown_question"},
		{session.ErrNotStarted, http.StatusConflict, "not_started"},
		{session.ErrSessionClosed, http.StatusConflict, "session_closed"},
		{session.ErrAttemptClosed, http.StatusConflict, "attempt_closed"},
		{session.ErrTimeUp, http.StatusConflict, "time_up"},
		{session.ErrDuplicate, http.StatusConflict, "duplicate"},
		{session.ErrQuestionClosed, http.StatusConflict, "question_closed"},
	} {
		if errors.Is(err, m.target) {
			writeError(w, m.status, m.code, err.Error())
			return
		}
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	h.log.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "unexpected server error")
}
