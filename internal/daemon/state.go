// Package daemon runs one exam session as a background process and exposes a
// local control API for the CLI and TUI.
package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/lisk0vian/assess/internal/datadir"
)

// HeartbeatEvery is how often a running daemon refreshes its state file.
const HeartbeatEvery = 3 * time.Second

// staleAfter marks a daemon as dead when its heartbeat is this old, which
// also covers PID reuse after a crash.
const staleAfter = 4 * HeartbeatEvery

// State is daemon.json: how to reach a running daemon. It holds the control
// token, so it is written with owner-only permissions.
type State struct {
	PID         int       `json:"pid"`
	Version     string    `json:"version"`
	SessionID   string    `json:"session_id"`
	ExamTitle   string    `json:"exam_title"`
	Code        string    `json:"code"`
	StudentURL  string    `json:"student_url"`
	OtherURLs   []string  `json:"other_urls,omitempty"`
	ControlAddr string    `json:"control_addr"`
	Token       string    `json:"token"`
	StartedAt   time.Time `json:"started_at"`
	Heartbeat   time.Time `json:"heartbeat"`
	Stopping    bool      `json:"stopping,omitempty"`
}

// ReadState loads the state file of dir; os.ErrNotExist when absent.
func ReadState(dir string) (*State, error) {
	data, err := os.ReadFile(datadir.State(dir))
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func writeState(dir string, s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := datadir.State(dir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, datadir.State(dir))
}

func removeState(dir string) { _ = os.Remove(datadir.State(dir)) }

// Health is what `ax status` reports.
type Health string

// Health values.
const (
	Running    Health = "running"
	NotRunning Health = "not_running"
	Crashed    Health = "crashed" // a state file remains but the process is gone
)

// Check inspects dir and reports whether a daemon is alive.
func Check(dir string) (Health, *State, error) {
	s, err := ReadState(dir)
	if errors.Is(err, os.ErrNotExist) {
		return NotRunning, nil, nil
	}
	if err != nil {
		return Crashed, nil, err
	}
	if processAlive(s.PID) && time.Since(s.Heartbeat) < staleAfter {
		return Running, s, nil
	}
	return Crashed, s, nil
}
