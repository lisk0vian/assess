package tui

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/i18n"
	"github.com/lisk0vian/assess/internal/report"
	"github.com/lisk0vian/assess/internal/server/student"
	"github.com/lisk0vian/assess/internal/session"
)

func press(m *Monitor, keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		default:
			r := []rune(k)[0]
			msg = tea.KeyPressMsg{Code: r, Text: k}
		}
		_, cmd = m.Update(msg)
	}
	return cmd
}

func board(state session.State) *daemon.Board {
	now := time.Now().UnixMilli()
	nine := 9.0
	return &daemon.Board{
		Status: daemon.StatusView{ExamTitle: "Álgebra 1", Code: "K7Q2XM", State: state, StudentURL: "http://192.168.1.20:27337/",
			ServerNow: now, EndsAt: now + (32*60+14)*1000, Joined: 2, Online: 1, Active: 1, Submitted: 1},
		Students: []daemon.StudentRow{
			{AttemptID: "a1", ParticipantID: "p1", StudentID: "A-1", Name: "Ana Pérez", State: session.AttemptActive, Online: true, Question: 4, Answered: 3, Total: 6, MaxScore: 9},
			{AttemptID: "a2", ParticipantID: "p2", StudentID: "L-7", Name: "Luis Gómez", State: session.AttemptSubmitted, Total: 6, Answered: 6, Score: &nine, MaxScore: 9, UsedMs: 31 * 60_000},
			{ParticipantID: "p3", StudentID: "Z9", Name: "Zoe"},
		},
		Requests: []student.Request{{ID: "r1", Kind: student.KindDevice, StudentID: "J-1", Name: "Juan", Status: student.Pending}},
	}
}

func TestMonitorLobbyShowsLinkFirst(t *testing.T) {
	m := NewMonitor(&daemon.Client{}, &daemon.State{StudentURL: "http://192.168.1.20:27337/", Code: "K7Q2XM"}, Options{})
	m.Update(boardMsg{board: board(session.StateLobby)})
	v := m.View().Content
	for _, want := range []string{"Álgebra 1", "K7Q2XM", "Sala de espera", "comenzar", "Juan (J-1) quiere entrar desde otro dispositivo"} {
		if !strings.Contains(v, want) {
			t.Fatalf("lobby view missing %q:\n%s", want, v)
		}
	}
	if m.view != viewLink {
		t.Fatal("the lobby should open on the link view")
	}
}

func TestMonitorStudentsAndActions(t *testing.T) {
	m := NewMonitor(&daemon.Client{}, &daemon.State{StudentURL: "http://x/", Code: "K7Q2XM"}, Options{})
	m.Update(boardMsg{board: board(session.StateRunning)})
	v := m.View().Content
	for _, want := range []string{"Ana Pérez", "P4  3/6", "●", "Luis Gómez", "entregado 9/9", "sin entrar", "ALUMNOS", "ENTREGADOS 1"} {
		if !strings.Contains(v, want) {
			t.Fatalf("students view missing %q:\n%s", want, v)
		}
	}

	// X asks for confirmation; N cancels without calling the server.
	press(m, "x")
	if m.mode != modeConfirm || !strings.Contains(m.View().Content, "¿Anular el examen de Ana Pérez?") {
		t.Fatal("annul did not ask for confirmation")
	}
	press(m, "n")
	if m.mode != modeNormal || !strings.Contains(m.View().Content, "Cancelado") {
		t.Fatal("confirmation was not cancelled")
	}

	// T opens a minutes prompt; an invalid value is refused locally.
	press(m, "t")
	if m.mode != modePrompt || !strings.Contains(m.View().Content, "Minutos extra para Ana Pérez") {
		t.Fatal("extra time prompt not shown")
	}
	if cmd := press(m, "a", "b", "enter"); cmd != nil || !strings.Contains(m.notice, "número de minutos") {
		t.Fatalf("invalid minutes accepted: %q", m.notice)
	}

	// The ranking lists finished students only.
	press(m, "tab")
	if v := m.View().Content; !strings.Contains(v, " 1. Luis Gómez") || strings.Contains(v, "Ana Pérez") {
		t.Fatalf("ranking wrong:\n%s", v)
	}

	if _, ok := press(m, "q")().(tea.QuitMsg); !ok {
		t.Fatal("q did not quit")
	}
}

