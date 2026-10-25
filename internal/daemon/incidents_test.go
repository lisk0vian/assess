package daemon

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestIncidentsAndRevert(t *testing.T) {
	c, st := startDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	body := `{"code":"` + st.Code + `","name":"Ana Pérez","student_id":"A1"}`
	resp, err := browser.Post(st.StudentURL+"api/join", "application/json", strings.NewReader(body))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("join: %v %v", err, resp)
	}
	resp.Body.Close()
	if _, err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}

	origin := strings.TrimSuffix(st.StudentURL, "/")
	ws, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(origin, "http")+"/api/ws", &websocket.DialOptions{
		HTTPClient: browser, HTTPHeader: http.Header{"Origin": {origin}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body != nil {
		resp.Body.Close()
	}
	defer ws.CloseNow()
	// A forged signature counts even during the start grace period.
	forged := `{"type":"signal","seq":1,"kind":"copy","phase":"","duration_ms":0,"question_id":"","mac":"` + strings.Repeat("0", 64) + `"}`
	if err := ws.Write(ctx, websocket.MessageText, []byte(forged)); err != nil {
		t.Fatal(err)
	}

	var b *Board
	for {
		if b, err = c.Board(ctx); err != nil {
			t.Fatal(err)
		}
		if len(b.Incidents) > 0 {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("the incident never reached the board")
		}
		time.Sleep(50 * time.Millisecond)
	}
	in := b.Incidents[0]
	if in.Kind != "telemetry" || in.Name != "Ana Pérez" || len(in.Penalties) != 1 || b.StrikeLimit != 3 || b.Students[0].Strikes != 1 {
		t.Fatalf("incident %+v, limit %d, row %+v", in, b.StrikeLimit, b.Students[0])
	}
	if b, err = c.RevertPenalties(ctx, []int64{in.Penalties[0].ID}, "falso positivo"); err != nil {
		t.Fatal(err)
	}
	if b.Students[0].Strikes != 0 || b.Incidents[0].Penalties[0].Active() || b.Incidents[0].Penalties[0].Note != "falso positivo" {
		t.Fatalf("after revert: row %+v, penalty %+v", b.Students[0], b.Incidents[0].Penalties[0])
	}
	if _, err = c.RevertPenalties(ctx, []int64{in.Penalties[0].ID}, ""); err == nil {
		t.Fatal("reverting twice must fail")
	}
}
