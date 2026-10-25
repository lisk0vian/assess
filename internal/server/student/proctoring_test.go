package student

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/lisk0vian/assess/internal/exam"
)

func sign(key string, m signalMsg) signalMsg {
	k, _ := hex.DecodeString(key)
	mac := hmac.New(sha256.New, k)
	mac.Write([]byte(m.canonical()))
	m.MAC = hex.EncodeToString(mac.Sum(nil))
	return m
}

func unseal(key, q, o, sealed string) string {
	k, _ := hex.DecodeString(key)
	b, _ := base64.StdEncoding.DecodeString(sealed)
	var block []byte
	for i := range b {
		if i%32 == 0 {
			mac := hmac.New(sha256.New, k)
			fmt.Fprintf(mac, "opt|%s|%s|", q, o)
			var n [4]byte
			binary.BigEndian.PutUint32(n[:], uint32(i/32))
			mac.Write(n[:])
			block = mac.Sum(nil)
		}
		b[i] ^= block[i%32]
	}
	return string(b)
}

func (c *client) dial(ctx context.Context) *websocket.Conn {
	c.e.t.Helper()
	ws, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(c.e.srv.URL, "http")+"/api/ws", &websocket.DialOptions{
		HTTPClient: c.http, HTTPHeader: http.Header{"Origin": {c.e.srv.URL}},
	})
	if err != nil {
		c.e.t.Fatal(err)
	}
	if resp.Body != nil {
		resp.Body.Close()
	}
	return ws
}

func send(ctx context.Context, t *testing.T, ws *websocket.Conn, m signalMsg) {
	t.Helper()
	b, _ := json.Marshal(struct {
		Type string `json:"type"`
		signalMsg
	}{"signal", m})
	if err := ws.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

// nextVerdict skips other pushes until a verdict arrives.
func nextVerdict(ctx context.Context, t *testing.T, ws *websocket.Conn) map[string]any {
	t.Helper()
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if m["type"] == "verdict" {
			return m
		}
	}
}

func TestSignedSignals(t *testing.T) {
	e := newEnv(t, "")
	c := e.browser()
	c.mustStatus(http.StatusOK, "POST", "/api/join", map[string]string{"code": e.sess.Code, "name": "Ana", "student_id": "A1"}, nil)
	if _, err := e.svc.Start(context.Background(), e.sess.ID); err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(20 * time.Second)
	var me MeView
	c.mustStatus(http.StatusOK, "GET", "/api/me", nil, &me)
	key := me.Proctoring.Key
	if key == "" || me.Proctoring.Profile != "standard" || me.Proctoring.StrikeLimit != 3 {
		t.Fatalf("proctoring view %+v", me.Proctoring)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws := c.dial(ctx)
	defer ws.CloseNow()

	// The browser claims 2 s away, but the server saw 40 s between messages.
	send(ctx, t, ws, sign(key, signalMsg{Seq: 1, Kind: "hidden", Phase: "start"}))
	time.Sleep(100 * time.Millisecond) // let the start land before the clock moves
	e.clk.Advance(40 * time.Second)
	send(ctx, t, ws, sign(key, signalMsg{Seq: 2, Kind: "hidden", Phase: "end", DurationMs: 2000}))
	if v := nextVerdict(ctx, t, ws); v["kind"] != "hidden" || v["strike"] != true || v["strikes"] != 1.0 {
		t.Fatalf("hidden verdict %v", v)
	}

	// A forged signature counts as tampering.
	forged := signalMsg{Seq: 3, Kind: "resize", MAC: strings.Repeat("0", 64)}
	send(ctx, t, ws, forged)
	if v := nextVerdict(ctx, t, ws); v["kind"] != "telemetry" || v["strikes"] != 2.0 {
		t.Fatalf("forged verdict %v", v)
	}

	c.mustStatus(http.StatusOK, "POST", "/api/pledge", nil, &me)
	if !me.Proctoring.Pledged || me.Proctoring.Strikes != 2 || len(me.Penalties) != 2 {
		t.Fatalf("after signals: %+v, %d penalties", me.Proctoring, len(me.Penalties))
	}
}

func TestHoverOptionsAreSealed(t *testing.T) {
	e := newEnvWith(t, func(p *exam.Published) { p.Exam.Manifest.Proctoring.Reveal = "hover" })
	c := e.browser()
	var me MeView
	c.mustStatus(http.StatusOK, "POST", "/api/join", map[string]string{"code": e.sess.Code, "name": "Ana", "student_id": "A1"}, &me)
	if _, err := e.svc.Start(context.Background(), e.sess.ID); err != nil {
		t.Fatal(err)
	}
	var q QuestionView
	c.mustStatus(http.StatusOK, "GET", "/api/questions/q001", nil, &q)
	if q.Reveal != "hover" || len(q.Options) == 0 {
		t.Fatalf("question %+v", q)
	}
	pub, _, err := exam.Publish("../../../examples/algebra-01")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, o := range pub.Exam.Questions["q001"].Options {
		want[o.ID] = o.Text
	}
	for _, o := range q.Options {
		if o.Text != "" || o.Sealed == "" {
			t.Fatalf("option %s leaks its text", o.ID)
		}
		if got := unseal(me.Proctoring.Key, "q001", o.ID, o.Sealed); got != want[o.ID] {
			t.Fatalf("option %s unseals to %q, want %q", o.ID, got, want[o.ID])
		}
	}
}
