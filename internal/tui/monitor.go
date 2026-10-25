// Package tui holds the teacher's terminal panels.
package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mdp/qrterminal/v3"

	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/session"
)

// Mesa de luz palette, terminal edition.
var (
	accent = lipgloss.Color("#ec2a8a")
	muted  = lipgloss.Color("#8f8d88")

	brandStyle  = lipgloss.NewStyle().Bold(true)
	boldStyle   = lipgloss.NewStyle().Bold(true)
	accentStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
	mutedStyle  = lipgloss.NewStyle().Foreground(muted)
	keyStyle    = lipgloss.NewStyle().Bold(true).Reverse(true).Padding(0, 1)
	selStyle    = lipgloss.NewStyle().Bold(true).Reverse(true)
	headStyle   = lipgloss.NewStyle().Foreground(muted).Bold(true)
	bannerStyle = lipgloss.NewStyle().Bold(true).Reverse(true).Padding(0, 1)
)

// Views of the panel.
const (
	viewStudents = iota
	viewRanking
	viewIncidents
	viewLink
	viewCount
)

var viewNames = [viewCount]string{"Alumnos", "Ranking", "Incidencias", "Enlace"}

// signalNames are the teacher-facing names of integrity signals.
var signalNames = map[string]string{
	"hidden": "fuera de la pestaña", "blur": "en otra ventana", "copy": "copiar", "screenshot": "captura o impresión", "paste_input": "pegar en respuesta",
	"burst": "escritura en ráfaga", "fullscreen_exit": "salió de pantalla completa", "untrusted": "clics o teclas sintéticos",
	"webdriver": "navegador automatizado", "dom_injection": "extensión en la página", "resize": "ventana reducida",
	"second_tab": "segunda pestaña", "telemetry": "telemetría alterada", "telemetry_gap": "canal en silencio",
}

type mode int

const (
	modeNormal mode = iota
	modePrompt
	modeConfirm
	modeLocked
)

type promptKind int

const (
	promptMinutes promptKind = iota
	promptMessage
	promptAddID
	promptAddName
	promptRevert
	promptMessageAll
)

// Options configures the panel.
type Options struct {
	// CheckPassword verifies the panel password; nil means no password.
	CheckPassword func(string) bool
	// LockAfter locks an idle panel (only with a password). Zero disables it.
	LockAfter time.Duration
}

// Monitor is the live session panel. Quitting only detaches; the exam keeps
// running in the background.
type Monitor struct {
	client *daemon.Client
	opts   Options
	board  *daemon.Board
	offset time.Duration
	err    error
	notice string
	qr     string
	width  int

	view, sel int
	viewSet   bool
	mode      mode
	prompt    promptKind
	input     textinput.Model
	pendingID string  // student id typed in the first step of "add"
	reverting []int64 // penalties a revert prompt applies to
	question  string
	onYes     func() tea.Cmd
	altLabel  string // a third answer in a confirmation, e.g. "end and stop the server"
	onAlt     func() tea.Cmd
	lastInput time.Time
}

// NewMonitor builds the panel for a connected daemon.
func NewMonitor(c *daemon.Client, st *daemon.State, opts Options) *Monitor {
	var b strings.Builder
	// The QR carries the code too, so scanning it pre-fills the join form.
	qrterminal.GenerateWithConfig(st.StudentURL+"?c="+st.Code, qrterminal.Config{
		Level: qrterminal.L, Writer: &b, HalfBlocks: true, QuietZone: 1,
		BlackChar: qrterminal.BLACK_BLACK, BlackWhiteChar: qrterminal.BLACK_WHITE,
		WhiteChar: qrterminal.WHITE_WHITE, WhiteBlackChar: qrterminal.WHITE_BLACK,
	})
	in := textinput.New()
	in.CharLimit = 300
	in.SetWidth(50)
	return &Monitor{client: c, opts: opts, qr: strings.TrimRight(b.String(), "\n"), width: 100, input: in, lastInput: time.Now()}
}

type tickMsg time.Time

// stoppedMsg: the server was shut down from the panel; the panel closes.
type stoppedMsg struct{}

type boardMsg struct {
	board  *daemon.Board
	err    error
	notice string
}

func tick() tea.Cmd { return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }) }

// call runs a control action off the UI goroutine.
func (m *Monitor) call(notice string, fn func(context.Context) (*daemon.Board, error)) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		b, err := fn(ctx)
		if err != nil {
			notice = ""
		}
		return boardMsg{board: b, err: err, notice: notice}
	}
}

