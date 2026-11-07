package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lisk0vian/assess/internal/i18n"
)

func TestFirstRunCreatesAnEncryptedVault(t *testing.T) {
	m := NewFirstRun(8)
	var gotName, gotPw string
	m.Create = func(name, pw string) (string, error) {
		gotName, gotPw = name, pw
		return "RK4T-8MZP-Q2XH", nil
	}
	drive(m, keys("enter")...)
	if !strings.Contains(view(m), "CONTRASEÑA") {
		t.Fatalf("Enter goes to the vault:\n%s", view(m))
	}
	drive(m, keys("enter", "corta", "enter", "enter")...)
	if gotName != "" || !strings.Contains(view(m), "al menos 8") {
		t.Fatalf("a short password is refused in words:\n%s", view(m))
	}
	drive(m, keys("larga-y-segura", "enter", "larga-y-segura", "enter")...)
	if gotName != "default" || gotPw != "larga-y-segura" {
		t.Fatalf("created %q %q", gotName, gotPw)
	}
	if v := view(m); !strings.Contains(v, "RK4T-8MZP-Q2XH") || m.Done {
		t.Fatalf("the recovery key waits for Enter:\n%s", v)
	}
	drive(m, keys("enter")...)
	if !m.Done {
		t.Fatal("Enter after saving the key finishes")
	}
}

func TestFirstRunWithoutPassword(t *testing.T) {
	m := NewFirstRun(8)
	m.Create = func(_, pw string) (string, error) {
		if pw != "" {
			return "", errors.New("expected no password")
		}
		return "", nil
	}
	drive(m, keys("enter")...)
	drive(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if !m.Done {
		t.Fatalf("Ctrl+U creates a plain vault: %q", m.problem)
	}
}

func prepareData() *PrepareData {
	return &PrepareData{ExamTitle: "Listas y bucles", Duration: 45 * time.Minute, Classes: []string{"1.º DAM"}, Members: []int{24},
		Addrs: []string{"192.168.1.20"}, Firewall: FirewallState{Supported: true, Text: "El firewall puede dejar fuera"},
		OpenPort: func() FirewallState { return FirewallState{Supported: true, Ready: true, Text: "abierto"} }}
}

func TestPrepareChoosesAndChecks(t *testing.T) {
	m := NewPrepare(prepareData())
	drive(m, keys("right", "down", "right", "down", "right", "right")...)
	drive(m, keys("enter")...)
	if m.Open {
		t.Fatal("the first Enter with a closed firewall only warns")
	}
	drive(m, keys("a")...)
	if !m.d.Firewall.Ready {
		t.Fatal("A opens the port")
	}
	drive(m, keys("enter")...)
	if !m.Open || m.Class != "1.º DAM" || !m.RosterOnly || m.Duration != 45*time.Minute {
		t.Fatalf("result %+v", m)
	}
	bad := prepareData()
	bad.Problems = []string{"q001: falta la respuesta"}
	b := NewPrepare(bad)
	drive(b, keys("enter", "enter")...)
	if b.Open || !strings.Contains(view(b), "falta la respuesta") {
		t.Fatalf("an exam with errors cannot open:\n%s", view(b))
	}
}

func TestRecoverGivesTheTimeBack(t *testing.T) {
	now := time.Date(2026, 11, 7, 10, 30, 0, 0, time.Local)
	d := &RecoverData{ExamTitle: "Listas", Code: "K7Q2XM", Joined: 24, Submitted: 3, Waiting: 21, Running: true,
		Down: 6*time.Minute + 10*time.Second, EndsAt: now.Add(20 * time.Minute), Now: now}
	m := NewRecover(d)
	if m.extra != 7 {
		t.Fatalf("preselects the minutes it was down: %d", m.extra)
	}
	v := view(m)
	for _, want := range []string{"a salvo", "no se paró", "10:50", "10:57"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
	drive(m, keys("enter")...)
	if !m.Resume || m.Extra != 7*time.Minute {
		t.Fatalf("resume with %v", m.Extra)
	}

	// The time ran out: it cannot go on without extra time.
	d.EndsAt, d.Down = now.Add(-3*time.Minute), time.Minute
	r := NewRecover(d)
	drive(r, keys("left", "left", "left", "left", "left", "left", "left", "left", "left")...)
	if r.extra < 8 || !strings.Contains(view(r), "se acabó") {
		t.Fatalf("at least enough to go on: %d\n%s", r.extra, view(r))
	}
	drive(r, keys("t", "n")...)
	if r.End {
		t.Fatal("ending asks first")
	}
	drive(r, keys("t", "s")...)
	if !r.End {
		t.Fatal("yes ends it")
	}
}

func TestSetupScreensFitTheWindow(t *testing.T) {
	defer func() { _ = i18n.SetLanguage("es") }()
	now := time.Now()
	for _, lang := range []string{"es", "en"} {
		if err := i18n.SetLanguage(lang); err != nil {
			t.Fatal(err)
		}
		for _, size := range [][2]int{{120, 36}, {80, 24}, {72, 16}, {160, 50}} {
			check := func(m tea.Model, what string) {
				t.Helper()
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				lines := strings.Split(m.View().Content, "\n")
				if len(lines) != size[1] {
					t.Fatalf("%s %v %s: %d rows", lang, size, what, len(lines))
				}
				for i, l := range lines {
					if w := ansi.StringWidth(l); w != size[0] {
						t.Fatalf("%s %v %s: row %d is %d cells", lang, size, what, i, w)
					}
				}
			}
			f := NewFirstRun(8)
			f.Create = func(string, string) (string, error) { return "RK4T-8MZP-Q2XH-7WNC-L9DA-6VJE", nil }
			check(f, "welcome")
			f.step = 1
			f.problem = strings.Repeat("problema ", 20)
			check(f, "vault")
			f.step, f.recovery = 2, "RK4T-8MZP-Q2XH-7WNC-L9DA-6VJE"
			check(f, "recovery")
			p := NewPrepare(prepareData())
			check(p, "prepare")
			bad := prepareData()
			bad.Problems = []string{strings.Repeat("error largo ", 12)}
			check(NewPrepare(bad), "prepare with errors")
			r := NewRecover(&RecoverData{ExamTitle: "Listas", Running: true, Down: time.Minute, EndsAt: now.Add(-time.Minute), Now: now,
				Log: []string{strings.Repeat("línea del registro ", 8), "otra"}})
			check(r, "recover")
			r.showLog, r.confirmEnd = true, true
			check(r, "recover log")
			check(NewRecover(&RecoverData{ExamTitle: "Listas", Now: now}), "recover lobby")
		}
	}
}
