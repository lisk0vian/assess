package student

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/lisk0vian/assess/internal/session"
)

// pingEvery keeps the connection (and the presence count) honest through NATs
// and sleeping laptops.
const pingEvery = 15 * time.Second

// Presence is what the teacher sees live about one attempt.
type Presence struct {
	AttemptID string    `json:"attempt_id"`
	Online    bool      `json:"online"`
	Question  string    `json:"question,omitempty"` // question the student is on
	LastSeen  time.Time `json:"last_seen"`
	Paused    bool      `json:"paused"`
}

// hub delivers pushes to the session's open browsers and tracks presence.
// Pause state is kept in memory: a daemon restart clears it.
type hub struct {
	mu       sync.Mutex
	conns    map[chan []byte]string // outbox -> attempt id
	presence map[string]*Presence
}

func newHub() *hub {
	return &hub{conns: map[chan []byte]string{}, presence: map[string]*Presence{}}
}

func (h *hub) add(attemptID string) chan []byte {
	ch := make(chan []byte, 16)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[ch] = attemptID
	p := h.entry(attemptID)
	p.Online, p.LastSeen = true, time.Now()
	if p.Paused {
		ch <- encode("pause", map[string]any{"on": true})
	}
	return ch
}

func (h *hub) remove(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := h.conns[ch]
	delete(h.conns, ch)
	for _, other := range h.conns {
		if other == id {
			return
		}
	}
	if p := h.presence[id]; p != nil {
		p.Online, p.LastSeen = false, time.Now()
	}
}

func (h *hub) entry(attemptID string) *Presence {
	p := h.presence[attemptID]
	if p == nil {
		p = &Presence{AttemptID: attemptID}
		h.presence[attemptID] = p
	}
	return p
}

// send queues msg for every connection of attemptID ("" = everyone). A full
// outbox drops the message; the client reloads its state on reconnect.
func (h *hub) send(attemptID string, msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, id := range h.conns {
		if attemptID != "" && id != attemptID {
			continue
		}
		select {
		case ch <- msg:
		default:
		}
	}
}

func (h *hub) at(attemptID, question string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.entry(attemptID)
	p.Question, p.LastSeen = question, time.Now()
}

func (h *hub) snapshot() []Presence {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Presence, 0, len(h.presence))
	for _, p := range h.presence {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AttemptID < out[j].AttemptID })
	return out
}

func (h *hub) online() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, p := range h.presence {
		if p.Online {
			n++
		}
	}
	return n
}

func encode(typ string, fields map[string]any) []byte {
	m := map[string]any{"type": typ}
	for k, v := range fields {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return b
}

// Notify tells connected browsers of sessionID to refresh their state. Wire
// it to session.WithNotify.
func (h *Handler) Notify(sessionID string) {
	if sessionID == h.sessionID {
		h.live.send("", encode("changed", nil))
	}
}

// Online returns how many students have the exam open right now.
func (h *Handler) Online() int { return h.live.online() }

// Presence returns the live view of every attempt seen since start-up.
func (h *Handler) Presence() []Presence { return h.live.snapshot() }

// Message shows a teacher's message on the student's screen.
func (h *Handler) Message(attemptID, text string) {
	h.live.send(attemptID, encode("message", map[string]any{"text": text}))
}

// SetPaused pauses or resumes one student's screen. Their clock keeps running.
func (h *Handler) SetPaused(attemptID string, paused bool) {
	h.live.mu.Lock()
	h.live.entry(attemptID).Paused = paused
	h.live.mu.Unlock()
	h.live.send(attemptID, encode("pause", map[string]any{"on": paused}))
}

type clientMsg struct {
	Type     string `json:"type"`
	Question string `json:"question"`
}

// liveSocket pushes {"type":"changed"|"message"|"pause"} and receives
// {"type":"at","question":id} as the student navigates. Answers stay on HTTP.
func (h *Handler) liveSocket(w http.ResponseWriter, r *http.Request, d *session.AttemptDetail) {
	c, err := websocket.Accept(w, r, nil) // same-origin only (default origin check)
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()
	c.SetReadLimit(1 << 10)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	out := h.live.add(d.Attempt.ID)
	defer h.live.remove(out)

	go func() {
		defer cancel()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m clientMsg
			if json.Unmarshal(data, &m) == nil && m.Type == "at" && len(m.Question) <= 64 {
				h.live.at(d.Attempt.ID, m.Question)
			}
		}
	}()

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-out:
			wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.Write(wctx, websocket.MessageText, msg)
			wcancel()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		}
	}
}