func (m *Monitor) refresh() tea.Cmd { return m.call("", m.client.Board) }

// Init implements tea.Model.
func (m *Monitor) Init() tea.Cmd { return tea.Batch(m.refresh(), tick()) }

// Update implements tea.Model.
func (m *Monitor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tickMsg:
		if m.opts.CheckPassword != nil && m.opts.LockAfter > 0 && m.mode != modeLocked && time.Since(m.lastInput) > m.opts.LockAfter {
			m.lock()
		}
		return m, tea.Batch(m.refresh(), tick())
	case boardMsg:
		m.err = msg.err
		if msg.board != nil {
			m.board = msg.board
			m.offset = time.Duration(msg.board.Status.ServerNow-time.Now().UnixMilli()) * time.Millisecond
			if !m.viewSet {
				m.viewSet = true
				if m.board.Status.State == session.StateLobby {
					m.view = viewLink
				}
			}
			m.clampSel()
		}
		if msg.notice != "" {
			m.notice = msg.notice
		}
	case tea.KeyPressMsg:
		m.lastInput = time.Now()
		return m.key(msg)
	case stoppedMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m *Monitor) lock() {
	m.mode = modeLocked
	m.input.Reset()
	m.input.EchoMode = textinput.EchoPassword
	m.input.Placeholder = "contraseña"
	m.input.Focus()
}

func (m *Monitor) startPrompt(kind promptKind, question, placeholder string) tea.Cmd {
	m.mode, m.prompt, m.question = modePrompt, kind, question
	m.input.Reset()
	m.input.EchoMode = textinput.EchoNormal
	m.input.Placeholder = placeholder
	return m.input.Focus()
}

func (m *Monitor) confirm(question string, yes func() tea.Cmd) {
	m.mode, m.question, m.onYes, m.altLabel, m.onAlt = modeConfirm, question, yes, "", nil
}

// confirmOr asks a question with a second way to say yes (key A).
func (m *Monitor) confirmOr(question string, yes func() tea.Cmd, altLabel string, alt func() tea.Cmd) {
	m.confirm(question, yes)
	m.altLabel, m.onAlt = altLabel, alt
}

// stopServer ends the exam (if still open) and shuts the server down.
func (m *Monitor) stopServer() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := m.client.Shutdown(ctx, true); err != nil {
			return boardMsg{err: err}
		}
		return stoppedMsg{}
	}
}

