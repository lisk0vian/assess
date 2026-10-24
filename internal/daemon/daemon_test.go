package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lisk0vian/assess/internal/datadir"
	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/storage/sqlite"
)

func openSession(t *testing.T, dir string) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, datadir.DB(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pub, _, err := exam.Publish("../../examples/algebra-01")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.NewService(db).CreateSession(ctx, pub, session.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestRunLifecycle(t *testing.T) {
	dir := t.TempDir()
	if h, _, _ := Check(dir); h != NotRunning {
		t.Fatalf("health before start = %s", h)
	}
	openSession(t, dir)

	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Config{DataDir: dir, Host: "127.0.0.1", Port: 0, Version: "test"})
	}()

	var c *Client
	deadline := time.Now().Add(10 * time.Second)
	for {
		var err error
		if c, _, err = Dial(dir); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon never became ready: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	ctx := context.Background()
	st, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != session.StateLobby || len(st.Code) != session.CodeLength || !strings.HasPrefix(st.StudentURL, "http://127.0.0.1:") {
		t.Fatalf("unexpected status %+v", st)
	}
	resp, err := http.Get(st.StudentURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("student page: %v %v", err, resp)
	}
	resp.Body.Close()

	if st, err = c.Start(ctx); err != nil || st.State != session.StateRunning {
		t.Fatalf("start: %v %+v", err, st)
	}
	before := st.EndsAt
	if st, err = c.Extend(ctx, 5); err != nil || st.EndsAt-before != (5*time.Minute).Milliseconds() {
		t.Fatalf("extend: %v %+v", err, st)
	}
	if _, err := c.Start(ctx); err == nil {
		t.Fatal("starting twice should fail")
	}

	bad := &Client{addr: c.addr, token: "wrong", hc: c.hc}
	if _, err := bad.Status(ctx); err == nil {
		t.Fatal("a wrong token must be rejected")
	}

	if err := c.Shutdown(ctx, true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not stop")
	}
	if _, err := os.Stat(datadir.State(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("state file left behind after a clean stop")
	}
}

func TestCheckDetectsCrash(t *testing.T) {
	dir := t.TempDir()
	// A state file whose process is gone and whose heartbeat is stale.
	if err := writeState(dir, &State{PID: 999999, Heartbeat: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if h, _, _ := Check(dir); h != Crashed {
		t.Fatalf("health = %s, want crashed", h)
	}
	if _, _, err := Dial(dir); err == nil {
		t.Fatal("dialing a crashed daemon must fail")
	}
}

func TestRunWithoutSessionFails(t *testing.T) {
	err := Run(context.Background(), Config{DataDir: t.TempDir(), Host: "127.0.0.1"})
	if err == nil || !strings.Contains(err.Error(), "no open session") {
		t.Fatalf("err = %v", err)
	}
}
