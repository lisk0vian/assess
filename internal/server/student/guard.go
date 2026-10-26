package student

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/lisk0vian/assess/internal/netutil"
)

// LocalNetworksOnly rejects requests from outside the classroom network,
// so a leaked link or code is useless elsewhere. It lets in loopback,
// private, link-local and carrier-grade NAT addresses, and any address in
// one of nets (the subnets of the teacher's computer), which covers
// networks numbered with public ranges.
func LocalNetworksOnly(next http.Handler, nets []*net.IPNet) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !netutil.IsLocal(net.ParseIP(host), nets) {
			writeError(w, http.StatusForbidden, "outside_network",
				"this exam only accepts devices on the classroom network (the teacher can allow others with server.allow_public)")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limiter allows `burst` join attempts per IP per `window`, enough for typos
// but not for guessing student IDs.
type limiter struct {
	mu     sync.Mutex
	window time.Duration
	burst  int
	hits   map[string][]time.Time
}

func newLimiter(burst int, window time.Duration) *limiter {
	return &limiter{window: window, burst: burst, hits: map[string][]time.Time{}}
}

func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.burst {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
