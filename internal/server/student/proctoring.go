package student

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/lisk0vian/assess/internal/proctor"
	"github.com/lisk0vian/assess/internal/session"
)

// Integrity signals travel on the live socket, signed with the attempt's
// telemetry key (session.TelemetryKey) and numbered so a replayed or forged
// message is noticed. The browser cannot use crypto.subtle over plain HTTP,
// so the web app ships its own SHA-256; the canonical string is
// "seq|kind|phase|duration_ms|question_id".

// signalMsg is {"type":"signal", ...} from the browser.
type signalMsg struct {
	Seq        int64  `json:"seq"`
	Kind       string `json:"kind"`
	Phase      string `json:"phase"` // "start" while away, "end" (or empty) when it is over
	DurationMs int64  `json:"duration_ms"`
	QuestionID string `json:"question_id"`
	At         int64  `json:"at"` // client clock, informational
	MAC        string `json:"mac"`
}

func (m signalMsg) canonical() string {
	return fmt.Sprintf("%d|%s|%s|%d|%s", m.Seq, m.Kind, m.Phase, m.DurationMs, m.QuestionID)
}

// knownSignals are the kinds a browser may report.
var knownSignals = map[string]bool{
	proctor.Hidden: true, proctor.Blur: true, proctor.Copy: true, proctor.PasteInput: true, proctor.Burst: true,
	proctor.FullscreenExit: true, proctor.Untrusted: true, proctor.Webdriver: true, proctor.DOMInjection: true,
	proctor.Resize: true, proctor.SecondTab: true,
}

// timedSignals have a start and an end; the server measures them too.
var timedSignals = map[string]bool{proctor.Hidden: true, proctor.Blur: true, proctor.FullscreenExit: true}

// verifyMAC checks the signature with the hex telemetry key.
func verifyMAC(keyHex string, m signalMsg) bool {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) == 0 {
		return false
	}
	got, err := hex.DecodeString(m.MAC)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(m.canonical()))
	return hmac.Equal(got, mac.Sum(nil))
}

// seal hides an option's text until it is revealed: the text is XORed with an
// HMAC-SHA256 keystream bound to the question and option, then base64. It
// keeps hover-only options out of the DOM and of casual network inspection;
// it is not encryption against the student's own browser.
func seal(keyHex, questionID, optionID, text string) string {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) == 0 {
		return ""
	}
	out := []byte(text)
	var block []byte
	for i := range out {
		if i%sha256.Size == 0 {
			mac := hmac.New(sha256.New, key)
			fmt.Fprintf(mac, "opt|%s|%s|", questionID, optionID)
			var n [4]byte
			binary.BigEndian.PutUint32(n[:], uint32(i/sha256.Size)) //nolint:gosec // G115: option texts are short
			mac.Write(n[:])
			block = mac.Sum(nil)
		}
		out[i] ^= block[i%sha256.Size]
	}
	return base64.StdEncoding.EncodeToString(out)
}

// away remembers when timed signals started, per attempt, so durations do not
// rest on the browser's word alone. Kept in memory.
type away struct {
	mu    sync.Mutex
	since map[string]time.Time // attempt|kind -> start
}

func (a *away) start(attemptID, kind string, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.since == nil {
		a.since = map[string]time.Time{}
	}
	// A start always wins: an episode the browser folded into another one
	// (focus loss that became a hidden tab) never sent its end.
	a.since[attemptID+"|"+kind] = at
}

func (a *away) end(attemptID, kind string, at time.Time) (time.Duration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t0, ok := a.since[attemptID+"|"+kind]
	delete(a.since, attemptID+"|"+kind)
	return at.Sub(t0), ok
}

// measureSlack absorbs network latency between the browser's start and end
// messages when comparing with the server's own measurement.
const measureSlack = 1500 * time.Millisecond

// verdict is pushed back to the student after a signal had consequences.
func verdict(kind, questionID string, out *session.SignalOutcome) []byte {
	d := out.Decision
	return encode("verdict", map[string]any{
		"kind": kind, "question_id": questionID, "warn": d.Warn, "strike": d.Strike,
		"lose_ms": d.LoseTime.Milliseconds(), "void": d.VoidQ, "strikes": out.Strikes,
		"limit": out.Limit, "annulled": out.Annulled,
	})
}

// handleSignal verifies and records one signal from the live socket.
func (h *Handler) handleSignal(ctx context.Context, d *session.AttemptDetail, key string, m signalMsg) {
	if !knownSignals[m.Kind] || len(m.QuestionID) > 64 || m.DurationMs < 0 {
		return
	}
	now := h.svc.Now()
	report := session.SignalReport{Kind: m.Kind, DurationMs: m.DurationMs, QuestionID: m.QuestionID, Seq: m.Seq}
	if m.At > 0 {
		report.ClientAt = time.UnixMilli(m.At)
	}
	if key != "" && !verifyMAC(key, m) {
		h.log.Warn("telemetry failed verification", "attempt", d.Attempt.ID, "kind", m.Kind)
		report = session.SignalReport{Kind: proctor.Telemetry, QuestionID: m.QuestionID, Data: map[string]any{"claimed": m.Kind}}
	} else if timedSignals[m.Kind] {
		if m.Phase == "start" {
			h.away.start(d.Attempt.ID, m.Kind, now)
			return
		}
		if srv, ok := h.away.end(d.Attempt.ID, m.Kind, now); ok {
			report.Data = map[string]any{"client_ms": m.DurationMs, "server_ms": srv.Milliseconds()}
			report.DurationMs = max(m.DurationMs, (srv - measureSlack).Milliseconds())
		}
	}
	out, err := h.svc.ReportSignal(ctx, d.Attempt.ID, report)
	if err != nil || out.Duplicate || out.Decision.Ignore {
		return
	}
	if out.Decision.Warn || len(out.Penalties) > 0 {
		h.live.send(d.Attempt.ID, verdict(report.Kind, report.QuestionID, out))
	}
	if out.Annulled {
		h.log.Warn("attempt annulled by strikes", "attempt", d.Attempt.ID, "strikes", out.Strikes)
	}
}

// gapAfter is how long the live channel must be silent before answers arriving
// over HTTP are flagged: telemetry may have been blocked on purpose.
const gapAfter = time.Minute

// checkGap flags answers that arrive while the live channel is down.
func (h *Handler) checkGap(ctx context.Context, d *session.AttemptDetail) {
	if !session.ProctorSettings(&d.Exam.Exam.Manifest).Enabled() || d.Session.State != session.StateRunning {
		return
	}
	now := time.Now()
	if !h.live.silentFor(d.Attempt.ID, gapAfter, now) {
		return
	}
	h.gapMu.Lock()
	last := h.gaps[d.Attempt.ID]
	fresh := now.Sub(last) > 2*time.Minute
	if fresh {
		h.gaps[d.Attempt.ID] = now
	}
	h.gapMu.Unlock()
	if fresh {
		_, _ = h.svc.ReportSignal(ctx, d.Attempt.ID, session.SignalReport{Kind: proctor.TelemetryGap})
	}
}

func (h *Handler) pledge(w http.ResponseWriter, r *http.Request, d *session.AttemptDetail) {
	if _, err := h.svc.Pledge(r.Context(), d.Attempt.ID); err != nil {
		h.fail(w, err)
		return
	}
	fresh, err := h.svc.Attempt(r.Context(), d.Attempt.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.writeMe(w, fresh)
}
