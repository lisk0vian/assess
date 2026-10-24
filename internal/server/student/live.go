package student

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/lisk0vian/assess/internal/session"
)

// pingEvery keeps the connection (and the presence count) honest through NATs
// and sleeping laptops.
const pingEvery = 15 * time.Second

// hub fans "changed" notifications out to the session's open browsers and
// counts who is online.
type hub struct {
	mu    sync.Mutex
	conns map[chan struct{}]string // signal channel -> attempt id
}

func newHub() *hub { return &hub{conns: map[chan struct{}]string{}} }

func (h *hub) add(attemptID string) chan struct{} {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.conns[ch] = attemptID
	h.mu.Unlock()
	return ch
}

func (h *hub) remove(ch chan struct{}) {
	h.mu.Lock()
	delete(h.conns, ch)
	h.mu.Unlock()
}

// signal wakes every connection; a pending signal is enough, so it never blocks.
func (h *hub) signal() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.conns {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// online counts distinct attempts with at least one open connection.
func (h *hub) online() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := map[string]bool{}
	for _, id := range h.conns {
		seen[id] = true
	}
	return len(seen)
}

// Notify tells connected browsers of sessionID to refresh their state. Wire
// it to session.WithNotify.
func (h *Handler) Notify(sessionID string) {
	if sessionID == h.sessionID {
		h.live.signal()
	}
}

// Online returns how many students have the exam open right now.
func (h *Handler) Online() int { return h.live.online() }

// liveSocket streams {"type":"changed"} whenever the session or the attempt
// changes; the client then reloads /api/me. Answers stay on plain HTTP.
func (h *Handler) liveSocket(w http.ResponseWriter, r *http.Request, d *session.AttemptDetail) {
	c, err := websocket.Accept(w, r, nil) // same-origin only (default origin check)
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()
	ctx := c.CloseRead(r.Context())
	ch := h.live.add(d.Attempt.ID)
	defer h.live.remove(ch)

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.Write(wctx, websocket.MessageText, []byte(`{"type":"changed"}`))
			cancel()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