func (m *Monitor) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch m.mode {
	case modeLocked:
		if k == "enter" {
			if m.opts.CheckPassword == nil || m.opts.CheckPassword(m.input.Value()) {
				m.mode, m.notice = modeNormal, "Panel desbloqueado."
				m.input.Blur()
			} else {
				m.notice = "Contraseña incorrecta."
			}
			m.input.Reset()
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	case modeConfirm:
		m.mode = modeNormal
		if k == "y" || k == "s" || k == "Y" || k == "S" {
			return m, m.onYes()
		}
		if (k == "a" || k == "A") && m.onAlt != nil {
			return m, m.onAlt()
		}
		m.notice = "Cancelado."
		return m, nil
	case modePrompt:
		switch k {
		case "esc":
			m.mode, m.notice = modeNormal, "Cancelado."
			return m, nil
		case "enter":
			return m, m.submitPrompt(strings.TrimSpace(m.input.Value()))
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m.normalKey(k)
}

func (m *Monitor) submitPrompt(v string) tea.Cmd {
	m.mode = modeNormal
	row := m.selected()
	switch m.prompt {
	case promptMinutes:
		mins, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64)
		if err != nil || mins <= 0 || mins > 600 || row == nil {
			m.notice = "Escribe un número de minutos mayor que 0."
			return nil
		}
		return m.call(fmt.Sprintf("%s minutos más para %s.", v, row.Name), func(ctx context.Context) (*daemon.Board, error) {
			return m.client.AttemptAction(ctx, row.AttemptID, "extend", map[string]any{"minutes": mins})
		})
	case promptMessage:
		if v == "" || row == nil {
			return nil
		}
		return m.call("Mensaje enviado a "+row.Name+".", func(ctx context.Context) (*daemon.Board, error) {
			return m.client.AttemptAction(ctx, row.AttemptID, "message", map[string]any{"text": v})
		})
	case promptAddID:
		if v == "" {
			return nil
		}
		m.pendingID = v
		return m.startPrompt(promptAddName, "Nombre y apellidos de "+v+":", "Nombre Apellido")
	case promptAddName:
		id := m.pendingID
		return m.call("Alumno añadido: "+v+".", func(ctx context.Context) (*daemon.Board, error) {
			return m.client.AddStudent(ctx, id, v)
		})
	case promptMessageAll:
		if v == "" {
			return nil
		}
		return m.call("Mensaje enviado a todos.", func(ctx context.Context) (*daemon.Board, error) {
			return m.client.MessageAll(ctx, v)
		})
	case promptRevert:
		ids := m.reverting
		return m.call("Penalización revertida.", func(ctx context.Context) (*daemon.Board, error) {
			return m.client.RevertPenalties(ctx, ids, v)
		})
	}
	return nil
}

// revert asks for a note and reverts the active penalties of an incident.
func (m *Monitor) revert(in *daemon.Incident) tea.Cmd {
	if in == nil {
		m.notice = "No hay penalizaciones activas que revertir."
		return nil
	}
	m.reverting = activePenalties(in)
	return m.startPrompt(promptRevert, fmt.Sprintf("Revertir «%s» de %s. Nota (opcional):", signalName(in.Kind), in.Name), "motivo")
}

func activePenalties(in *daemon.Incident) []int64 {
	var ids []int64
	for _, p := range in.Penalties {
		if p.Active() {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// latestPenalized is the newest incident of attemptID that still costs something.
func (m *Monitor) latestPenalized(attemptID string) *daemon.Incident {
	if m.board == nil {
		return nil
	}
	for i := range m.board.Incidents {
		in := &m.board.Incidents[i]
		if in.AttemptID == attemptID && len(activePenalties(in)) > 0 {
			return in
		}
	}
	return nil
}

func (m *Monitor) selectedIncident() *daemon.Incident {
	if m.board == nil || m.view != viewIncidents || m.sel < 0 || m.sel >= len(m.board.Incidents) {
		return nil
	}
	return &m.board.Incidents[m.sel]
}

func signalName(kind string) string {
	if n, ok := signalNames[kind]; ok {
		return n
	}
	return kind
}

func (m *Monitor) normalKey(k string) (tea.Model, tea.Cmd) {
	st := m.status()
	row := m.selected()
	attempt := func(action, notice string, body map[string]any) tea.Cmd {
		return m.call(notice, func(ctx context.Context) (*daemon.Board, error) {
			return m.client.AttemptAction(ctx, row.AttemptID, action, body)
		})
	}
	switch k {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab":
		m.view = (m.view + 1) % viewCount
		m.sel = 0
	case "shift+tab":
		m.view = (m.view + viewCount - 1) % viewCount
		m.sel = 0
	case "up", "k":
		if m.sel > 0 {
			m.sel--
		}
	case "down", "j":
		m.sel++
		m.clampSel()
	case "l", "L":
		if m.opts.CheckPassword == nil {
			m.notice = "Configura una contraseña con `ax admin set-password` para poder bloquear."
		} else {
			m.lock()
		}
	case "s", "S":
		if st != nil && st.State == session.StateLobby {
			m.view, m.sel = viewStudents, 0
			return m, m.call("Examen iniciado.", m.startExam)
		}
	case "+":
		if st != nil && st.State == session.StateRunning {
			return m, m.call("5 minutos más para todos.", func(ctx context.Context) (*daemon.Board, error) {
				if _, err := m.client.Extend(ctx, 5); err != nil {
					return nil, err
				}
				return m.client.Board(ctx)
			})
		}
	case "e", "E":
		switch {
		case st != nil && st.State != session.StateEnded:
			m.confirmOr("¿Terminar el examen para todos y entregar lo guardado?", func() tea.Cmd {
				return m.call("Examen terminado: entregas guardadas. El servidor sigue para ver resultados.", func(ctx context.Context) (*daemon.Board, error) {
					if _, err := m.client.End(ctx); err != nil {
						return nil, err
					}
					return m.client.Board(ctx)
				})
			}, "terminar y apagar el servidor", m.stopServer)
		case st != nil:
			m.confirm("¿Apagar el servidor? Los resultados quedan guardados (`ax results`).", m.stopServer)
		}
	case "M", "shift+m":
		if st != nil && st.State != session.StateEnded {
			return m, m.startPrompt(promptMessageAll, "Mensaje para TODOS los alumnos:", "Quedan 10 minutos")
		}
	case "P", "shift+p":
		if st != nil && st.State == session.StateRunning {
			if m.board.AllPaused {
				return m, m.call("Todos pueden continuar.", func(ctx context.Context) (*daemon.Board, error) { return m.client.PauseAll(ctx, false) })
			}
			m.confirm("¿Pausar el examen para todos? Su tiempo sigue corriendo.", func() tea.Cmd {
				return m.call("Examen en pausa para todos (el tiempo sigue).", func(ctx context.Context) (*daemon.Board, error) { return m.client.PauseAll(ctx, true) })
			})
		}
	case "a", "A", "r", "R":
		if req := m.firstRequest(); req != nil {
			approve := k == "a" || k == "A"
			verb := "Rechazado"
			if approve {
				verb = "Aprobado"
			}
			return m, m.call(fmt.Sprintf("%s: %s (%s).", verb, req.Name, req.StudentID), func(ctx context.Context) (*daemon.Board, error) {
				return m.client.DecideRequest(ctx, req.ID, approve)
			})
		}
	case "n", "N":
		return m, m.startPrompt(promptAddID, "Identificador del nuevo alumno:", "A-123")
	case "z", "Z":
		switch {
		case m.view == viewIncidents:
			in := m.selectedIncident()
			if in != nil && len(activePenalties(in)) == 0 {
				in = nil
			}
			return m, m.revert(in)
		case m.view == viewStudents && row != nil && row.AttemptID != "":
			return m, m.revert(m.latestPenalized(row.AttemptID))
		}
	}
	if row == nil || m.view != viewStudents {
		return m, nil
	}
	switch k {
	case "d", "D":
		label := "¿Quitar a " + row.Name + " de la lista?"
		if row.AttemptID != "" {
			label = "¿Dar de baja a " + row.Name + "? Sus respuestas se guardan y puedes readmitirlo."
		}
		m.confirm(label, func() tea.Cmd {
			return m.call(row.Name+" quitado.", func(ctx context.Context) (*daemon.Board, error) {
				return m.client.RemoveParticipant(ctx, row.ParticipantID)
			})
		})
		return m, nil
	}
	if row.AttemptID == "" {
		return m, nil
	}
	switch k {
	case "t", "T":
		return m, m.startPrompt(promptMinutes, "Minutos extra para "+row.Name+":", "5")
	case "m":
		return m, m.startPrompt(promptMessage, "Mensaje para "+row.Name+":", "Recuerda mantener esta pestaña activa")
	case "p":
		if row.Paused {
			return m, attempt("pause", row.Name+" puede continuar.", map[string]any{"on": false})
		}
		return m, attempt("pause", row.Name+" está en pausa (su tiempo sigue).", map[string]any{"on": true})
	case "u", "U":
		return m, attempt("readmit", row.Name+" readmitido.", nil)
	case "x", "X":
		m.confirm("¿Anular el examen de "+row.Name+"? Se guarda todo y puedes readmitirlo.", func() tea.Cmd {
			return attempt("annul", "Examen de "+row.Name+" anulado.", nil)
		})
	}
	return m, nil
}

func (m *Monitor) startExam(ctx context.Context) (*daemon.Board, error) {
	if _, err := m.client.Start(ctx); err != nil {
		return nil, err
	}
	return m.client.Board(ctx)
}

func (m *Monitor) status() *daemon.StatusView {
	if m.board == nil {
		return nil
	}
	return &m.board.Status
}

func (m *Monitor) firstRequest() *daemonRequest {
	if m.board == nil || len(m.board.Requests) == 0 {
		return nil
	}
	r := m.board.Requests[len(m.board.Requests)-1] // oldest first
	return &daemonRequest{ID: r.ID, Name: r.Name, StudentID: r.StudentID, Kind: string(r.Kind)}
}

type daemonRequest struct{ ID, Name, StudentID, Kind string }

func (m *Monitor) rows() []daemon.StudentRow {
	if m.board == nil {
		return nil
	}
	if m.view != viewRanking {
		return m.board.Students
	}
	var done []daemon.StudentRow
	for _, r := range m.board.Students {
		if r.State == session.AttemptSubmitted && r.Score != nil {
			done = append(done, r)
		}
	}
	sort.SliceStable(done, func(i, j int) bool {
		if *done[i].Score != *done[j].Score {
			return *done[i].Score > *done[j].Score
		}
		return done[i].UsedMs < done[j].UsedMs
	})
	return done
}

func (m *Monitor) selected() *daemon.StudentRow {
	rows := m.rows()
	if m.sel < 0 || m.sel >= len(rows) {
		return nil
	}
	r := rows[m.sel]
	return &r
}

func (m *Monitor) clampSel() {
	n := len(m.rows())
	if m.view == viewIncidents && m.board != nil {
		n = len(m.board.Incidents)
	}
	if m.sel >= n {
		m.sel = max(n-1, 0)
	}
}

// ---------------- view ----------------

// View implements tea.Model.
func (m *Monitor) View() tea.View {
	var b strings.Builder
	w := max(72, min(m.width, 140))
	st := m.status()
	b.WriteString(brandStyle.Render("ASSESS"))
	if st != nil {
		b.WriteString(mutedStyle.Render("  ·  ") + st.ExamTitle + "   " + m.stateLine())
	}
	b.WriteString("\n" + mutedStyle.Render(strings.Repeat("─", w)) + "\n")

	if st == nil {
		if m.err != nil {
			b.WriteString(accentStyle.Render("Sin conexión con el servidor: ") + m.err.Error() + "\n")
		} else {
			b.WriteString("Conectando…\n")
		}
		return m.finish(&b)
	}

	if m.mode == modeLocked {
		b.WriteString("\n" + boldStyle.Render("Sesión en curso — panel bloqueado") + "\n")
		b.WriteString(mutedStyle.Render(fmt.Sprintf("%d en línea · %d entregados", st.Online, st.Submitted)) + "\n\n")
		b.WriteString("Escribe la contraseña y pulsa Enter:  " + m.input.View() + "\n")
		return m.finish(&b)
	}

	fmt.Fprintf(&b, "%s  %s   %s\n", mutedStyle.Render("Código"), accentStyle.Render(st.Code), boldStyle.Render(st.StudentURL))
	b.WriteString(mutedStyle.Render(fmt.Sprintf("%d en línea · %d respondiendo · %d entregados · %d en total", st.Online, st.Active, st.Submitted, st.Joined)))
	if m.board.StrikeLimit > 0 {
		strikes := 0
		for _, r := range m.board.Students {
			strikes += r.Strikes
		}
		b.WriteString(mutedStyle.Render(fmt.Sprintf(" · %d avisos (anula con %d)", strikes, m.board.StrikeLimit)))
	}
	b.WriteString("\n")
	if st.LastError != "" {
		b.WriteString(accentStyle.Render("Error del servidor: ") + st.LastError + "\n")
	}
	if n := len(m.board.Requests); n > 0 {
		r := m.firstRequest()
		what := "quiere entrar desde otro dispositivo"
		if r.Kind == "late" {
			what = "quiere entrar con la entrada cerrada"
		}
		more := ""
		if n > 1 {
			more = fmt.Sprintf(" (+%d más)", n-1)
		}
		b.WriteString("\n" + bannerStyle.Render(fmt.Sprintf("%s (%s) %s%s", r.Name, r.StudentID, what, more)) + "  " +
			keyStyle.Render("A") + " aprobar  " + keyStyle.Render("R") + " rechazar\n")
	}

	tabs := make([]string, viewCount)
	for i, name := range viewNames {
		if i == m.view {
			tabs[i] = selStyle.Render(" " + name + " ")
		} else {
			tabs[i] = mutedStyle.Render(" " + name + " ")
		}
	}
	b.WriteString("\n" + strings.Join(tabs, " ") + mutedStyle.Render("   Tab cambia de vista") + "\n\n")

	switch m.view {
	case viewLink:
		b.WriteString(m.qr + "\n")
		for _, u := range st.OtherURLs {
			b.WriteString(mutedStyle.Render("También: "+u) + "\n")
		}
	case viewIncidents:
		if len(m.board.Incidents) == 0 {
			b.WriteString(mutedStyle.Render("  Sin incidencias.") + "\n")
			if m.board.StrikeLimit == 0 {
				b.WriteString(mutedStyle.Render("  La vigilancia está desactivada en este examen (proctoring: off).") + "\n")
			}
		} else {
			b.WriteString(headStyle.Render(fmt.Sprintf("  %-8s %-22s %-28s %-6s %s", "HORA", "ALUMNO", "SEÑAL", "PREG.", "CONSECUENCIA")) + "\n")
		}
		for i, in := range m.board.Incidents {
			b.WriteString(m.line(i, incidentLine(in)) + "\n")
		}
	case viewRanking:
		rows := m.rows()
		if len(rows) == 0 {
			b.WriteString(mutedStyle.Render("Todavía nadie ha entregado.") + "\n")
		}
		for i, r := range rows {
			line := fmt.Sprintf("%2d. %-28s %8s   %s", i+1, cut(r.Name, 28), score(r), clock(time.Duration(r.UsedMs)*time.Millisecond))
			b.WriteString(m.line(i, line) + "\n")
		}
	default:
		b.WriteString(headStyle.Render(fmt.Sprintf("  %-24s %-9s %-13s %-6s %-9s %-7s %-8s %-8s %s", "ALUMNO", "ID", "ESTADO", "PREG.", "RESP.", "CONEX.", "AVISOS", "NOTA", "TIEMPO")) + "\n")
		rows := m.rows()
		if len(rows) == 0 {
			b.WriteString(mutedStyle.Render("  Nadie ha entrado todavía. Comparte el código o el QR (vista Enlace).") + "\n")
		}
		for i, r := range rows {
			b.WriteString(m.line(i, m.studentLine(r)) + "\n")
		}
	}
	return m.finish(&b)
}

func (m *Monitor) line(i int, s string) string {
	if i == m.sel {
		return selStyle.Render("▸ " + s)
	}
	return "  " + s
}

func (m *Monitor) studentLine(r daemon.StudentRow) string {
	state := map[session.AttemptState]string{
		session.AttemptActive: "respondiendo", session.AttemptSubmitted: "entregado",
		session.AttemptWithdrawn: "retirado", session.AttemptAnnulled: "anulado",
	}[r.State]
	switch {
	case r.AttemptID == "":
		state = "sin entrar"
	case r.State == session.AttemptActive && m.board.Status.State == session.StateLobby:
		state = "en la sala"
	case r.Paused:
		state = "en pausa"
	}
	q := "—"
	if r.Question > 0 && r.State == session.AttemptActive {
		q = fmt.Sprintf("%d/%d", r.Question, r.Total)
	}
	resp := "—"
	if r.AttemptID != "" {
		resp = fmt.Sprintf("%d/%d", r.Answered, r.Total)
	}
	conn := "○"
	if r.Online {
		conn = "● sí"
	}
	used := ""
	if r.UsedMs > 0 {
		used = clock(time.Duration(r.UsedMs) * time.Millisecond)
	}
	if r.ExtraMs > 0 {
		used += fmt.Sprintf(" +%dm", r.ExtraMs/60000)
	}
	strikes := "—"
	if m.board.StrikeLimit > 0 && r.AttemptID != "" {
		strikes = fmt.Sprintf("%d/%d", r.Strikes, m.board.StrikeLimit)
		if r.Signals > 0 {
			strikes += fmt.Sprintf(" ·%d", r.Signals)
		}
	}
	return fmt.Sprintf("%-24s %-9s %-13s %-6s %-9s %-7s %-8s %-8s %s", cut(r.Name, 24), cut(r.StudentID, 9), state, q, resp, conn, strikes, score(r), used)
}

func incidentLine(in daemon.Incident) string {
	at := time.UnixMilli(in.At).Format("15:04:05")
	what := signalName(in.Kind)
	if in.DurationMs > 0 {
		what += fmt.Sprintf(" %ds", in.DurationMs/1000)
	}
	q := "—"
	if in.Question > 0 {
		q = strconv.Itoa(in.Question)
	}
	var fx []string
	for _, p := range in.Penalties {
		s := map[session.PenaltyKind]string{session.PenaltyStrike: "aviso", session.PenaltyVoid: "pregunta anulada", session.PenaltyPoints: "puntos"}[p.Kind]
		if p.Kind == session.PenaltyTime {
			s = fmt.Sprintf("−%d min", int(p.Amount)/60000)
		}
		if !p.Active() {
			s += " (revertido)"
		}
		fx = append(fx, s)
	}
	if len(fx) == 0 {
		fx = []string{map[session.Severity]string{session.SeverityInfo: "registro", session.SeverityWarn: "advertencia", session.SeverityHigh: "grave"}[in.Severity]}
	}
	return fmt.Sprintf("%-8s %-22s %-28s %-6s %s", at, cut(in.Name, 22), cut(what, 28), q, strings.Join(fx, ", "))
}

func score(r daemon.StudentRow) string {
	if r.Score == nil {
		return "—"
	}
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return f(*r.Score) + "/" + f(r.MaxScore)
}

func cut(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n-1]) + "…"
}

func (m *Monitor) finish(b *strings.Builder) tea.View {
	b.WriteString("\n")
	if m.err != nil && m.board != nil {
		b.WriteString(accentStyle.Render("Aviso: ") + m.err.Error() + "\n")
	}
	if m.notice != "" {
		b.WriteString(m.notice + "\n")
	}
	b.WriteString("\n")
	switch m.mode {
	case modeConfirm:
		b.WriteString(accentStyle.Render(m.question) + "  " + keyStyle.Render("Y") + " sí  ")
		if m.onAlt != nil {
			b.WriteString(keyStyle.Render("A") + " " + m.altLabel + "  ")
		}
		b.WriteString(keyStyle.Render("N") + " no\n")
	case modePrompt:
		b.WriteString(boldStyle.Render(m.question) + "  " + m.input.View() + "\n" + mutedStyle.Render("Enter confirma · Esc cancela") + "\n")
	case modeLocked:
		b.WriteString(mutedStyle.Render("Q salir (el examen sigue)") + "\n")
	default:
		b.WriteString(m.keys() + "\n")
	}
	v := tea.NewView(b.String())
	v.AltScreen = true
	v.WindowTitle = "Assess"
	return v
}

func (m *Monitor) keys() string {
	k := func(key, label string) string { return keyStyle.Render(key) + " " + label }
	var top, bottom []string
	st := m.status()
	if m.view == viewStudents {
		if r := m.selected(); r != nil {
			if r.AttemptID != "" {
				pause := "pausar"
				if r.Paused {
					pause = "reanudar"
				}
				top = append(top, k("T", "tiempo"), k("M", "mensaje"), k("P", pause))
				switch r.State {
				case session.AttemptActive:
					top = append(top, k("X", "anular"))
				case session.AttemptWithdrawn, session.AttemptAnnulled:
					top = append(top, k("U", "readmitir"))
				}
			}
			top = append(top, k("D", "dar de baja"))
		}
		top = append(top, k("N", "nuevo alumno"))
		if r := m.selected(); r != nil && r.Strikes > 0 {
			top = append(top, k("Z", "revertir último aviso"))
		}
	}
	if m.view == viewIncidents {
		if in := m.selectedIncident(); in != nil && len(activePenalties(in)) > 0 {
			top = append(top, k("Z", "revertir"))
		}
		top = append(top, k("↑↓", "elegir"))
	}
	if st != nil && st.State == session.StateLobby {
		bottom = append(bottom, k("S", "comenzar"))
	}
	if st != nil && st.State == session.StateRunning {
		pauseAll := "pausar a todos"
		if m.board.AllPaused {
			pauseAll = "reanudar a todos"
		}
		bottom = append(bottom, k("+", "5 min a todos"), k("⇧P", pauseAll))
	}
	if st != nil && st.State != session.StateEnded {
		bottom = append(bottom, k("⇧M", "mensaje a todos"), k("E", "terminar"))
	} else if st != nil {
		bottom = append(bottom, k("E", "apagar el servidor"))
	}
	bottom = append(bottom, k("L", "bloquear"), k("Q", "salir (el examen sigue)"))
	if len(top) == 0 {
		return strings.Join(bottom, "  ")
	}
	return strings.Join(top, "  ") + "\n" + strings.Join(bottom, "  ")
}

func (m *Monitor) stateLine() string {
	s := m.status()
	switch s.State {
	case session.StateLobby:
		return accentStyle.Render("Sala de espera")
	case session.StateRunning:
		now := time.Now().Add(m.offset).UnixMilli()
		left := max(time.Duration(s.EndsAt-now)*time.Millisecond, 0)
		return boldStyle.Render("En curso ") + accentStyle.Render(clock(left))
	default:
		return boldStyle.Render("Terminado")
	}
}

func clock(d time.Duration) string {
	d = d.Round(time.Second)
	h, mnt, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, mnt, s)
	}
	return fmt.Sprintf("%02d:%02d", mnt, s)
}