func TestMonitorLock(t *testing.T) {
	m := NewMonitor(&daemon.Client{}, &daemon.State{StudentURL: "http://x/", Code: "K7Q2XM"}, Options{
		CheckPassword: func(pw string) bool { return pw == "secreto" },
		LockAfter:     time.Minute,
	})
	m.Update(boardMsg{board: board(session.StateRunning)})
	m.lastInput = time.Now().Add(-2 * time.Minute)
	m.Update(tickMsg(time.Now()))
	v := m.View().Content
	if m.mode != modeLocked || strings.Contains(v, "Ana Pérez") || !strings.Contains(v, "panel bloqueado") {
		t.Fatalf("idle panel did not lock or leaked names:\n%s", v)
	}
	press(m, "m", "a", "l", "enter")
	if m.mode != modeLocked || !strings.Contains(m.notice, "incorrecta") {
		t.Fatal("wrong password unlocked the panel")
	}
	for _, r := range "secreto" {
		press(m, string(r))
	}
	press(m, "enter")
	if m.mode != modeNormal || !strings.Contains(m.View().Content, "Ana Pérez") {
		t.Fatal("right password did not unlock")
	}

	n := NewMonitor(&daemon.Client{}, &daemon.State{}, Options{})
	n.Update(boardMsg{board: board(session.StateRunning)})
	press(n, "l")
	if n.mode == modeLocked || !strings.Contains(n.notice, "config password") {
		t.Fatal("locking without a password should explain how to set one")
	}
}

func TestMonitorIncidentsAndRevert(t *testing.T) {
	m := NewMonitor(&daemon.Client{}, &daemon.State{}, Options{})
	b := board(session.StateRunning)
	b.StrikeLimit = 3
	b.Students[0].Strikes, b.Students[0].Signals = 2, 4
	b.Incidents = []daemon.Incident{
		{EventID: 9, AttemptID: "a1", Name: "Ana Pérez", Kind: "hidden", Severity: session.SeverityHigh, Question: 4, DurationMs: 22000,
			At: time.Now().UnixMilli(), Penalties: []*session.Penalty{{ID: 5, Kind: session.PenaltyStrike}, {ID: 6, Kind: session.PenaltyTime, Amount: 60000}}},
		{EventID: 8, AttemptID: "a1", Name: "Ana Pérez", Kind: "copy", Severity: session.SeverityHigh,
			Penalties: []*session.Penalty{{ID: 4, Kind: session.PenaltyStrike, RevertedAt: time.Now()}}},
	}
	m.Update(boardMsg{board: b})
	if v := m.View().Content; !strings.Contains(v, "■■□") || !strings.Contains(v, "avisos") {
		t.Fatalf("students view lacks strikes:\n%s", v)
	}
	press(m, "tab", "tab")
	v := m.View().Content
	for _, want := range []string{"fuera de la pestaña 22s", "aviso, −1 min", "aviso (revertido)"} {
		if !strings.Contains(v, want) {
			t.Fatalf("incidents view missing %q:\n%s", want, v)
		}
	}
	press(m, "z")
	if m.mode != modePrompt || len(m.reverting) != 2 || !strings.Contains(m.View().Content, "Revertir «fuera de la pestaña» de Ana Pérez") {
		t.Fatalf("revert prompt not shown: mode %v ids %v", m.mode, m.reverting)
	}
	press(m, "esc", "down", "z")
	if m.mode == modePrompt {
		t.Fatal("an incident with nothing active must not open the prompt")
	}
}

