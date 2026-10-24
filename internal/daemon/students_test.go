package daemon

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lisk0vian/assess/internal/session"
)

// startDaemon runs a daemon on loopback for the test and returns its client.
func startDaemon(t *testing.T) (*Client, *State) {
	t.Helper()
	dir := t.TempDir()
	openSession(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, Config{DataDir: dir, Host: "127.0.0.1", Port: 0, Version: "test"}) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if c, st, err := Dial(dir); err == nil {
			return c, st
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon never became ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestBoardAndStudentActions(t *testing.T) {
	c, st := startDaemon(t)
	ctx := context.Background()

	body := `{"code":"` + st.Code + `","name":"Ana Pérez","student_id":"A1"}`
	resp, err := http.Post(st.StudentURL+"api/join", "application/json", strings.NewReader(body))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("join: %v %v", err, resp)
	}
	resp.Body.Close()

	b, err := c.AddStudent(ctx, "z9", "Zoe Invitada")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Students) != 2 || b.Students[0].Name != "Ana Pérez" || b.Students[1].AttemptID != "" || b.Students[1].StudentID != "Z9" {
		t.Fatalf("unexpected board %+v", b.Students)
	}
	ana := b.Students[0]
	if ana.Total != 6 || ana.State != session.AttemptActive {
		t.Fatalf("unexpected row %+v", ana)
	}

	if _, err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if b, err = c.AttemptAction(ctx, ana.AttemptID, "extend", map[string]any{"minutes": 3}); err != nil || b.Students[0].ExtraMs != 180_000 {
		t.Fatalf("extend: %v %+v", err, b)
	}
	if _, err = c.AttemptAction(ctx, ana.AttemptID, "message", map[string]any{"text": "Mantén la pestaña activa"}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.AttemptAction(ctx, ana.AttemptID, "message", map[string]any{"text": "  "}); err == nil {
		t.Fatal("empty message must fail")
	}
	if b, err = c.AttemptAction(ctx, ana.AttemptID, "pause", map[string]any{"on": true}); err != nil || !b.Students[0].Paused {
		t.Fatalf("pause: %v", err)
	}
	if b, err = c.AttemptAction(ctx, ana.AttemptID, "withdraw", map[string]any{"note": "se fue"}); err != nil || b.Students[0].State != session.AttemptWithdrawn {
		t.Fatalf("withdraw: %v", err)
	}
	if _, err = c.AttemptAction(ctx, ana.AttemptID, "withdraw", nil); err == nil {
		t.Fatal("withdrawing twice must fail")
	}
	if b, err = c.AttemptAction(ctx, ana.AttemptID, "readmit", nil); err != nil || b.Students[0].State != session.AttemptActive {
		t.Fatalf("readmit: %v", err)
	}
	if b, err = c.RemoveParticipant(ctx, b.Students[1].ParticipantID); err != nil || len(b.Students) != 1 {
		t.Fatalf("remove invited: %v %+v", err, b)
	}
	if b, err = c.AttemptAction(ctx, ana.AttemptID, "annul", nil); err != nil || b.Students[0].State != session.AttemptAnnulled {
		t.Fatalf("annul: %v", err)
	}
}
