// Package tui holds the teacher's terminal panels.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	labelStyle  = lipgloss.NewStyle().Foreground(muted).Width(13)
	valueStyle  = lipgloss.NewStyle().Bold(true)
	accentStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
	mutedStyle  = lipgloss.NewStyle().Foreground(muted)
	keyStyle    = lipgloss.NewStyle().Bold(true).Reverse(true).Padding(0, 1)
	ruleStyle   = lipgloss.NewStyle().Foreground(muted)
)

// Monitor is the live session panel. Quitting it only detaches; the exam
// keeps running in the background.
type Monitor struct {
	client  *daemon.Client
	status  *daemon.StatusView
	offset  time.Duration // server clock minus local clock
	err     error
	notice  string
	confirm string // pending confirmation: "end"
	qr      string
	width   int
}

// NewMonitor builds the panel for a connected daemon.
func NewMonitor(c *daemon.Client, st *daemon.State) *Monitor {
	var b strings.Builder
	qrterminal.GenerateWithConfig(st.StudentURL, qrterminal.Config{
		Level: qrterminal.L, Writer: &b, HalfBlocks: true, QuietZone: 1,
		BlackChar: qrterminal.BLACK_BLACK, BlackWhiteChar: qrterminal.BLACK_WHITE,
		WhiteChar: qrterminal.WHITE_WHITE, WhiteBlackChar: qrterminal.WHITE_BLACK,
	})
	return &Monitor{client: c, qr: strings.TrimRight(b.String(), "\n"), width: 80}
}

type tickMsg time.Time

type statusMsg struct {
	status *daemon.StatusView
	err    error
	notice string
}

func tick() tea.Cmd { return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }) }

func (m *Monitor) fetch(action func(context.Context) (*daemon.StatusView, error), notice string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		st, err := action(ctx)
		return statusMsg{status: st, err: err, notice: notice}
	}
}

// Init implements tea.Model.
func (m *Monitor) Init() tea.Cmd { return tea.Batch(m.fetch(m.client.Status, ""), tick()) }

// Update implements tea.Model.
func (m *Monitor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tickMsg:
		return m, tea.Batch(m.fetch(m.client.Status, ""), tick())
	case statusMsg:
		m.err = msg.err
		if msg.status != nil {
			m.status = msg.status
			m.offset = time.Duration(msg.status.ServerNow-time.Now().UnixMilli()) * time.Millisecond
		}
		if msg.notice != "" {
			m.notice = msg.notice
		}
	case tea.KeyPressMsg:
		return m.key(msg.String())
	}
	return m, nil
}

func (m *Monitor) key(k string) (tea.Model, tea.Cmd) {
	if m.confirm != "" {
		confirmed := k == "y" || k == "s"
		pending := m.confirm
		m.confirm = ""
		if confirmed && pending == "end" {
			return m, m.fetch(m.client.End, "Examen terminado: todas las entregas están guardadas.")
		}
		m.notice = "Cancelado."
		return m, nil
	}
	switch k {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
	case "s", "S":
		if m.status != nil && m.status.State == session.StateLobby {
			return m, m.fetch(m.client.Start, "Examen iniciado.")
		}
	case "+":
		if m.status != nil && m.status.State == session.StateRunning {
			return m, m.fetch(func(ctx context.Context) (*daemon.StatusView, error) { return m.client.Extend(ctx, 5) }, "Se añadieron 5 minutos para todos.")
		}
	case "e", "E":
		if m.status != nil && m.status.State != session.StateEnded {
			m.confirm = "end"
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m *Monitor) View() tea.View {
	var b strings.Builder
	w := max(60, min(m.width, 100))
	b.WriteString(brandStyle.Render("ASSESS"))
	if m.status != nil {
		b.WriteString(mutedStyle.Render("  ·  ") + m.status.ExamTitle)
	}
	b.WriteString("\n" + ruleStyle.Render(strings.Repeat("─", w)) + "\n\n")

	if m.status == nil {
		if m.err != nil {
			b.WriteString(accentStyle.Render("Sin conexión con el servidor: ") + m.err.Error() + "\n")
		} else {
			b.WriteString("Conectando…\n")
		}
		return m.finish(&b)
	}
	s := m.status
	row := func(label, value string) { b.WriteString(labelStyle.Render(label) + value + "\n") }
	row("Estado", m.stateLine())
	row("Código", accentStyle.Render(s.Code))
	row("Enlace", valueStyle.Render(s.StudentURL))
	for _, u := range s.OtherURLs {
		row("", mutedStyle.Render(u))
	}
	if s.State == session.StateLobby {
		row("Alumnos", valueStyle.Render(fmt.Sprint(s.Joined))+" en la sala de espera")
	} else {
		row("Alumnos", fmt.Sprintf("%s conectados · %s respondiendo · %s entregados",
			valueStyle.Render(fmt.Sprint(s.Joined)), valueStyle.Render(fmt.Sprint(s.Active)), valueStyle.Render(fmt.Sprint(s.Submitted))))
	}
	if s.LastError != "" {
		row("Error", accentStyle.Render(s.LastError))
	}
	if m.err != nil {
		row("Aviso", accentStyle.Render(m.err.Error()))
	}
	b.WriteString("\n" + m.qr + "\n")
	return m.finish(&b)
}

func (m *Monitor) finish(b *strings.Builder) tea.View {
	b.WriteString("\n")
	if m.notice != "" {
		b.WriteString(m.notice + "\n\n")
	}
	if m.confirm == "end" {
		b.WriteString(accentStyle.Render("¿Terminar el examen para todos y entregar lo guardado? ") + keyStyle.Render("Y") + " sí  " + keyStyle.Render("N") + " no\n")
	} else {
		var keys []string
		if m.status != nil && m.status.State == session.StateLobby {
			keys = append(keys, keyStyle.Render("S")+" comenzar")
		}
		if m.status != nil && m.status.State == session.StateRunning {
			keys = append(keys, keyStyle.Render("+")+" 5 min a todos")
		}
		if m.status != nil && m.status.State != session.StateEnded {
			keys = append(keys, keyStyle.Render("E")+" terminar")
		}
		keys = append(keys, keyStyle.Render("Q")+" salir (el examen sigue)")
		b.WriteString(strings.Join(keys, "   ") + "\n")
	}
	v := tea.NewView(b.String())
	v.AltScreen = true
	v.WindowTitle = "Assess"
	return v
}

func (m *Monitor) stateLine() string {
	s := m.status
	switch s.State {
	case session.StateLobby:
		return accentStyle.Render("Sala de espera") + mutedStyle.Render("  · pulsa S para comenzar")
	case session.StateRunning:
		now := time.Now().Add(m.offset).UnixMilli()
		left := max(time.Duration(s.EndsAt-now)*time.Millisecond, 0)
		return valueStyle.Render("En curso") + "  " + accentStyle.Render(clock(left)) + mutedStyle.Render(" restantes")
	default:
		return valueStyle.Render("Terminado") + mutedStyle.Render("  · resultados guardados")
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
