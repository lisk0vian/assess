package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/session"
)

func TestMonitorView(t *testing.T) {
	m := NewMonitor(&daemon.Client{}, &daemon.State{StudentURL: "http://192.168.1.20:27337/"})
	now := time.Now().UnixMilli()
	m.Update(statusMsg{status: &daemon.StatusView{
		ExamTitle: "Álgebra 1", Code: "K7Q2XM", State: session.StateLobby,
		StudentURL: "http://192.168.1.20:27337/", Joined: 3, ServerNow: now,
	}})
	v := m.View().Content
	for _, want := range []string{"Álgebra 1", "K7Q2XM", "http://192.168.1.20:27337/", "Sala de espera", "comenzar", "salir (el examen sigue)"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}

	m.Update(statusMsg{status: &daemon.StatusView{State: session.StateRunning, EndsAt: now + (32*60+14)*1000, ServerNow: now}})
	if v := m.View().Content; !strings.Contains(v, "32:1") || !strings.Contains(v, "5 min a todos") {
		t.Fatalf("running view wrong:\n%s", v)
	}

	// E asks for confirmation; N cancels without calling the server.
	m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	if !strings.Contains(m.View().Content, "¿Terminar el examen") {
		t.Fatal("end did not ask for confirmation")
	}
	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if m.confirm != "" || !strings.Contains(m.View().Content, "Cancelado") {
		t.Fatal("confirmation was not cancelled")
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q did not produce QuitMsg")
	}
}