func TestMonitorKeysFollowState(t *testing.T) {
	m := NewMonitor(&daemon.Client{}, &daemon.State{}, Options{})
	b := board(session.StateRunning)
	m.Update(boardMsg{board: b})
	press(m, "?")
	v := m.View().Content
	for _, want := range []string{"pausar", "pausar a todos", "mensaje a todos"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
	b.Students[0].Paused, b.AllPaused = true, true
	m.Update(boardMsg{board: b})
	v = m.View().Content
	press(m, "esc")
	if !strings.Contains(v, "reanudar") || !strings.Contains(v, "reanudar a todos") {
		t.Fatalf("pause labels must turn into resume:\n%s", v)
	}

	press(m, "M")
	if m.mode != modePrompt || m.prompt != promptMessageAll {
		t.Fatal("⇧M asks for a message to everyone")
	}
	press(m, "esc", "E")
	if v := m.View().Content; m.mode != modeConfirm || !strings.Contains(v, "terminar y apagar el servidor") {
		t.Fatalf("E offers ending with the server:\n%s", v)
	}
	press(m, "n")
	ended := board(session.StateEnded)
	m.Update(boardMsg{board: ended})
	if v := m.View().Content; !strings.Contains(v, "apagar el servidor") || strings.Contains(v, "mensaje a todos") {
		t.Fatalf("after the end, E stops the server:\n%s", v)
	}
}

func TestMonitorOpensReportWhenEnded(t *testing.T) {
	m := NewMonitor(&daemon.Client{}, &daemon.State{StudentURL: "http://x/", Code: "K7Q2XM"}, Options{})
	m.Update(boardMsg{board: board(session.StateRunning)})
	if cmd := press(m, "i"); cmd != nil || m.WantsReport() {
		t.Fatal("the report is only for ended sessions")
	}
	m.Update(boardMsg{board: board(session.StateEnded)})
	if !strings.Contains(m.View().Content, "informe") {
		t.Fatal("an ended session should offer the report")
	}
	if cmd := press(m, "i"); cmd == nil || !m.WantsReport() {
		t.Fatal("I should close the panel to open the report")
	}
}

func TestMonitorShowsAwayHits(t *testing.T) {
	m := NewMonitor(&daemon.Client{}, &daemon.State{StudentURL: "http://x/", Code: "K7Q2XM"}, Options{})
	b := board(session.StateRunning)
	b.StrikeLimit = 3
	b.Students[0].Strikes, b.Students[0].Signals, b.Students[0].AwayHits = 1, 2, 1
	b.AwayHits = []report.AwayHit{{Who: report.Who{Name: "Ana Pérez", StudentID: "A-1"}, QuestionID: "q004", Signal: "hidden",
		AwayMs: 40_000, AfterMs: 8_000, Difficulty: 0.75, Strong: true}}
	m.Update(boardMsg{board: b})
	press(m, "enter")
	if v := m.View().Content; !strings.Contains(v, "■□□") || !strings.Contains(v, "salió y acertó 1 vez") {
		t.Fatalf("the card should flag the hit:\n%s", v)
	}
	press(m, "esc")
	m.view = viewIncidents
	v := m.View().Content
	for _, want := range []string{"SALIÓ Y ACERTÓ", "Ana Pérez", "q004", "fuera de la pestaña 40s → acertó a los 8s", "la falla el 75 % del resto"} {
		if !strings.Contains(v, want) {
			t.Fatalf("incidents view missing %q:\n%s", want, v)
		}
	}
}

func TestMonitorLensAccommodation(t *testing.T) {
	m := NewMonitor(&daemon.Client{}, &daemon.State{}, Options{})
	b := board(session.StateRunning)
	m.Update(boardMsg{board: b})
	if strings.Contains(m.View().Content, "lupa") {
		t.Fatal("V is offered only in exams that use the lens")
	}
	b.Lens = true
	m.Update(boardMsg{board: b})
	press(m, "?")
	if v := m.View().Content; !strings.Contains(v, "sin lupa") {
		t.Fatalf("V offers reading without the lens:\n%s", v)
	}
	press(m, "esc")
	b.Students[0].NoLens = true
	m.Update(boardMsg{board: b})
	press(m, "enter", "?")
	if v := m.View().Content; !strings.Contains(v, "con lupa") {
		t.Fatalf("V gives the lens back:\n%s", v)
	}
	press(m, "esc")
	if v := m.View().Content; !strings.Contains(v, "sin lupa") {
		t.Fatalf("the card shows the accommodation:\n%s", v)
	}
}

func bigClass(n int) *daemon.Board {
	b := board(session.StateRunning)
	b.StrikeLimit, b.Requests = 3, nil
	b.Students = nil
	for i := range n {
		r := daemon.StudentRow{AttemptID: "a" + strconv.Itoa(i), ParticipantID: "p" + strconv.Itoa(i), StudentID: "A-" + strconv.Itoa(i),
			Name: "Alumna María " + strconv.Itoa(i), State: session.AttemptActive, Online: true, Question: 3, Answered: i % 7, Total: 6}
		switch i % 11 {
		case 3:
			r.Strikes = 1
		case 7:
			r.State, r.Answered = session.AttemptSubmitted, 6
		}
		b.Students = append(b.Students, r)
	}
	return b
}

// Every row must fill the window exactly, in both languages and at any size.
func TestMonitorFitsTheWindow(t *testing.T) {
	defer func() {
		if err := i18n.SetLanguage("es"); err != nil {
			t.Fatal(err)
		}
	}()
	for _, lang := range []string{"es", "en"} {
		if err := i18n.SetLanguage(lang); err != nil {
			t.Fatal(err)
		}
		for _, size := range [][2]int{{120, 36}, {80, 24}, {72, 16}, {160, 50}} {
			for _, n := range []int{0, 3, 25, 26, 60, 100, 140} {
				for _, state := range []session.State{session.StateLobby, session.StateRunning, session.StateEnded} {
					m := NewMonitor(&daemon.Client{}, &daemon.State{StudentURL: "http://192.168.1.20:27337/", Code: "K7Q2XM"}, Options{})
					m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					b := bigClass(n)
					b.Status.State = state
					m.Update(boardMsg{board: b})
					for view := range viewCount {
						m.view = view
						for _, overlay := range []string{"", "?", "enter"} {
							if overlay != "" {
								press(m, overlay)
							}
							lines := strings.Split(m.View().Content, "\n")
							if len(lines) != size[1] {
								t.Fatalf("%v, %d students: %d rows", size, n, len(lines))
							}
							for i, l := range lines {
								if w := ansi.StringWidth(l); w != size[0] {
									t.Fatalf("%s %v, %d students, view %d: row %d is %d cells", lang, size, n, view, i, w)
								}
							}
							press(m, "esc")
						}
					}
				}
			}
		}
	}
}

func TestMonitorFiltersAndLargeClasses(t *testing.T) {
	m := NewMonitor(&daemon.Client{}, &daemon.State{StudentURL: "http://x/", Code: "K7Q2XM"}, Options{})
	m.Update(boardMsg{board: bigClass(100)})
	v := m.View().Content
	if !strings.Contains(v, "NECESITAN ATENCIÓN 9") || !strings.Contains(v, "EL RESTO 91") {
		t.Fatalf("a large class puts problems first:\n%s", v)
	}
	if got := m.selected(); got == nil || got.Strikes == 0 {
		t.Fatal("the cursor starts on what needs attention")
	}
	press(m, "f")
	if m.filter != filterAttention || len(m.rows()) != 9 {
		t.Fatalf("F shows who needs attention: %d rows", len(m.rows()))
	}
	press(m, "4")
	for _, r := range m.rows() {
		if r.State != session.AttemptSubmitted {
			t.Fatal("4 shows who submitted")
		}
	}

	// A small class never moves: the teacher learns where each tile is.
	s := NewMonitor(&daemon.Client{}, &daemon.State{StudentURL: "http://x/", Code: "K7Q2XM"}, Options{})
	s.Update(boardMsg{board: bigClass(25)})
	if s.rows()[0].Strikes != 0 || s.rows()[3].Strikes != 1 {
		t.Fatal("tiles must keep their place up to 25 students")
	}
	s.View()
	press(s, "down")
	if s.sel != 5 {
		t.Fatalf("↓ moves a row of tiles, not one: %d", s.sel)
	}
}

func TestMonitorCardAndHelp(t *testing.T) {
	m := NewMonitor(&daemon.Client{}, &daemon.State{StudentURL: "http://x/", Code: "K7Q2XM"}, Options{})
	b := board(session.StateRunning)
	b.StrikeLimit = 3
	m.Update(boardMsg{board: b})
	press(m, "enter")
	if v := m.View().Content; !m.showCard || !strings.Contains(v, "LO QUE HA PASADO") || !strings.Contains(v, "Sin incidencias") {
		t.Fatalf("Enter opens the card:\n%s", v)
	}
	press(m, "t")
	if m.mode != modePrompt {
		t.Fatal("the card keeps the keys working")
	}
	press(m, "esc", "esc")
	if m.showCard {
		t.Fatal("Esc closes the card")
	}
	press(m, "?")
	if v := m.View().Content; !strings.Contains(v, "Teclas de esta pantalla") {
		t.Fatalf("? opens the help:\n%s", v)
	}
	if press(m, "q"); !m.showHelp {
		t.Fatal("keys do nothing behind the help")
	}
	press(m, "?")
	if m.showHelp {
		t.Fatal("? closes the help")
	}
	// The bar comes from the same table as the help.
	bar := map[string]bool{}
	for _, k := range m.barKeys() {
		bar[k.key] = true
	}
	var all []string
	for _, g := range m.keyTable() {
		for _, it := range g.items {
			all = append(all, it.key)
		}
	}
	for k := range bar {
		if !slices.Contains(all, k) {
			t.Fatalf("%s is on the bar but not in the help", k)
		}
	}
}

func TestMonitorLockedShowsNoNames(t *testing.T) {
	m := NewMonitor(&daemon.Client{}, &daemon.State{StudentURL: "http://x/", Code: "K7Q2XM"}, Options{CheckPassword: func(string) bool { return false }})
	m.Update(boardMsg{board: board(session.StateRunning)})
	press(m, "enter", "l", "a", "b")
	v := ansi.Strip(m.View().Content)
	for _, secret := range []string{"Ana Pérez", "Luis", "K7Q2XM", "ab"} {
		if strings.Contains(v, secret) {
			t.Fatalf("the locked panel shows %q:\n%s", secret, v)
		}
	}
}
